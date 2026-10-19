package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vsm/internal/core"
)

func (m *Manager) programLock(tool, version string) (func(), error) {
	if _, err := engine(tool); err != nil {
		return nil, err
	}
	if !versionPattern.MatchString(version) || strings.Contains(version, "..") {
		return nil, fmt.Errorf("无效数据库版本")
	}
	if err := m.ensure("program-locks"); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(filepath.Join(m.Root, "program-locks"))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	name := tool + "-" + version + ".lock"
	if _, err := r.Readlink(name); err == nil {
		return nil, fmt.Errorf("数据库程序锁不能是链接")
	}
	f, err := r.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	unlock, err := lockDatabase(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return func() { unlock(); f.Close() }, nil
}
func (m *Manager) guardUninstall(ctx context.Context, tool, version string) (func(), error) {
	programUnlock, err := m.programLock(tool, version)
	if err != nil {
		return nil, err
	}
	locks := []func(){programUnlock}
	release := func() {
		for n := len(locks) - 1; n >= 0; n-- {
			locks[n]()
		}
	}
	entries, err := os.ReadDir(filepath.Join(m.Root, "instances"))
	if os.IsNotExist(err) {
		return release, nil
	}
	if err != nil {
		release()
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !namePattern.MatchString(entry.Name()) {
			continue
		}
		i, err := m.load(entry.Name())
		if err != nil {
			release()
			return nil, err
		}
		if i.Engine != tool || i.Backend != "native" || i.Versions[version] == nil {
			continue
		}
		unlock, err := m.lock(i.Name)
		if err != nil {
			release()
			return nil, err
		}
		locks = append(locks, unlock)
		i, err = m.load(i.Name)
		if err != nil {
			release()
			return nil, err
		}
		if i.Versions[version] == nil {
			continue
		}
		i.Version = version
		running, err := m.driver(i).running(ctx, i)
		if err != nil || running {
			release()
			if err != nil {
				return nil, fmt.Errorf("无法确认实例 %s 已停止，保留数据库程序: %w", i.Name, err)
			}
			return nil, fmt.Errorf("实例 %s 正在使用 %s %s，请先执行 vsm db stop %s", i.Name, tool, version, i.Name)
		}
	}
	return release, nil
}

// GuardUninstall coordinates public CLI/GUI uninstall with native server startup.
func GuardUninstall(paths core.Paths, tool, version string) (func(), error) {
	if _, err := engine(tool); err != nil || paths.ProjectMode == "local" || paths.ProjectMode == "docker" {
		return func() {}, nil
	}
	global, err := core.DefaultPaths()
	if err != nil {
		return nil, err
	}
	left, lerr := os.Stat(paths.Installs())
	right, rerr := os.Stat(global.Installs())
	if lerr == nil && rerr == nil && !os.SameFile(left, right) {
		return func() {}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return New(global).guardUninstall(ctx, tool, version)
}
