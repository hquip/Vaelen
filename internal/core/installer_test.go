package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertNoPartialInstall(t *testing.T, dst string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+filepath.Base(dst)+".partial") {
			t.Fatalf("staging directory leaked: %s", entry.Name())
		}
	}
}

func mkTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// withCrossVolume 让第一次 rename（src → dst）失败，模拟安装根目录在另一块盘。
func withCrossVolume(t *testing.T) {
	t.Helper()
	orig := renameFn
	first := true
	renameFn = func(a, b string) error {
		if first {
			first = false
			return errors.New("invalid cross-device link")
		}
		return orig(a, b)
	}
	t.Cleanup(func() { renameFn = orig })
}

// TestMoveIntoCrossVolumeIsAtomic 跨卷复制中途失败时，dst 不能出现半成品
// （否则下次安装会被判为「已安装，跳过」），暂存目录也要清理掉。
func TestMoveIntoCrossVolumeIsAtomic(t *testing.T) {
	src := t.TempDir()
	mkTree(t, src, map[string]string{"bin/go": "x", "VERSION": "1.22.0"})
	dst := filepath.Join(t.TempDir(), "go", "1.22.0")

	withCrossVolume(t)
	origCopy := copyDirFn
	copyDirFn = func(a, b string) error {
		// 复制了一部分后失败
		mkTree(t, b, map[string]string{"bin/go": "x"})
		return errors.New("disk full")
	}
	t.Cleanup(func() { copyDirFn = origCopy })

	if err := moveInto(src, dst); err == nil {
		t.Fatal("复制失败时应返回错误")
	}
	if pathExists(dst) {
		t.Fatal("失败后 dst 不应存在（半成品会被误判为已安装）")
	}
	assertNoPartialInstall(t, dst)
}

func TestMoveIntoCrossVolumeSucceeds(t *testing.T) {
	src := t.TempDir()
	mkTree(t, src, map[string]string{"bin/go": "x", "VERSION": "1.22.0"})
	dst := filepath.Join(t.TempDir(), "go", "1.22.0")
	withCrossVolume(t)
	if err := moveInto(src, dst); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dst, "bin", "go")) || !fileExists(filepath.Join(dst, "VERSION")) {
		t.Fatal("跨卷复制后文件应完整就位")
	}
	assertNoPartialInstall(t, dst)
}

// TestRegistryIgnoresPartialDirs 中断留下的 .xxx.partial 不能被当成已装版本。
func TestRegistryIgnoresPartialDirs(t *testing.T) {
	root := t.TempDir()
	p := Paths{Root: root}
	mkTree(t, p.ToolInstalls("go"), map[string]string{
		"1.22.0/bin/go":          "x",
		".1.23.0.partial/bin/go": "x",
	})
	got, err := Registry{Paths: p}.Installed("go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "1.22.0" {
		t.Fatalf("应只识别 1.22.0，实际 %v", got)
	}
}

// TestCheckExistingRemovesDanglingLink --path 指向的目录被删掉后，残留链接不能
// 让 `vsm install` 永远「已安装，跳过」。
func TestCheckExistingRemovesDanglingLink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "installs", "go", "1.22.0")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(target, link); err != nil {
		t.Fatalf("创建目录链接失败（Windows junction 无需管理员权限，失败即为回归）: %v", err)
	}
	if ok, err := checkExisting(link); err != nil || !ok {
		t.Fatalf("目标存在时应视为已安装，实际 ok=%v err=%v", ok, err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	ok, err := checkExisting(link)
	if err != nil || ok {
		t.Fatalf("悬空链接应被清理并视为未安装，实际 ok=%v err=%v", ok, err)
	}
	if pathExists(link) {
		t.Fatal("悬空链接应已被删除")
	}
}

// TestPlaceInstallCustomDirWritesMarker 自定义目录安装必须带上 vsm 标记，
// 卸载时才会连真实目录一起删除。
func TestPlaceInstallCustomDirWritesMarker(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "extract")
	mkTree(t, root, map[string]string{"bin/go": "x"})
	custom := filepath.Join(base, "sdk")
	dest := filepath.Join(base, "installs", "go", "1.22.0")
	if err := placeInstall(root, dest, custom, "go", "1.22.0"); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(custom, "go", "1.22.0")
	if !fileExists(filepath.Join(real, installMarker)) {
		t.Fatal("自定义目录里应有 .vsm-install 标记")
	}
	if got, owned := InstallTarget(dest); !owned || !samePathForTest(got, real) {
		t.Fatalf("InstallTarget 应识别为 vsm 所有的自定义目录，实际 %s owned=%v", got, owned)
	}
}

// TestPlaceInstallRefusesForeignDir --path 下已有非 vsm 的同名目录时拒绝覆盖。
func TestPlaceInstallRefusesForeignDir(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "extract")
	mkTree(t, root, map[string]string{"bin/go": "x"})
	custom := filepath.Join(base, "sdk")
	mkTree(t, filepath.Join(custom, "go", "1.22.0"), map[string]string{"mine.txt": "keep"})
	dest := filepath.Join(base, "installs", "go", "1.22.0")
	if err := placeInstall(root, dest, custom, "go", "1.22.0"); err == nil {
		t.Fatal("不应覆盖不属于 vsm 的目录")
	}
	if !fileExists(filepath.Join(custom, "go", "1.22.0", "mine.txt")) {
		t.Fatal("用户原有文件必须保留")
	}
}

func samePathForTest(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return filepath.Clean(ra) == filepath.Clean(rb)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// TestLinkDirWithSpaces 回归：路径含空格（如 C:\Program Files\Java\...）时也必须能建链接。
// 曾经为此手动给 mklink 参数加引号，结果被二次转义，所有 junction 全部创建失败。
func TestLinkDirWithSpaces(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "Program Files", "Java", "jdk 21")
	mkTree(t, target, map[string]string{"bin/java": "x"})
	link := filepath.Join(base, "my installs", "java", "21.0.1")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(target, link); err != nil {
		t.Fatalf("含空格路径创建链接失败: %v", err)
	}
	if !isLink(link) {
		t.Fatal("应识别为链接")
	}
	if !fileExists(filepath.Join(link, "bin", "java")) {
		t.Fatal("通过链接应能访问目标目录内容")
	}
}

// TestUninstallAdoptedKeepsRealDir 安全承诺：卸载纳管的系统版本只删链接，
// 系统里的真实目录与文件必须原封不动。
func TestUninstallAdoptedKeepsRealDir(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "Program Files", "Java", "jdk-21")
	mkTree(t, real, map[string]string{"bin/java": "jdk", "release": "21"})
	p := Paths{Root: filepath.Join(base, "vsm")}
	link := p.InstallDir("java", "21.0.1")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(real, link); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{Paths: p}).Uninstall("java", "21.0.1"); err != nil {
		t.Fatal(err)
	}
	if pathExists(link) {
		t.Fatal("链接应被删除")
	}
	for _, f := range []string{"bin/java", "release"} {
		if !fileExists(filepath.Join(real, f)) {
			t.Fatalf("系统目录中的 %s 被删除了——这绝不能发生", f)
		}
	}
}

// TestUninstallCustomDirRemovesRealDir --path 安装（带 vsm 标记）卸载时连真实目录一起删除。
func TestUninstallCustomDirRemovesRealDir(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "extract")
	mkTree(t, root, map[string]string{"bin/go": "x"})
	p := Paths{Root: filepath.Join(base, "vsm")}
	dest := p.InstallDir("go", "1.22.0")
	custom := filepath.Join(base, "sdk")
	if err := placeInstall(root, dest, custom, "go", "1.22.0"); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{Paths: p}).Uninstall("go", "1.22.0"); err != nil {
		t.Fatal(err)
	}
	if pathExists(dest) || pathExists(filepath.Join(custom, "go", "1.22.0")) {
		t.Fatal("链接与 vsm 自己的真实目录都应被删除")
	}
}

func TestUninstallPlainDirAndMissing(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	dir := p.InstallDir("node", "20.11.0")
	mkTree(t, dir, map[string]string{"bin/node": "x"})
	in := Installer{Paths: p}
	if err := in.Uninstall("node", "20.11.0"); err != nil {
		t.Fatal(err)
	}
	if pathExists(dir) {
		t.Fatal("普通安装目录应被删除")
	}
	if err := in.Uninstall("node", "20.11.0"); err == nil {
		t.Fatal("卸载不存在的版本应报错")
	}
}
