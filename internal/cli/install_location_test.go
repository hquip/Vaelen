package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIRejectsNestedInstallBaseBeforeRemoteResolution(t *testing.T) {
	bin, home, dir := buildVsm(t), t.TempDir(), t.TempDir()
	for _, tool := range []string{"go", "java"} {
		base := filepath.Join(home, "installs", tool, "nested")
		code, out := runVsm(t, bin, home, dir, "install", tool, "latest", "--path", base)
		if code == 0 || !strings.Contains(out, "安装位置不能嵌套") {
			t.Fatalf("invalid path reached remote resolution: code=%d %s", code, out)
		}
	}
}
