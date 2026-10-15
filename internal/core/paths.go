// Package core 实现版本管理器的核心引擎：磁盘布局、下载安装、版本解析、
// 已装版本索引与激活（PATH/env/shim）。所有前端（CLI/TUI/GUI）都复用本包。
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirName 是默认数据目录名（位于用户主目录下）。
const DirName = ".vsm"

// Paths 描述 VSM 的磁盘布局，所有版本互相隔离地存放在 installs/<tool>/<version>。
type Paths struct {
	Root string
	// InstallsRoot 可选：自定义“全局安装根目录”（默认 Root/installs）。改它即可把所有版本
	// 装到别的磁盘/目录（如 D:\vsm-installs 省 C 盘空间）。
	InstallsRoot string
}

// DefaultPaths 解析数据根目录，优先使用环境变量 VSM_HOME，否则用 ~/.vsm。
// 同时读取可选的 install-root 配置文件作为自定义全局安装根目录。
func DefaultPaths() (Paths, error) {
	var root string
	if env := os.Getenv("VSM_HOME"); env != "" {
		root = env
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(home, DirName)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{Root: abs}
	if data, err := os.ReadFile(filepath.Join(p.Root, "install-root")); err == nil {
		if d := strings.TrimSpace(string(data)); d != "" {
			if !filepath.IsAbs(d) {
				d = filepath.Join(p.Root, d)
			}
			p.InstallsRoot = d
		}
	} else if !os.IsNotExist(err) {
		return Paths{}, err
	}
	if err := p.ValidateInstallRoot(p.Installs()); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// InstallRootFile 返回自定义全局安装根目录的配置文件路径。
func (p Paths) InstallRootFile() string { return filepath.Join(p.Root, "install-root") }

// Installs 返回所有安装包的根目录（默认 Root/installs，可被 InstallsRoot 覆盖）。
func (p Paths) Installs() string {
	if p.InstallsRoot != "" {
		return p.InstallsRoot
	}
	return filepath.Join(p.Root, "installs")
}

// ToolInstalls 返回某工具所有版本的目录。
func (p Paths) ToolInstalls(tool string) string { return filepath.Join(p.Installs(), tool) }

// InstallDir 返回某工具某版本的安装目录。
func (p Paths) InstallDir(tool, version string) string {
	return filepath.Join(p.ToolInstalls(tool), version)
}

// Shims 返回垫片目录（无 shell hook 环境下的兜底激活机制）。
func (p Paths) Shims() string { return filepath.Join(p.Root, "shims") }

// Downloads 返回下载缓存目录。
func (p Paths) Downloads() string { return filepath.Join(p.Root, "downloads") }

// ConfigFile 返回全局配置（含全局默认版本）路径。
func (p Paths) ConfigFile() string { return filepath.Join(p.Root, "config.toml") }

// GlobalToolVersions 返回全局 .tool-versions 路径。
func (p Paths) GlobalToolVersions() string { return filepath.Join(p.Root, ".tool-versions") }

// EnsureDirs 创建基础目录结构。
func (p Paths) EnsureDirs() error {
	if err := p.ValidateInstallRoot(p.Installs()); err != nil {
		return err
	}
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		return err
	}
	r, err := os.OpenRoot(p.Root)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, name := range []string{"shims", "downloads"} {
		if err := r.Mkdir(name, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		fi, err := r.Lstat(name)
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s must be an isolated directory, not a link", name)
		}
		if _, err := r.Readlink(name); err == nil {
			return fmt.Errorf("%s cannot be a junction", name)
		}
	}
	if pathWithin(p.Root, p.Installs()) {
		rel, err := filepath.Rel(p.Root, p.Installs())
		if err != nil {
			return err
		}
		if err := r.MkdirAll(rel, 0o755); err != nil {
			return err
		}
		fi, err := r.Lstat(rel)
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("install root cannot be a link")
		}
		if _, err := r.Readlink(rel); err == nil {
			return fmt.Errorf("install root cannot be a junction")
		}
		return nil
	}
	return os.MkdirAll(p.Installs(), 0o755)
}

func (p Paths) ValidateInstallRoot(dir string) error {
	abs, err := canonicalPath(dir)
	if err != nil {
		return err
	}
	root, err := canonicalPath(p.Root)
	if err != nil {
		return err
	}
	if filepath.Dir(abs) == abs || pathWithin(abs, root) {
		return fmt.Errorf("install root must not be a filesystem root or contain VSM_HOME")
	}
	for _, name := range []string{"shims", "downloads", "plugins"} {
		d := filepath.Join(root, name)
		if pathWithin(d, abs) || pathWithin(abs, d) {
			return fmt.Errorf("install root overlaps vsm %s directory", name)
		}
	}
	return nil
}
