package core

import (
	"fmt"
	"os"
	"path/filepath"

	"vsm/internal/plugin"
)

// Installer 负责下载并安装某工具的某个版本。
type Installer struct {
	Paths Paths
}

// ProgressFunc 用于回报安装过程中的进度信息。
type ProgressFunc func(msg string)

// Install 下载并安装 p 的 version。customDir 为空时装到默认 installs 目录；
// 非空时装到 customDir/<tool>/<version> 并在 installs 下建 junction 链接过去
// （这样既能自选磁盘位置，vsm 又能照常识别/切换/卸载）。
func (in Installer) Install(p plugin.Plugin, version, customDir string, progress ProgressFunc) error {
	if progress == nil {
		progress = func(string) {}
	}
	dest := in.Paths.InstallDir(p.Name(), version)
	if pathExists(dest) {
		progress(fmt.Sprintf("%s %s 已安装，跳过", p.Name(), version))
		return nil
	}
	if err := in.Paths.EnsureDirs(); err != nil {
		return err
	}

	url, err := p.DownloadURL(version, plugin.CurrentTarget())
	if err != nil {
		return err
	}
	url = plugin.MirrorURL(url)

	progress("下载 " + url)
	dlPath, err := downloadResolve(url, in.Paths.Downloads(), p.Name()+"-"+version, progress)
	if err != nil {
		return err
	}
	if cs, ok := p.(plugin.Checksummer); ok {
		if sum, e := cs.Checksum(version, plugin.CurrentTarget()); e == nil && sum != "" {
			progress("校验 sha256 …")
			if err := verifyChecksum(dlPath, sum); err != nil {
				return err
			}
			progress("校验通过")
		}
	}

	tmp, err := os.MkdirTemp(in.Paths.Root, "extract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	progress("解压中 ...")
	if err := Extract(dlPath, tmp); err != nil {
		return err
	}

	// 官方包通常带一层顶层目录（如 go/ 或 node-vX-os-arch/），剥掉它。
	root := stripSingleDir(tmp)
	if customDir == "" {
		if err := moveInto(root, dest); err != nil {
			return err
		}
	} else {
		realDir := filepath.Join(customDir, p.Name(), version)
		if err := moveInto(root, realDir); err != nil {
			return err
		}
		// 标记为 vsm 安装：卸载时连真实目录一起删（纳管的系统目录无此标记，只删链接）。
		_ = os.WriteFile(filepath.Join(realDir, installMarker), []byte("vsm\n"), 0o644)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := linkDir(realDir, dest); err != nil {
			return err
		}
		progress("已装到自定义目录并链接: " + realDir)
	}
	progress(fmt.Sprintf("已安装 %s %s", p.Name(), version))
	return nil
}

// installMarker 标识某目录由 vsm 安装到自定义位置（用于 junction 卸载时判定是否删真实目录）。
const installMarker = ".vsm-install"

// moveInto 把 src 目录移动到 dst（跨卷 rename 失败时退回递归复制）。
func moveInto(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		if cerr := copyDir(src, dst); cerr != nil {
			return fmt.Errorf("移动安装文件失败: %v / %v", err, cerr)
		}
	}
	return nil
}

// Uninstall 删除某版本的安装目录。对“纳管的外部版本”（链接）只删除链接本身，
// 绝不递归删除系统里的真实目录。
func (in Installer) Uninstall(tool, version string) error {
	dest := in.Paths.InstallDir(tool, version)
	if isLink(dest) {
		// 用 Readlink 解析 junction 目标（EvalSymlinks 对部分 Windows junction 会失败）。
		target, _ := os.Readlink(dest)
		if target == "" {
			if r, err := filepath.EvalSymlinks(dest); err == nil {
				target = r
			}
		}
		_ = os.Remove(dest) // 删链接本身
		// 仅当目标带 vsm 安装标记（= 自定义目录安装）时才删真实目录；
		// 纳管的系统目录无标记，绝不删除。
		if target != "" && fileExists(filepath.Join(target, installMarker)) {
			return os.RemoveAll(target)
		}
		return nil
	}
	if !dirExists(dest) {
		return fmt.Errorf("%s %s 未安装", tool, version)
	}
	return os.RemoveAll(dest)
}

// 下载实现（分块并行 / 进度 / 重试 / sha256 校验）见 download.go。

// stripSingleDir 若 dir 下只有一个子目录，返回该子目录路径；否则返回 dir。
func stripSingleDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(dir, entries[0].Name())
	}
	return dir
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		return writeFile(target, in, info.Mode())
	})
}
