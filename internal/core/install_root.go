package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vsm/internal/config"
	"vsm/internal/logging"
	"vsm/internal/plugin"
)

// SetInstallRoot changes the lookup root, never moves existing runtimes. Refuse
// to hide working global selections behind an empty or incomplete new root.
func (p Paths) SetInstallRoot(dir string) (next Paths, err error) {
	next = p
	fields := logging.Fields{"old_install_root": p.Installs(), "requested_root": dir}
	finish := logging.Start("activation", "install-root.change", fields)
	defer func() { finish(err) }()
	if p.ProjectMode == "local" || p.ProjectMode == "docker" {
		return next, fmt.Errorf("项目隔离环境使用固定目录，不修改全局安装根目录")
	}
	if dir = strings.TrimSpace(dir); dir == "" {
		next.InstallsRoot = ""
	} else {
		next.InstallsRoot, err = filepath.Abs(dir)
		if err != nil {
			return p, err
		}
	}
	if err := next.ValidateInstallRoot(next.Installs()); err != nil {
		return p, err
	}
	if err := p.checkInstallRootChange(next); err != nil {
		return p, err
	}
	if err := p.EnsureDirs(); err != nil {
		return p, err
	}
	if err := next.EnsureDirs(); err != nil {
		return p, err
	}
	if next.InstallsRoot == "" {
		if err := os.Remove(p.InstallRootFile()); err != nil && !os.IsNotExist(err) {
			return p, fmt.Errorf("恢复默认安装目录失败: %w", err)
		}
	} else if err := config.WriteFileAtomic(p.InstallRootFile(), []byte(next.InstallsRoot+"\n"), 0644); err != nil {
		return p, err
	}
	logging.Record("info", "activation", "install-root.changed", logging.Fields{"old_install_root": p.Installs(), "install_root": next.Installs(), "files_migrated": false})
	return next, nil
}

func (p Paths) checkInstallRootChange(next Paths) error {
	if sameDir(p.Installs(), next.Installs()) {
		return nil
	}
	global, err := config.Load(p.GlobalToolVersions())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	current := Activator{Paths: p, Registry: Registry{Paths: p}}
	current.Resolver = Resolver{Paths: p, Registry: current.Registry}
	target := Activator{Paths: next, Registry: Registry{Paths: next}}
	target.Resolver = Resolver{Paths: next, Registry: target.Registry}
	var missing []string
	for _, tool := range global.Tools() {
		if _, ok := plugin.Get(tool); !ok || plugin.IsServiceTool(tool) {
			continue
		}
		spec, _ := global.Get(tool)
		// Pin each global requirement so project/env overrides cannot hide a
		// dependency that will break in a different shell or project later.
		current.Resolver.Pinned = map[string]string{tool: spec}
		target.Resolver.Pinned = map[string]string{tool: spec}
		if _, err := current.Which(tool, p.Root); err != nil {
			continue // Allow repairing an already-broken lookup root.
		}
		if _, err := target.Which(tool, next.Root); err != nil {
			missing = append(missing, tool+" "+spec)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	logging.Record("warn", "activation", "install-root.change.blocked", logging.Fields{"old_install_root": p.Installs(), "install_root": next.Installs(), "missing_global_versions": missing, "files_migrated": false})
	return fmt.Errorf("安装目录未更改：目标目录缺少当前全局版本 %s。原目录：%s；目标目录：%s。更改查找目录不会迁移已有安装，请先在目标目录准备对应版本，或继续使用原目录", strings.Join(missing, "、"), p.Installs(), next.Installs())
}
