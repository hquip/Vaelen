package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveLimits bounds expansion before writing file contents to disk.
type ArchiveLimits struct {
	MaxFiles                    int
	MaxFileBytes, MaxTotalBytes int64
}

var defaultArchiveLimits = ArchiveLimits{100000, 4 << 30, 16 << 30}

// Extract confines every filesystem operation to dst, including symlink traversal.
func Extract(src, dst string) error { return ExtractWithLimits(src, dst, defaultArchiveLimits) }

func ExtractWithLimits(src, dst string, limits ArchiveLimits) error {
	if limits.MaxFiles <= 0 || limits.MaxFileBytes <= 0 || limits.MaxTotalBytes <= 0 {
		return fmt.Errorf("invalid archive limits")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	b := archiveBudget{limits: limits}
	switch lower := strings.ToLower(src); {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
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
		if err := extractTar(tar.NewReader(gz), root, &b); err != nil {
			return err
		}
		// Read through the trailer so a corrupt gzip checksum cannot be ignored.
		_, err = io.Copy(io.Discard, io.LimitReader(gz, 1<<20))
		return err
	case strings.HasSuffix(lower, ".zip"):
		return extractZipRoot(src, root, &b)
	case strings.HasSuffix(lower, ".tar.xz"):
		return extractTarXzRoot(src, root, &b)
	default:
		return fmt.Errorf("不支持的压缩格式: %s", src)
	}
}

type archiveBudget struct {
	limits ArchiveLimits
	files  int
	bytes  int64
}

func (b *archiveBudget) add(size int64) error {
	b.files++
	if size < 0 || b.files > b.limits.MaxFiles || size > b.limits.MaxFileBytes || size > b.limits.MaxTotalBytes-b.bytes {
		return fmt.Errorf("archive exceeds file count or expanded size limits")
	}
	b.bytes += size
	return nil
}

func archiveName(name string) (string, error) {
	if strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("非法归档路径: %q", name)
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", fmt.Errorf("归档路径越界: %q", name)
		}
		if component != "" && component != "." {
			if err := validateComponent(component); err != nil {
				return "", err
			}
		}
	}
	clean := path.Clean(name)
	if clean == "." {
		return ".", nil
	}
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("非法归档路径: %q", name)
	}
	return filepath.FromSlash(clean), nil
}

func extractTar(tr *tar.Reader, root *os.Root, budget *archiveBudget) error {
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name, err := archiveName(h.Name)
		if err != nil {
			return err
		}
		if err := budget.add(h.Size); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeArchiveFile(root, name, tr, os.FileMode(h.Mode), h.Size); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := archiveSymlink(root, name, h.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			target, err := archiveName(h.Linkname)
			if err != nil {
				return err
			}
			fi, err := root.Lstat(target)
			if err != nil {
				return err
			}
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("hard link target must be a regular file")
			}
			if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return err
			}
			if err := root.Link(target, name); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type %d: %s", h.Typeflag, h.Name)
		}
	}
}

func archiveSymlink(root *os.Root, name, target string) error {
	if strings.ContainsAny(target, "\\:\x00") || path.IsAbs(target) || !linkTargetInside(".", filepath.Dir(name), target) {
		return fmt.Errorf("压缩包内存在越界符号链接: %s -> %s", name, target)
	}
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	// Never replace existing entries, and never silently discard a failed link.
	return root.Symlink(filepath.FromSlash(target), name)
}

func extractZipRoot(src string, root *os.Root, budget *archiveBudget) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name, err := archiveName(f.Name)
		if err != nil {
			return err
		}
		if f.UncompressedSize64 > uint64(budget.limits.MaxFileBytes) {
			return fmt.Errorf("archive file too large: %s", f.Name)
		}
		if err := budget.add(int64(f.UncompressedSize64)); err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&os.ModeType != 0 && f.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("unsupported zip entry: %s", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if f.Mode()&os.ModeSymlink != 0 {
			var target []byte
			target, err = io.ReadAll(io.LimitReader(rc, 4097))
			if err == nil && len(target) > 4096 {
				err = fmt.Errorf("symlink target too long")
			}
			if err == nil {
				err = archiveSymlink(root, name, string(target))
			}
		} else {
			err = writeArchiveFile(root, name, rc, f.Mode(), int64(f.UncompressedSize64))
		}
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func writeArchiveFile(root *os.Root, name string, r io.Reader, mode os.FileMode, size int64) error {
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	// O_EXCL prevents duplicate entries and writes through existing links/hard links.
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()&0o755)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(r, size+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != size {
		return fmt.Errorf("archive size mismatch for %s: got %d want %d", name, n, size)
	}
	return nil
}

// XZ is decoded to a tar stream only; external tools never extract paths to disk.
func extractTarXzRoot(src string, root *os.Root, budget *archiveBudget) error {
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var cmd *exec.Cmd
	if xz, err := exec.LookPath("xz"); err == nil {
		cmd = exec.CommandContext(ctx, xz, "-dc", "--", abs)
	} else {
		// macOS/Windows bsdtar supports @archive inclusion in create mode.
		cmd = exec.CommandContext(ctx, "tar", "-c", "-f", "-", "@"+abs)
	}
	hideConsole(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	extractErr := extractTar(tar.NewReader(pipe), root, budget)
	if extractErr != nil {
		cancel()
	} else {
		_, extractErr = io.Copy(io.Discard, io.LimitReader(pipe, 1<<20))
		if extractErr != nil {
			cancel()
		}
	}
	pipe.Close()
	waitErr := cmd.Wait()
	if extractErr != nil {
		return extractErr
	}
	if waitErr != nil {
		return fmt.Errorf("XZ decode failed: %w: %s", waitErr, stderr.String())
	}
	return nil
}

func linkTargetInside(root, linkDir, target string) bool {
	if filepath.IsAbs(target) {
		return false
	}
	rel, err := filepath.Rel(root, filepath.Join(linkDir, target))
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}
