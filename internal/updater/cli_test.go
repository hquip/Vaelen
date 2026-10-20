package updater

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallMatchingCLILeavesUnrelatedOldCopyUntouched(t *testing.T) {
	name := "vsm"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("go", "build", "-ldflags", "-X vsm/internal/buildinfo.Version=1.0.5", "-o", bin, "vsm").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := releaseFixture(t, data)
	root := t.TempDir()
	old := filepath.Join(root, name)
	if err := os.WriteFile(old, []byte("old 0.1.0 copy"), 0o755); err != nil {
		t.Fatal(err)
	}
	installed, err := client.InstallCLI(context.Background(), "v1.0.5", filepath.Join(root, "runtime", "cli", "v1.0.5"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(installed, "version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "vsm v1.0.5" {
		t.Fatalf("recovered CLI: %v %s", err, out)
	}
	if got, err := os.ReadFile(old); err != nil || string(got) != "old 0.1.0 copy" {
		t.Fatalf("unrelated old CLI changed: %q %v", got, err)
	}
	if _, err := client.InstallCLI(context.Background(), "v1.0.6", filepath.Join(root, "other")); err == nil {
		t.Fatal("downloaded a different version than requested")
	}
	if _, err := os.Stat(filepath.Join(root, "other")); !os.IsNotExist(err) {
		t.Fatalf("version mismatch created destination: %v", err)
	}
}
