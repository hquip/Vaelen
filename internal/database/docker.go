package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"vsm/internal/config"
)

type dockerDriver struct{ m *Manager }
type containerInfo struct {
	Image      string
	State      struct{ Running bool }
	Config     struct{ Labels map[string]string }
	HostConfig struct {
		PortBindings map[string][]struct {
			HostIp   string
			HostPort string
		}
	}
}

const ownerLabel = "io.github.hquip.vsm.database.owner"

func containerName(i *Instance) string {
	hash := sha256.Sum256([]byte(i.Version))
	return fmt.Sprintf("vsm-db-%s-%s-%x", i.Owner[:12], i.Name, hash[:4])
}
func volumeName(i *Instance) string { return containerName(i) + "-data" }
func (d dockerDriver) command(ctx context.Context, args []string, env map[string]string) ([]byte, error) {
	program := "docker"
	if d.m.runner == nil {
		path, err := exec.LookPath(program)
		if err != nil {
			return nil, fmt.Errorf("Docker 模式需要安装并启动 Docker Desktop / Docker Engine")
		}
		program = path
	}
	return d.m.run(ctx, program, args, env)
}
func missingDockerObject(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such object") || strings.Contains(s, "no such container") || strings.Contains(s, "no such volume")
}
func (d dockerDriver) inspect(ctx context.Context, i *Instance) (*containerInfo, error) {
	out, err := d.command(ctx, []string{"container", "inspect", "--format", "{{json .}}", containerName(i)}, nil)
	if missingDockerObject(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c containerInfo
	if err := json.Unmarshal(out, &c); err != nil {
		return nil, err
	}
	if c.Config.Labels[ownerLabel] != i.Owner || c.Config.Labels["io.github.hquip.vsm.database.version"] != i.Version {
		return nil, fmt.Errorf("同名容器不属于该 VSM 实例，已停止操作")
	}
	if pin := i.Versions[i.Version].ImageID; pin != "" && c.Image != pin {
		return nil, fmt.Errorf("容器镜像与锁定版本不一致")
	}
	return &c, nil
}
func (d dockerDriver) running(ctx context.Context, i *Instance) (bool, error) {
	if !i.Versions[i.Version].Allocated {
		return false, nil
	}
	c, err := d.inspect(ctx, i)
	return c != nil && c.State.Running, err
}
func (d dockerDriver) start(ctx context.Context, i *Instance) (err error) {
	if running, err := d.running(ctx, i); err != nil {
		return err
	} else if running {
		return nil
	}
	if err := d.m.prepareVersion(i); err != nil {
		return err
	}
	creds, err := d.m.credentials(i)
	if err != nil {
		return err
	}
	e, _ := engine(i.Engine)
	v := i.Versions[i.Version]
	if v.ImageID == "" {
		image := e.Image + ":" + i.Version
		d.m.progress("正在拉取官方镜像 " + image)
		if _, err := d.command(ctx, []string{"pull", image}, nil); err != nil {
			return err
		}
		out, err := d.command(ctx, []string{"image", "inspect", "--format", "{{.Id}}", image}, nil)
		if err != nil {
			return err
		}
		v.ImageID = strings.TrimSpace(string(out))
		if !digestPattern.MatchString(v.ImageID) {
			return fmt.Errorf("Docker 未返回有效镜像摘要")
		}
		if err := d.m.saveVersion(i); err != nil {
			return err
		}
	}
	// Named volumes survive stop/remove; never attach a volume without our label.
	if v.ActualVersion == "" {
		out, err := d.command(ctx, []string{"run", "--rm", "--network", "none", "--read-only", "--entrypoint", e.Server, v.ImageID, "--version"}, nil)
		if err != nil {
			return err
		}
		actual, err := checkReportedVersion(i.Version, out)
		if err != nil {
			return err
		}
		v.ActualVersion = actual
		if err := d.m.saveVersion(i); err != nil {
			return err
		}
	}
	out, inspectErr := d.command(ctx, []string{"volume", "inspect", "--format", "{{json .Labels}}", volumeName(i)}, nil)
	if inspectErr != nil && !missingDockerObject(inspectErr) {
		return inspectErr
	}
	if inspectErr == nil {
		var labels map[string]string
		if err := json.Unmarshal(out, &labels); err != nil {
			return err
		}
		if labels[ownerLabel] != i.Owner {
			return fmt.Errorf("数据库卷不属于此实例，拒绝复用")
		}
	} else {
		if v.Allocated {
			return fmt.Errorf("已有数据库数据卷丢失，拒绝创建空库替代：%s", volumeName(i))
		}
		if _, err := d.command(ctx, []string{"volume", "create", "--label", ownerLabel + "=" + i.Owner, volumeName(i)}, nil); err != nil {
			return err
		}
	}
	v.Allocated = true
	if err := d.m.saveVersion(i); err != nil {
		return err
	}
	old, err := d.inspect(ctx, i)
	if err != nil {
		return err
	}
	if old != nil {
		if old.State.Running {
			return fmt.Errorf("实例状态已变化，请刷新重试")
		}
		if _, err := d.command(ctx, []string{"container", "rm", containerName(i)}, nil); err != nil {
			return err
		}
	}
	env := []string{}
	switch i.Engine {
	case "postgres":
		env = []string{"POSTGRES_USER=vsm", "POSTGRES_PASSWORD=" + creds.Password, "POSTGRES_DB=app"}
	case "mysql":
		env = []string{"MYSQL_ROOT_PASSWORD=" + creds.Password, "MYSQL_ROOT_HOST=%", "MYSQL_DATABASE=app"}
	case "mariadb":
		env = []string{"MARIADB_ROOT_PASSWORD=" + creds.Password, "MARIADB_ROOT_HOST=%", "MARIADB_DATABASE=app"}
	}
	envPath := d.m.versionPath(i, "docker.env")
	if err := config.WriteFileAtomic(envPath, []byte(strings.Join(env, "\n")+"\n"), 0600); err != nil {
		return err
	}
	target := e.DataMount
	if i.Engine == "postgres" {
		major, _ := strconv.Atoi(strings.Split(strings.Split(i.Version, "-")[0], ".")[0])
		if major < 18 {
			target = "/var/lib/postgresql/data"
		}
	}
	args := []string{"run", "--detach", "--name", containerName(i), "--label", ownerLabel + "=" + i.Owner, "--label", "io.github.hquip.vsm.database.version=" + i.Version, "--publish", fmt.Sprintf("127.0.0.1:%d:%d", i.Port, e.Port), "--mount", "type=volume,source=" + volumeName(i) + ",target=" + target, "--env-file", envPath, "--restart", "no", "--pull", "never", v.ImageID}
	if i.Engine == "redis" || i.Engine == "valkey" {
		args = append(args, e.Server, "--appendonly", "yes", "--requirepass", creds.Password)
	}
	d.m.progress("正在启动 " + i.Engine + " " + i.Version + " 并等待就绪")
	if _, err := d.command(ctx, args, nil); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = d.stop(cleanup, i)
		}
	}()
	err = waitReady(ctx, func() error {
		probe, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		_, err := d.exec(probe, i, readyArgs(i.Engine))
		return err
	})
	if err != nil {
		return err
	}
	v.Initialized = true
	return d.m.saveVersion(i)
}
func (d dockerDriver) stop(ctx context.Context, i *Instance) error {
	if !i.Versions[i.Version].Allocated {
		return nil
	}
	c, err := d.inspect(ctx, i)
	if err != nil {
		return err
	}
	if c == nil || !c.State.Running {
		return nil
	}
	_, err = d.command(ctx, []string{"container", "stop", "--time", "30", containerName(i)}, nil)
	return err
}
func (d dockerDriver) logs(ctx context.Context, i *Instance) (string, error) {
	c, err := d.inspect(ctx, i)
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", nil
	}
	out, err := d.command(ctx, []string{"logs", "--tail", "200", containerName(i)}, nil)
	return string(out), err
}
func clientSettings(i *Instance, c credential, port int) ([]string, map[string]string) {
	if i.Engine == "postgres" {
		return []string{"-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "vsm", "-d", "app", "-v", "ON_ERROR_STOP=1"}, map[string]string{"PGPASSWORD": c.Password, "PGCONNECT_TIMEOUT": "3", "PGSSLMODE": "disable"}
	}
	if i.Engine == "mysql" || i.Engine == "mariadb" {
		return []string{"--no-defaults", "--protocol=tcp", "-h", "127.0.0.1", "-P", strconv.Itoa(port), "-u", "root", "--connect-timeout=3", "-D", "app"}, map[string]string{"MYSQL_PWD": c.Password}
	}
	return []string{"-e", "--no-auth-warning", "-h", "127.0.0.1", "-p", strconv.Itoa(port)}, map[string]string{"REDISCLI_AUTH": c.Password, "VALKEYCLI_AUTH": c.Password}
}
func readyArgs(engine string) []string {
	if engine == "postgres" {
		return []string{"-Atc", "SELECT 1"}
	}
	if engine == "mysql" || engine == "mariadb" {
		return []string{"-Nse", "SELECT 1"}
	}
	return []string{"PING"}
}
func (d dockerDriver) exec(ctx context.Context, i *Instance, args []string) (string, error) {
	c, err := d.inspect(ctx, i)
	if err != nil {
		return "", err
	}
	if c == nil || !c.State.Running {
		return "", fmt.Errorf("数据库容器未运行")
	}
	creds, err := d.m.credentials(i)
	if err != nil {
		return "", err
	}
	e, _ := engine(i.Engine)
	base, env := clientSettings(i, creds, e.Port)
	flags := []string{"exec", "-i"}
	for key := range env {
		flags = append(flags, "--env", key)
	}
	flags = append(flags, containerName(i), e.Client)
	flags = append(flags, base...)
	flags = append(flags, args...)
	out, err := d.command(ctx, flags, env)
	if err == nil && (i.Engine == "redis" || i.Engine == "valkey") && strings.HasPrefix(strings.TrimSpace(string(out)), "NOAUTH") {
		err = fmt.Errorf("数据库身份验证失败")
	}
	return string(out), err
}
