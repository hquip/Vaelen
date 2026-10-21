package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/config"
)

func TestInstallRootCLIProtectsSelectionsAndReportsRestoredDefault(t *testing.T) {
	bin, home, dir := buildVsm(t), t.TempDir(), t.TempDir()
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(home, "installs", "java", "17.0.14", "bin", name)
	if err := config.WriteFileAtomic(executable, []byte("fixture: must not execute"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.SetIn(filepath.Join(home, ".tool-versions"), "java", "17.0.14"); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "empty-target")
	if code, out := runVsm(t, bin, home, dir, "install-root", empty); code == 0 || !strings.Contains(out, "java 17.0.14") || !strings.Contains(out, empty) {
		t.Fatalf("CLI silently hid an existing global version: code=%d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, "install-root")); !os.IsNotExist(err) {
		t.Fatalf("rejected root still written: %v", err)
	}
	if err := config.WriteFileAtomic(filepath.Join(home, "install-root"), []byte(empty), 0644); err != nil {
		t.Fatal(err)
	}
	if code, out := runVsm(t, bin, home, dir, "install-root", "default"); code != 0 || !strings.Contains(out, filepath.Join(home, "installs")) || strings.Contains(out, empty) {
		t.Fatalf("restoring default reported the stale root: code=%d %s", code, out)
	}
	if code, out := runVsm(t, bin, home, dir, "which", "java"); code != 0 || !strings.Contains(out, executable) {
		t.Fatalf("restoring default did not recover selected Java: code=%d %s", code, out)
	}
}
