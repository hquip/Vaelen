package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDetectFromEnv 验证可从环境变量（如 GOROOT）定位到运行时，即使它不在 PATH。
func TestDetectFromEnv(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// 造一个 >0 字节的占位主命令；probeVersion 可能取不到版本，但不影响"被发现"。
	if err := os.WriteFile(filepath.Join(bin, name), []byte("placeholder"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOROOT", dir)

	found := false
	for _, sv := range detectFromEnv() {
		if sv.Tool == "go" && filepath.Dir(sv.Path) == bin {
			found = true
		}
	}
	if !found {
		t.Fatal("应能从 GOROOT 发现 go 运行时（root/bin 下）")
	}
}
