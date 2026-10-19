package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"vsm/internal/logging"
	"vsm/internal/plugin"
)

func (in Installer) AdoptPath(tool, path, alias string) (SystemVersion, error) {
	if in.Paths.ProjectMode == "docker" {
		return SystemVersion{}, ErrDockerHostInstall
	}
	if in.Paths.ProjectMode == "local" {
		return SystemVersion{}, fmt.Errorf("本地隔离模式不链接共享工具，请使用 versions 模式")
	}
	s, err := InspectToolPath(tool, path)
	if err != nil {
		return SystemVersion{}, err
	}
	if alias != "" {
		if err := validateInstall(tool, alias); err != nil {
			return SystemVersion{}, err
		}
		s.ManagedVersion = alias
	}
	p, _ := plugin.Get(tool)
	return s, in.Adopt(p, s.RegistryVersion(), s.Path)
}

// Adopt 把系统中已存在的某语言运行时“纳管”进 vsm：在 installs/<tool>/<version>
// 处创建一个指向其真实安装根目录的链接（Windows 用 junction，其它平台用符号链接），
// 之后即可像 vsm 自己安装的版本一样 use / exec / 卸载。卸载只删除链接本身，
// 绝不会动到系统里的真实目录（见 Installer.Uninstall 的链接保护）。
//
// execPath 是系统检测得到的主命令可执行文件路径（如 C:\Java\jdk-21\bin\java.exe）。
func (in Installer) Adopt(p plugin.Plugin, version, execPath string) (err error) {
	finish := logging.Start("core", "adopt", logging.Fields{"tool": p.Name(), "version": version, "mode": in.Paths.ProjectMode})
	defer func() { finish(err) }()
	if in.Paths.ProjectMode == "docker" {
		return ErrDockerHostInstall
	}
	if in.Paths.ProjectMode == "local" {
		return fmt.Errorf("本地隔离模式不链接共享工具；请在项目中独立安装，或使用 versions 模式")
	}
	if err := validateInstall(p.Name(), version); err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("纳管 %s 需要版本号", p.Name())
	}
	root, err := inferInstallRoot(p, execPath)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(execPath); err != nil {
		return err
	} else if !fi.Mode().IsRegular() {
		return fmt.Errorf("adopt requires a regular executable file")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	guard, err := in.Paths.openInstalls(p.Name(), true)
	if err != nil {
		return err
	}
	guard.Close()
	if pathWithin(in.Paths.Root, root) || pathWithin(root, in.Paths.Root) || pathWithin(in.Paths.Installs(), root) {
		return fmt.Errorf("adopt target overlaps vsm directories")
	}
	if err := in.Paths.EnsureDirs(); err != nil {
		return err
	}
	dest := in.Paths.InstallDir(p.Name(), version)
	if pathExists(dest) {
		return fmt.Errorf("%s %s 已存在于 vsm，无需纳管", p.Name(), version)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return linkDir(root, dest)
}

// inferInstallRoot 由可执行文件路径反推该版本的“安装根目录”，使得
// plugin.BinPaths(root) 恰好覆盖该可执行文件所在目录（兼容 bin 在根目录或
// 在 <root>/bin 两种常见布局）。
func inferInstallRoot(p plugin.Plugin, execPath string) (string, error) {
	if execPath == "" {
		return "", fmt.Errorf("缺少可执行文件路径")
	}
	// System package managers often link /usr/bin/java or mvn to another root.
	// Python venv launchers are the exception: their surrounding environment is
	// significant even when the executable links to a shared base interpreter.
	keepPythonPrefix := false
	if p.Name() == "python" {
		root := filepath.Dir(execPath)
		if strings.EqualFold(filepath.Base(root), "bin") || strings.EqualFold(filepath.Base(root), "Scripts") {
			root = filepath.Dir(root)
		}
		keepPythonPrefix = plugin.CondaPrefix(root) != "" || fileExists(filepath.Join(root, "pyvenv.cfg"))
	}
	if !keepPythonPrefix {
		execPath = realPath(execPath)
	}
	binDir := filepath.Dir(execPath)
	candidates := []string{binDir, filepath.Dir(binDir)}
	if strings.EqualFold(filepath.Base(binDir), "bin") || strings.EqualFold(filepath.Base(binDir), "Scripts") {
		candidates = []string{filepath.Dir(binDir), binDir}
	}
	for _, cand := range candidates {
		for _, bp := range p.BinPaths(cand) {
			if sameDir(bp, binDir) {
				return cand, nil
			}
		}
	}
	// 退路：直接用可执行文件所在目录（适配 BinPaths 直接返回根目录的插件）。
	return binDir, nil
}

func sameDir(a, b string) bool {
	ca, _ := filepath.Abs(filepath.Clean(a))
	cb, _ := filepath.Abs(filepath.Clean(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// linkDir 创建目录链接 link → target。Windows 直接通过原生 API 创建 junction，
// 避免合法路径中的 shell 元字符被解释；其它平台使用符号链接。
func linkDir(target, link string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	return platformLinkDir(abs, link)
}

// isLink 判断 path 是否为符号链接 / Windows junction（reparse point）。
// 用于卸载时区分“纳管的外部版本”（只删链接）与“vsm 自己安装的真实目录”（递归删除）。
func isLink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	// 部分 Go 版本把 Windows junction 标记为 Irregular：用 Readlink 兜底判定。
	if _, err := os.Readlink(path); err == nil {
		return true
	}
	return false
}

// InstallTarget 返回版本目录的真实位置，以及它是否归 vsm 所有：
//   - vsm 直接安装的目录：返回自身，owned=true；
//   - --path 自定义安装（匹配 registry 记录及目录标记）：返回真实目录，owned=true；
//   - 纳管的系统版本（链接、无标记）：返回系统目录，owned=false。
//
// 与 Installer.Uninstall 的判定规则一致，供 size 等命令区分「vsm 占用的空间」。
func InstallTarget(dir string) (real string, owned bool) {
	if !isLink(dir) {
		return dir, true
	}
	target, _ := os.Readlink(dir)
	if target == "" {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			target = r
		}
	}
	if target == "" {
		return dir, false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dir), target)
	}
	_, owned, err := ownedReceipt(dir, target, filepath.Base(filepath.Dir(dir)), filepath.Base(dir))
	return target, owned && err == nil
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
