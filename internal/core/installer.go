package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"vsm/internal/plugin"
)

// Installer 负责下载并安装某工具的某个版本。
type Installer struct {
	Paths           Paths
	RequireChecksum bool
}

// ProgressFunc 用于回报安装过程中的进度信息。
type ProgressFunc func(msg string)

// Install 下载并安装 p 的 version。customDir 为空时装到默认 installs 目录；
// 非空时装到 customDir/<tool>/<version> 并在 installs 下建 junction 链接过去
// （这样既能自选磁盘位置，vsm 又能照常识别/切换/卸载）。
func (in Installer) Install(p plugin.Plugin, version, customDir string, progress ProgressFunc) error {
	if in.Paths.ProjectMode == "local" && customDir != "" && !pathWithin(in.Paths.ProjectRoot, customDir) {
		return fmt.Errorf("项目隔离模式不允许安装到项目目录外")
	}
	if err := validateInstall(p.Name(), version); err != nil {
		return err
	}
	if err := in.Paths.EnsureDirs(); err != nil {
		return err
	}
	guard, err := in.Paths.openInstalls(p.Name(), true)
	if err != nil {
		return err
	}
	guard.Close()
	if progress == nil {
		progress = func(string) {}
	}
	dest := in.Paths.InstallDir(p.Name(), version)
	installed, err := checkExisting(dest)
	if err != nil {
		return err
	}
	if installed {
		usable := false
		for _, name := range plugin.DetectCommands()[p.Name()] {
			if findExecutable(p.BinPaths(dest), name) != "" {
				usable = true
				break
			}
		}
		if !usable {
			return fmt.Errorf("%s %s 安装不完整，缺少主命令；请先卸载该损坏版本后重新安装", p.Name(), version)
		}
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
	sum, err := plugin.ChecksumFor(p, version, plugin.CurrentTarget(), url)
	if err != nil {
		return fmt.Errorf("获取官方完整性元数据失败（安装已停止）: %w", err)
	}
	if sum == "" && (in.RequireChecksum || os.Getenv("VSM_REQUIRE_CHECKSUM") == "1") {
		return fmt.Errorf("publisher checksum unavailable; --require-checksum refuses installation")
	}
	if sum != "" {
		decoded, err := hex.DecodeString(sum)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("invalid publisher SHA256 metadata")
		}
	}
	if sum == "" {
		progress("警告：发布方未提供 SHA256；仅检查传输长度、归档格式与解压边界。可用 --require-checksum 拒绝此类安装。")
	}
	url = plugin.MirrorURL(url)

	progress("下载 " + url)
	dlPath, err := downloadResolve(url, in.Paths.Downloads(), p.Name()+"-"+version, progress)
	if err != nil {
		return err
	}
	if sum != "" {
		progress("校验 sha256 …")
		if err := verifyChecksum(dlPath, sum); err != nil {
			os.Remove(dlPath)
			return err
		}
		progress("校验通过")
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
	validBinary := false
	for _, name := range plugin.DetectCommands()[p.Name()] {
		if findExecutable(p.BinPaths(root), name) != "" {
			validBinary = true
			break
		}
	}
	if !validBinary {
		return fmt.Errorf("archive does not contain the expected %s executable", p.Name())
	}
	if err := placeInstall(root, dest, customDir, p.Name(), version); err != nil {
		return err
	}
	if customDir != "" {
		progress("已装到自定义目录并链接: " + filepath.Join(customDir, p.Name(), version))
	}
	progress(fmt.Sprintf("已安装 %s %s", p.Name(), version))
	return nil
}

// installMarker 标识某目录由 vsm 安装到自定义位置（用于 junction 卸载时判定是否删真实目录）。
const installMarker = ".vsm-install"

// checkExisting 判断 dest 处是否已有可用的安装。
// 悬空链接（--path 指向的目录或纳管的系统目录已被删除）会被清理并视为未安装；
// 否则 `vsm install` 会一直报「已安装，跳过」，而这个版本根本用不了。
func checkExisting(dest string) (bool, error) {
	if _, err := os.Lstat(dest); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := os.Stat(dest); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if dirExists(dest) {
		return true, nil
	}
	if isLink(dest) {
		if err := os.Remove(dest); err != nil {
			return false, fmt.Errorf("清理失效链接 %s 失败: %w", dest, err)
		}
		return false, nil
	}
	return false, fmt.Errorf("%s 已存在但不是目录，请手动处理", dest)
}

// placeInstall 把解压好的 root 放到最终位置：默认装到 dest；
// 指定 customDir 时装到 customDir/<tool>/<version>，再在 dest 建链接指过去。
func placeInstall(root, dest, customDir, tool, version string) error {
	if err := validateInstall(tool, version); err != nil {
		return err
	}
	if customDir == "" {
		return moveInto(root, dest)
	}
	realDir := filepath.Join(customDir, tool, version)
	if pathWithin(realDir, dest) || pathWithin(dest, realDir) || pathWithin(root, realDir) || pathWithin(realDir, root) {
		return fmt.Errorf("installation directories must not overlap")
	}
	guard, err := (Paths{InstallsRoot: customDir}).openInstalls(tool, true)
	if err != nil {
		return err
	}
	guard.Close()
	if pathExists(realDir) {
		return fmt.Errorf("%s already exists; refusing to replace existing data", realDir)
	}
	// 标记在搬运之前写进 root，随安装文件一起就位：不会出现「文件在、标记缺」。
	// 缺标记时卸载只删链接，会把真实目录永久留在磁盘上（此前写标记的错误被直接忽略）。
	receipt, err := createReceipt(root, dest, realDir, tool, version)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(filepath.Join(filepath.Dir(dest), receiptName(version)))
		}
	}()
	if err := moveInto(root, realDir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := linkDir(realDir, dest); err != nil {
		cleanupErr := removeOwnedTarget(receipt)
		if cleanupErr != nil {
			return fmt.Errorf("link failed: %v; rollback failed: %w", err, cleanupErr)
		}
		return err
	}
	committed = true
	return nil
}

// renameFn / copyDirFn 供测试注入，模拟跨卷 rename 失败与复制中途失败。
var (
	renameFn  = os.Rename
	copyDirFn = copyDir
)

// moveInto 把 src 目录移动到 dst。同卷时 rename 本身是原子的；跨卷（如安装根目录在
// 另一块盘）时先完整复制到暂存目录，成功后再 rename 就位。此前直接复制进 dst，
// 中途失败会留下半成品，下次安装被 checkExisting 误判为「已安装，跳过」。
func moveInto(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := renameFn(src, dst)
	if err == nil {
		return nil
	}
	partial, tempErr := os.MkdirTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".partial-")
	if tempErr != nil {
		return tempErr
	}
	defer os.RemoveAll(partial)
	if cerr := copyDirFn(src, partial); cerr != nil {
		os.RemoveAll(partial)
		return fmt.Errorf("移动安装文件失败: %v / %v", err, cerr)
	}
	if rerr := renameFn(partial, dst); rerr != nil {
		os.RemoveAll(partial)
		return fmt.Errorf("移动安装文件失败: %w", rerr)
	}
	return nil
}

// Uninstall 删除某版本的安装目录。对“纳管的外部版本”（链接）只删除链接本身，
// 绝不递归删除系统里的真实目录。
func (in Installer) Uninstall(tool, version string) error {
	if err := validateInstall(tool, version); err != nil {
		return err
	}
	guard, err := in.Paths.openInstalls(tool, false)
	if err != nil {
		return err
	}
	defer guard.Close()
	dest := in.Paths.InstallDir(tool, version)
	if isLink(dest) {
		// 用 Readlink 解析 junction 目标（EvalSymlinks 对部分 Windows junction 会失败）。
		target, _ := os.Readlink(dest)
		if target == "" {
			if r, err := filepath.EvalSymlinks(dest); err == nil {
				target = r
			}
		}
		if target != "" && !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(dest), target)
		}
		r, owned, err := ownedReceipt(dest, target, tool, version)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("refusing unsafe uninstall: %w", err)
		}
		// 删链接本身；删不掉就到此为止，不能在链接还在的情况下去删真实目录
		if err := guard.Remove(version); err != nil {
			return fmt.Errorf("删除链接 %s 失败: %w", dest, err)
		}
		// 仅当目标带 vsm 安装标记（= 自定义目录安装）时才删真实目录；
		// 纳管的系统目录无标记，绝不删除。
		if owned {
			if err := removeOwnedTarget(r); err != nil {
				return err
			}
			if err := guard.Remove(receiptName(version)); err != nil {
				return err
			}
		}
		return nil
	}
	if !dirExists(dest) {
		return fmt.Errorf("%s %s 未安装", tool, version)
	}
	return guard.RemoveAll(version)
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
	source, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	targetRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer targetRoot.Close()
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return targetRoot.MkdirAll(rel, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, err := source.Readlink(rel)
			if err != nil {
				return err
			}
			return archiveSymlink(targetRoot, rel, link)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported installation file: %s", path)
		}
		in, err := source.Open(rel)
		if err != nil {
			return err
		}
		err = writeArchiveFile(targetRoot, rel, in, info.Mode(), info.Size())
		closeErr := in.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
