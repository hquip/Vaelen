package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Extract 根据文件后缀自动选择解压方式（支持 .tar.gz/.tgz 与 .zip），
// 将 src 解压到 dst 目录。
func Extract(src, dst string) error {
	lower := strings.ToLower(src)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(src, dst)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(src, dst)
	case strings.HasSuffix(lower, ".tar.xz"):
		return extractTarXz(src, dst)
	default:
		return fmt.Errorf("不支持的压缩格式: %s", src)
	}
}

func extractTarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dst, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(target, tr, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)
			// 链接目标同样要限制在解压目录内：否则归档里可以先放一个
			// 指向外部的软链，再用后续条目通过它写到目标目录之外。
			if !linkTargetInside(dst, filepath.Dir(target), hdr.Linkname) {
				return fmt.Errorf("压缩包内存在越界符号链接: %s -> %s", hdr.Name, hdr.Linkname)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				// 某些平台（如无权限的 Windows）创建符号链接失败可忽略。
				continue
			}
		}
	}
	return nil
}

// extractTarXz 用系统 tar 解压 .tar.xz（zig / typst 等在 Linux/macOS 的发行格式）。
// 类 Unix 自带 tar；Windows 10 1803+ 也内置 bsdtar(tar.exe)。tar 会自动识别 xz 压缩，
// 无需显式 -J，兼容 GNU tar 与 BSD/macOS/Windows 的 bsdtar。这样无需引入第三方 xz 库，
// 仍保持 CLI 零第三方依赖。
func extractTarXz(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if _, err := exec.LookPath("tar"); err != nil {
		return fmt.Errorf(".tar.xz 需要系统 tar 解压，但未找到 tar 命令（%s）", src)
	}
	cmd := exec.Command("tar", "-xf", src, "-C", dst)
	hideConsole(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("解压 .tar.xz 失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func extractZip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, file := range r.File {
		target, err := safeJoin(dst, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, file.Mode())
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFile(target string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

// safeJoin 防御 zip-slip / 路径穿越：确保解出的路径仍在 dst 之内。
func safeJoin(dst, name string) (string, error) {
	cleanRoot := filepath.Clean(dst)
	// 归档内条目为绝对路径时（/etc/passwd 或 C:\evil），filepath.Join 会把它
	// 相对化为 dst/etc/passwd，看似安全但已扭曲原意；直接拒绝更明确。
	if filepath.IsAbs(name) || (len(name) > 1 && name[1] == ':') {
		return "", fmt.Errorf("压缩包内存在绝对路径: %s", name)
	}
	target := filepath.Join(cleanRoot, name)
	if target != cleanRoot && !strings.HasPrefix(target, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("压缩包内存在非法路径: %s", name)
	}
	return target, nil
}

// linkTargetInside 判断软链（位于 linkDir 下、指向 linkTarget）解析后是否仍落在 root 内。
func linkTargetInside(root, linkDir, linkTarget string) bool {
	var resolved string
	if filepath.IsAbs(linkTarget) {
		resolved = filepath.Clean(linkTarget)
	} else {
		resolved = filepath.Clean(filepath.Join(linkDir, linkTarget))
	}
	cleanRoot := filepath.Clean(root)
	return resolved == cleanRoot || strings.HasPrefix(resolved, cleanRoot+string(os.PathSeparator))
}
