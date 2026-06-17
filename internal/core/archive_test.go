package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestExtractTarXz 验证 .tar.xz 经由系统 tar 正确解压。
// 依赖系统 tar 支持 xz（Unix 自带；Windows 10 1803+ 内置 bsdtar+liblzma）；不具备时跳过。
func TestExtractTarXz(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("系统无 tar，跳过")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello xz"), 0o644); err != nil {
		t.Fatal(err)
	}
	xz := filepath.Join(dir, "t.tar.xz")
	// -a：按扩展名(.xz)自动选择压缩；GNU tar 与 bsdtar 均支持。
	if out, err := exec.Command("tar", "-caf", xz, "-C", dir, "src").CombinedOutput(); err != nil {
		t.Skipf("系统 tar 不支持创建 xz，跳过：%v：%s", err, out)
	}
	out := filepath.Join(dir, "out")
	if err := Extract(xz, out); err != nil {
		t.Fatalf("Extract(.tar.xz) 失败: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "src", "a.txt"))
	if err != nil || string(b) != "hello xz" {
		t.Fatalf("解压内容不正确: %q err=%v", string(b), err)
	}
}
