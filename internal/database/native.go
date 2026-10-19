package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"vsm/internal/config"
	"vsm/internal/core"
)

type nativeDriver struct{ m *Manager }

func (d nativeDriver) dirs(i *Instance) []string {
	root := d.m.Tools.InstallDir(i.Engine, i.Version)
	dirs := []string{filepath.Join(root, "bin"), root}
	if real, err := filepath.EvalSymlinks(root); err == nil && (filepath.Base(real) == "bin" || filepath.Base(real) == "sbin") {
		dirs = append(dirs, filepath.Join(filepath.Dir(real), "bin"))
	}
	return dirs
}
func (d nativeDriver) program(i *Instance, names ...string) (string, error) {
	for _, dir := range d.dirs(i) {
		for _, name := range names {
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			path := filepath.Join(dir, name)
			if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("原生 %s %s 缺少 %s；请纳管完整数据库安装目录", i.Engine, i.Version, strings.Join(names, " / "))
}
func (d nativeDriver) call(ctx context.Context, i *Instance, names []string, args []string, env map[string]string) ([]byte, error) {
	program, err := d.program(i, names...)
	if err != nil {
		return nil, err
	}
	return d.m.run(ctx, program, args, env)
}
func (d nativeDriver) base(i *Instance) string {
	root := d.m.Tools.InstallDir(i.Engine, i.Version)
	real, err := filepath.EvalSymlinks(root)
	if err == nil {
		root = real
	}
	if filepath.Base(root) == "bin" || filepath.Base(root) == "sbin" {
		root = filepath.Dir(root)
	}
	if fi, err := os.Stat(filepath.Join(root, "share")); err == nil && fi.IsDir() {
		return root
	}
	return ""
}
func (d nativeDriver) running(ctx context.Context, i *Instance) (bool, error) {
	if !i.Versions[i.Version].Allocated {
		return false, nil
	}
	tracked, err := d.trackedProcess(i)
	if err != nil {
		return false, err
	}
	if !tracked {
		return false, nil
	}
	if i.Engine == "postgres" {
		if _, err := os.Stat(filepath.Join(d.m.versionPath(i, "data"), "PG_VERSION")); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		probe, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		_, err := d.call(probe, i, []string{"pg_ctl"}, []string{"-D", d.m.versionPath(i, "data"), "status"}, nil)
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 3 {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	probe, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if _, err := d.exec(probe, i, readyArgs(i.Engine)); err == nil {
		return true, nil
	} else {
		return false, fmt.Errorf("受管进程仍在运行，但连接验证失败，请查看日志: %w", err)
	}
}
func (d nativeDriver) prepareData(i *Instance) error {
	if err := d.m.prepareVersion(i); err != nil {
		return err
	}
	stamp := d.m.versionPath(i, "data.owner")
	data, err := config.ReadBounded(stamp, 1024)
	if err == nil {
		if strings.TrimSpace(string(data)) != i.Owner+":"+i.Engine+":"+i.Version {
			return fmt.Errorf("数据库数据目录所有权不匹配")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		if i.Versions[i.Version].Allocated {
			return fmt.Errorf("数据库数据所有权标记丢失，请恢复 %s", stamp)
		}
		entries, readErr := os.ReadDir(d.m.versionPath(i, "data"))
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if len(entries) > 0 {
			return fmt.Errorf("数据目录已有内容且不受 VSM 管理，拒绝初始化")
		}
		if err := config.WriteFileAtomic(stamp, []byte(i.Owner+":"+i.Engine+":"+i.Version+"\n"), 0600); err != nil {
			return err
		}
	}
	if i.Versions[i.Version].Allocated {
		entries, err := os.ReadDir(d.m.versionPath(i, "data"))
		if err != nil {
			return fmt.Errorf("已有数据库数据目录丢失，拒绝重新初始化: %w", err)
		}
		if i.Versions[i.Version].Initialized && len(entries) == 0 {
			return fmt.Errorf("已有数据库数据目录为空，拒绝创建空库替代")
		}
	}
	if err := d.m.ensure("instances", i.Name, "versions", i.Backend+"-"+i.Version, "data"); err != nil {
		return err
	}
	i.Versions[i.Version].Allocated = true
	return d.m.saveVersion(i)
}
func quotePG(path string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", "''") + "'"
}
func optionPath(path string) string {
	return "\"" + strings.ReplaceAll(filepath.ToSlash(path), "\"", "\\\"") + "\""
}

func (d nativeDriver) initialize(ctx context.Context, i *Instance, c credential) error {
	data := d.m.versionPath(i, "data")
	switch i.Engine {
	case "postgres":
		if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); err == nil {
			return nil
		}
		entries, err := os.ReadDir(data)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("PostgreSQL 初始化未完成，保留目录供检查: %s", data)
		}
		pw := d.m.versionPath(i, "init.pw")
		if err := config.WriteFileAtomic(pw, []byte(c.Password+"\n"), 0600); err != nil {
			return err
		}
		_, err = d.call(ctx, i, []string{"initdb"}, []string{"-D", data, "--username=vsm", "--pwfile=" + pw, "--auth-host=scram-sha-256", "--auth-local=scram-sha-256", "--encoding=UTF8", "--locale=C"}, nil)
		return err
	case "mysql", "mariadb":
		if fi, err := os.Stat(filepath.Join(data, "mysql")); err == nil && fi.IsDir() {
			return nil
		}
		entries, err := os.ReadDir(data)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("数据库初始化未完成，保留目录供检查: %s", data)
		}
		args := []string{"--no-defaults", "--datadir=" + data}
		if base := d.base(i); base != "" {
			args = append(args, "--basedir="+base)
		}
		if i.Engine == "mysql" {
			args = append(args, "--initialize-insecure")
			_, err = d.call(ctx, i, []string{"mysqld"}, args, nil)
			return err
		}
		if runtime.GOOS == "windows" {
			// The Windows utility does not accept Unix --basedir/--no-defaults.
			// Its bootstrap leaves root local; the managed init-file sets the password.
			_, err = d.call(ctx, i, []string{"mariadb-install-db", "mysql_install_db"}, []string{"--datadir=" + data}, nil)
			return err
		}
		args = append(args, "--auth-root-authentication-method=normal", "--skip-test-db")
		_, err = d.call(ctx, i, []string{"mariadb-install-db", "mysql_install_db"}, args, nil)
		return err
	}
	return nil
}
func (d nativeDriver) start(ctx context.Context, i *Instance) (err error) {
	if err := d.verify(ctx, i); err != nil {
		return err
	}
	if running, err := d.running(ctx, i); err != nil {
		return err
	} else if running {
		return nil
	}
	e, _ := engine(i.Engine)
	if _, err := d.program(i, serverNames(i.Engine)...); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(i.Port)))
	if err != nil {
		return fmt.Errorf("本机端口 %d 已占用，请选择其他端口: %w", i.Port, err)
	}
	listener.Close()
	c, err := d.m.credentials(i)
	if err != nil {
		return err
	}
	if err := d.prepareData(i); err != nil {
		return err
	}
	d.m.progress("正在准备原生 " + i.Engine + " " + i.Version + " 的独立数据目录")
	if err := d.initialize(ctx, i, c); err != nil {
		return err
	}
	data := d.m.versionPath(i, "data")
	logPath := d.m.versionPath(i, "server.log")
	var cleanup func()
	defer func() {
		if err != nil {
			if cleanup != nil {
				cleanup()
			} else {
				stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				_ = d.stop(stop, i)
			}
		}
	}()
	switch i.Engine {
	case "postgres":
		// Append runtime settings to an included file; preserve initdb's base config.
		managed := d.m.versionPath(i, "postgres-vsm.conf")
		settings := fmt.Sprintf("listen_addresses='127.0.0.1'\nport=%d\nunix_socket_directories=''\n", i.Port)
		if err := config.WriteFileAtomic(managed, []byte(settings), 0600); err != nil {
			return err
		}
		base := filepath.Join(data, "postgresql.conf")
		raw, err := config.ReadBounded(base, 2<<20)
		if err != nil {
			return err
		}
		line := "include = " + quotePG(managed)
		if !strings.Contains(string(raw), line) {
			if err := config.WriteFileAtomic(base, append(raw, []byte("\n"+line+"\n")...), 0600); err != nil {
				return err
			}
		}
		cleanup = func() {
			stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, _ = d.call(stop, i, []string{"pg_ctl"}, []string{"-D", data, "-m", "fast", "-w", "-t", "15", "stop"}, nil)
		}
		if _, err := d.call(ctx, i, []string{"pg_ctl"}, []string{"-D", data, "-l", logPath, "-w", "-t", "45", "start"}, nil); err != nil {
			return err
		}
		pidData, err := config.ReadBounded(filepath.Join(data, "postmaster.pid"), 4096)
		if err != nil {
			return err
		}
		lines := strings.Fields(string(pidData))
		if len(lines) == 0 {
			return fmt.Errorf("PostgreSQL 未写入进程 ID")
		}
		pid, err := strconv.Atoi(lines[0])
		if err != nil {
			return err
		}
		if err := d.recordProcess(i, pid); err != nil {
			return err
		}
	case "mysql", "mariadb":
		initSQL := d.m.versionPath(i, "init.sql")
		sql := "ALTER USER 'root'@'localhost' IDENTIFIED BY '" + c.Password + "';\nCREATE DATABASE IF NOT EXISTS app;\n"
		if err := config.WriteFileAtomic(initSQL, []byte(sql), 0600); err != nil {
			return err
		}
		settings := fmt.Sprintf("[mysqld]\ndatadir=%s\nport=%d\nbind-address=127.0.0.1\npid-file=%s\nlog-error=%s\ninit-file=%s\n", optionPath(data), i.Port, optionPath(d.m.versionPath(i, "server.pid")), optionPath(logPath), optionPath(initSQL))
		if base := d.base(i); base != "" {
			settings += "basedir=" + optionPath(base) + "\n"
		}
		if runtime.GOOS != "windows" {
			settings += "socket=" + optionPath(filepath.Join(os.TempDir(), containerName(i)+".sock")) + "\n"
		}
		if i.Engine == "mysql" {
			settings += "loose-mysqlx=0\n"
		}
		file := d.m.versionPath(i, "server.cnf")
		if err := config.WriteFileAtomic(file, []byte(settings), 0600); err != nil {
			return err
		}
		program, _ := d.program(i, serverNames(i.Engine)...)
		cleanup, err = d.launch(i, program, []string{"--defaults-file=" + file}, logPath)
		if err != nil {
			return err
		}
	case "redis", "valkey":
		settings := fmt.Sprintf("bind 127.0.0.1\nport %d\ndir %s\nappendonly yes\nrequirepass %s\n", i.Port, optionPath(data), c.Password)
		file := d.m.versionPath(i, "server.conf")
		if err := config.WriteFileAtomic(file, []byte(settings), 0600); err != nil {
			return err
		}
		program, _ := d.program(i, e.Server)
		cleanup, err = d.launch(i, program, []string{file}, logPath)
		if err != nil {
			return err
		}
	}
	d.m.progress("等待数据库就绪")
	err = waitReady(ctx, func() error {
		probe, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		args := readyArgs(i.Engine)
		if i.Engine == "postgres" && !i.Versions[i.Version].Initialized {
			args = append([]string{"-d", "postgres"}, args...)
		}
		_, err := d.exec(probe, i, args)
		return err
	})
	if err != nil {
		return err
	}
	if i.Engine == "postgres" && !i.Versions[i.Version].Initialized {
		check, err := d.exec(ctx, i, []string{"-d", "postgres", "-Atc", "SELECT 1 FROM pg_database WHERE datname='app'"})
		if err != nil {
			return err
		}
		if strings.TrimSpace(check) != "1" {
			if _, err := d.exec(ctx, i, []string{"-d", "postgres", "-c", "CREATE DATABASE app"}); err != nil {
				return err
			}
		}
	}
	i.Versions[i.Version].Initialized = true
	return d.m.saveVersion(i)
}
func (d nativeDriver) launch(i *Instance, program string, args []string, logPath string) (func(), error) {
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	cmd := exec.Command(program, args...)
	configureBackground(cmd)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if err := d.recordProcess(i, cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return func() { _ = cmd.Process.Kill() }, nil
}
func (d nativeDriver) stop(ctx context.Context, i *Instance) error {
	if !i.Versions[i.Version].Allocated {
		return nil
	}
	if i.Engine == "postgres" {
		running, err := d.running(ctx, i)
		if err != nil || !running {
			return err
		}
		_, err = d.call(ctx, i, []string{"pg_ctl"}, []string{"-D", d.m.versionPath(i, "data"), "-m", "fast", "-w", "-t", "30", "stop"}, nil)
		return err
	}
	running, err := d.running(ctx, i)
	if err != nil || !running {
		return err
	}
	if i.Engine == "mysql" || i.Engine == "mariadb" {
		c, err := d.m.credentials(i)
		if err != nil {
			return err
		}
		_, err = d.call(ctx, i, []string{"mysqladmin", "mariadb-admin"}, []string{"--no-defaults", "--protocol=tcp", "-h", "127.0.0.1", "-P", strconv.Itoa(i.Port), "-u", "root", "--connect-timeout=3", "shutdown"}, map[string]string{"MYSQL_PWD": c.Password})
		if err != nil {
			return err
		}
		return waitPortClosed(ctx, i.Port)
	}
	_, err = d.exec(ctx, i, []string{"SHUTDOWN", "SAVE"})
	if err != nil {
		return err
	}
	return waitPortClosed(ctx, i.Port)
}
func waitPortClosed(ctx context.Context, port int) error {
	return waitReady(ctx, func() error {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
		if err != nil {
			return nil
		}
		c.Close()
		return fmt.Errorf("等待数据库释放端口 %d", port)
	})
}
func (d nativeDriver) logs(ctx context.Context, i *Instance) (string, error) {
	path := d.m.versionPath(i, "server.log")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	start := fi.Size() - (128 << 10)
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, 0); err != nil {
		return "", err
	}
	data := make([]byte, 128<<10)
	n, err := f.Read(data)
	if n > 0 || err == io.EOF {
		return string(data[:n]), nil
	}
	return "", err
}
func (d nativeDriver) exec(ctx context.Context, i *Instance, args []string) (string, error) {
	c, err := d.m.credentials(i)
	if err != nil {
		return "", err
	}
	e, _ := engine(i.Engine)
	base, env := clientSettings(i, c, i.Port)
	names := []string{e.Client}
	if i.Engine == "mariadb" {
		names = append(names, "mysql")
	}
	out, err := d.call(ctx, i, names, append(base, args...), env)
	if err == nil && (i.Engine == "redis" || i.Engine == "valkey") && strings.HasPrefix(strings.TrimSpace(string(out)), "NOAUTH") {
		err = fmt.Errorf("数据库身份验证失败")
	}
	return string(out), err
}

// NativeVersions lists registered server installations; clients alone are insufficient.
func (m *Manager) NativeVersions(name string) ([]string, error) {
	_, err := engine(name)
	if err != nil {
		return nil, err
	}
	versions, err := (core.Registry{Paths: m.Tools}).Installed(name)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, v := range versions {
		i := &Instance{Engine: name, Version: v}
		if _, err := (nativeDriver{m}).program(i, serverNames(i.Engine)...); err == nil {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return core.CompareVersions(out[a], out[b]) < 0 })
	return out, nil
}
