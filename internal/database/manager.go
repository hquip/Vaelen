package database

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vsm/internal/core"
	"vsm/internal/logging"
	"vsm/internal/mcpserver"
)

type Manager struct {
	Root     string
	Tools    core.Paths
	Progress func(string)
	// Injected by transport tests; production always executes argument arrays.
	runner        func(context.Context, string, []string, map[string]string) ([]byte, error)
	driverFactory func(*Manager, *Instance) driver
	secretMu      sync.Mutex
	secrets       map[string]bool
}

func New(paths core.Paths) *Manager {
	return &Manager{Root: filepath.Join(paths.Root, "databases"), Tools: paths}
}
func (m *Manager) driver(i *Instance) driver {
	if m.driverFactory != nil {
		return m.driverFactory(m, i)
	}
	if i.Backend == "docker" {
		return dockerDriver{m}
	}
	return nativeDriver{m}
}
func (m *Manager) progress(text string) {
	if m.Progress != nil {
		m.Progress(text)
	}
}
func (m *Manager) run(ctx context.Context, program string, args []string, env map[string]string) ([]byte, error) {
	if m.runner != nil {
		return m.runner(ctx, program, args, env)
	}
	c := exec.CommandContext(ctx, program, args...)
	mcpserver.ConfigureProcess(c)
	c.WaitDelay = 2 * time.Second
	if env != nil {
		c.Env = os.Environ()
		for k, v := range env {
			c.Env = append(c.Env, k+"="+v)
		}
	}
	var out boundedOutput
	if len(args) > 0 && args[0] == "pull" {
		out.notify = func(chunk string) {
			if len(chunk) > 4096 {
				chunk = chunk[len(chunk)-4096:]
			}
			for _, line := range strings.FieldsFunc(chunk, func(c rune) bool { return c == '\n' || c == '\r' }) {
				if strings.TrimSpace(line) != "" {
					m.progress(line)
				}
			}
		}
	}
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	if err != nil {
		return []byte(out.text), fmt.Errorf("%s: %w；%s", filepath.Base(program), err, m.redact(strings.TrimSpace(out.text)))
	}
	return []byte(out.text), nil
}

type boundedOutput struct {
	text   string
	notify func(string)
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.notify != nil {
		b.notify(string(p))
	}
	left := (1 << 20) - len(b.text)
	if left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		b.text += string(p)
	}
	return n, nil
}
func (m *Manager) remember(password string) {
	m.secretMu.Lock()
	defer m.secretMu.Unlock()
	if m.secrets == nil {
		m.secrets = map[string]bool{}
	}
	m.secrets[password] = true
}
func (m *Manager) redact(text string) string {
	m.secretMu.Lock()
	defer m.secretMu.Unlock()
	for password := range m.secrets {
		if password != "" {
			text = strings.ReplaceAll(text, password, "[REDACTED]")
		}
	}
	return text
}

func (m *Manager) Use(ctx context.Context, r Request) (result Status, err error) {
	finish := logging.Start("database", "configure", logging.Fields{"name": r.Name, "engine": r.Engine, "backend": r.Backend, "version": r.Version})
	defer func() { finish(err) }()
	e, err := engine(r.Engine)
	if err != nil {
		return result, err
	}
	if r.Name == "" {
		r.Name = r.Engine
	}
	if r.Backend == "" {
		r.Backend = "docker"
	}
	if r.Port == 0 {
		r.Port = e.Port
	}
	if r.Backend == "native" {
		versions, err := (core.Registry{Paths: m.Tools}).Installed(r.Engine)
		if err != nil {
			return result, err
		}
		resolved := core.BestMatch(r.Version, versions)
		if resolved == "" {
			return result, fmt.Errorf("尚未纳管原生 %s %s；先使用 vsm adopt %s --path <安装目录>", r.Engine, r.Version, r.Engine)
		}
		r.Version = resolved
	}
	if r.Version == "" {
		return result, fmt.Errorf("请选择数据库版本")
	}
	owner, err := randomID()
	if err != nil {
		return result, err
	}
	next := &Instance{Schema: 1, Name: r.Name, Engine: r.Engine, Backend: r.Backend, Version: r.Version, Port: r.Port, Owner: owner, Versions: map[string]*Version{r.Version: {ID: r.Version}}}
	if err := validate(*next); err != nil {
		return result, err
	}
	unlock, err := m.lock(r.Name)
	if err != nil {
		return result, err
	}
	defer unlock()
	old, err := m.load(r.Name)
	if os.IsNotExist(err) {
		if next.Backend == "native" {
			if err := (nativeDriver{m}).verify(ctx, next); err != nil {
				return result, err
			}
		}
		if err := m.save(next); err != nil {
			return result, err
		}
		return m.status(ctx, next), nil
	}
	if err != nil {
		return result, err
	}
	if old.Engine != next.Engine || old.Backend != next.Backend {
		return result, fmt.Errorf("已有实例的数据库类型和运行方式不变；请使用新的实例名称")
	}
	// Clone the selection so failed switching leaves the old record intact.
	raw, _ := json.Marshal(old)
	json.Unmarshal(raw, next)
	next.Version = r.Version
	next.Port = r.Port
	if next.Versions[r.Version] == nil {
		next.Versions[r.Version] = &Version{ID: r.Version}
	}
	if next.Backend == "native" {
		if err := (nativeDriver{m}).verify(ctx, next); err != nil {
			return result, err
		}
	}
	if err := validate(*next); err != nil {
		return result, err
	}
	if old.Version == next.Version && old.Port == next.Port {
		return m.status(ctx, old), nil
	}
	running, err := m.driver(old).running(ctx, old)
	if err != nil {
		return result, err
	}
	if running {
		m.progress("正在停止旧版本 " + old.Version)
		if err := m.driver(old).stop(ctx, old); err != nil {
			return result, err
		}
		m.progress("正在启动版本 " + next.Version + "（使用该版本独立数据）")
		if err := m.driver(next).start(ctx, next); err != nil {
			old.Versions[next.Version] = next.Versions[next.Version]
			cleanup, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_ = m.driver(next).stop(cleanup, next)
			restoreErr := m.driver(old).start(cleanup, old)
			if restoreErr != nil {
				return result, fmt.Errorf("切换失败: %v；恢复旧服务失败: %w", err, restoreErr)
			}
			_ = m.save(old)
			return result, fmt.Errorf("切换失败，已恢复旧版本 %s: %w", old.Version, err)
		}
	}
	if err := m.save(next); err != nil {
		if running {
			rollback, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_ = m.driver(next).stop(rollback, next)
			_ = m.driver(old).start(rollback, old)
			_ = m.save(old)
		}
		return result, err
	}
	return m.status(ctx, next), nil
}

func (m *Manager) Switch(ctx context.Context, name, version string) (Status, error) {
	i, err := m.load(name)
	if err != nil {
		return Status{}, err
	}
	return m.Use(ctx, Request{Name: name, Engine: i.Engine, Backend: i.Backend, Version: version, Port: i.Port})
}
func (m *Manager) Start(ctx context.Context, name string) (result Status, err error) {
	finish := logging.Start("database", "start", logging.Fields{"name": name})
	defer func() { finish(err) }()
	unlock, err := m.lock(name)
	if err != nil {
		return result, err
	}
	defer unlock()
	i, err := m.load(name)
	if err != nil {
		return result, err
	}
	if err := m.driver(i).start(ctx, i); err != nil {
		return result, err
	}
	if err := m.save(i); err != nil {
		return result, err
	}
	return m.status(ctx, i), nil
}
func (m *Manager) Stop(ctx context.Context, name string) (result Status, err error) {
	finish := logging.Start("database", "stop", logging.Fields{"name": name})
	defer func() { finish(err) }()
	unlock, err := m.lock(name)
	if err != nil {
		return result, err
	}
	defer unlock()
	i, err := m.load(name)
	if err != nil {
		return result, err
	}
	if err := m.driver(i).stop(ctx, i); err != nil {
		return result, err
	}
	return m.status(ctx, i), nil
}
func (m *Manager) status(ctx context.Context, i *Instance) Status {
	s := Status{Instance: *i, DataLocation: m.versionPath(i, "data")}
	if i.Backend == "docker" {
		s.DataLocation = "Docker volume: " + volumeName(i)
	}
	running, err := m.driver(i).running(ctx, i)
	s.Running = running
	if err != nil {
		s.Error = err.Error()
	}
	return s
}
func (m *Manager) List(ctx context.Context) ([]Status, error) {
	if err := m.checkDirectories(""); err != nil {
		if os.IsNotExist(err) {
			return []Status{}, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(m.Root, "instances"))
	if os.IsNotExist(err) {
		return []Status{}, nil
	}
	if err != nil {
		return nil, err
	}
	list := []Status{}
	for _, entry := range entries {
		if !entry.IsDir() || !namePattern.MatchString(entry.Name()) {
			continue
		}
		i, err := m.load(entry.Name())
		if err != nil {
			list = append(list, Status{Instance: Instance{Name: entry.Name()}, Error: err.Error()})
			continue
		}
		list = append(list, m.status(ctx, i))
	}
	sort.Slice(list, func(a, b int) bool { return list[a].Name < list[b].Name })
	return list, nil
}
func (m *Manager) Logs(ctx context.Context, name string) (string, error) {
	i, err := m.load(name)
	if err != nil {
		return "", err
	}
	return m.driver(i).logs(ctx, i)
}
func (m *Manager) Exec(ctx context.Context, name string, args []string) (string, error) {
	i, err := m.load(name)
	if err != nil {
		return "", err
	}
	running, err := m.driver(i).running(ctx, i)
	if err != nil {
		return "", err
	}
	if !running {
		return "", fmt.Errorf("数据库实例尚未启动")
	}
	return m.driver(i).exec(ctx, i, args)
}
func (m *Manager) Connection(name string) (Connection, error) {
	unlock, err := m.lock(name)
	if err != nil {
		return Connection{}, err
	}
	defer unlock()
	i, err := m.load(name)
	if err != nil {
		return Connection{}, err
	}
	c, err := m.credentials(i)
	if err != nil {
		return Connection{}, err
	}
	user, db, scheme := "root", "app", i.Engine
	if i.Engine == "postgres" {
		user = "vsm"
	}
	if i.Engine == "redis" || i.Engine == "valkey" {
		user = "default"
		db = "0"
		scheme = "redis"
	}
	if i.Engine == "mariadb" {
		scheme = "mysql"
	}
	u := url.URL{Scheme: scheme, User: url.UserPassword(user, c.Password), Host: fmt.Sprintf("127.0.0.1:%d", i.Port), Path: "/" + db}
	if i.Engine == "postgres" {
		u.RawQuery = "sslmode=disable"
	}
	return Connection{Host: "127.0.0.1", Port: i.Port, User: user, Password: c.Password, Database: db, URI: u.String()}, nil
}
func waitReady(ctx context.Context, ready func() error) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		if err := ready(); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("数据库就绪超时: %w", last)
		case <-ticker.C:
		}
	}
}
