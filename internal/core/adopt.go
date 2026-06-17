package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"vsm/internal/plugin"
)

// Adopt 把系统中已存在的某语言运行时“纳管”进 vsm：在 installs/<tool>/<version>
// 处创建一个指向其真实安装根目录的链接（Windows 用 junction，其它平台用符号链接），
// 之后即可像 vsm 自己安装的版本一样 use / exec / 卸载。卸载只删除链接本身，
// 绝不会动到系统里的真实目录（见 Installer.Uninstall 的链接保护）。
//
// execPath 是系统检测得到的主命令可执行文件路径（如 C:\Java\jdk-21\bin\java.exe）。
func (in Installer) Adopt(p plugin.Plugin, version, execPath string) error {
	if version == "" {
		return fmt.Errorf("纳管 %s 需要版本号", p.Name())
	}
	root, err := inferInstallRoot(p, execPath)
	if err != nil {
		return err
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
	binDir := filepath.Dir(execPath)
	for _, cand := range []string{binDir, filepath.Dir(binDir)} {
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

// linkDir 创建目录链接 link → target。Windows 优先用 junction（mklink /J，
// 免管理员权限），失败再退回符号链接；其它平台直接用符号链接。
func linkDir(target, link string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
		if out, err := cmd.CombinedOutput(); err != nil {
			if serr := os.Symlink(target, link); serr != nil {
				return fmt.Errorf("创建 junction 失败: %v (%s)；符号链接亦失败: %v",
					err, strings.TrimSpace(string(out)), serr)
			}
		}
		return nil
	}
	return os.Symlink(target, link)
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

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
