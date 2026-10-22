package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/plugin"
)

func TestNonExecutableInstalledRuntimeIsRejectedWithoutChangingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable permissions")
	}
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_GO_VERSION", "")
	paths := Paths{Root: t.TempDir()}
	file := filepath.Join(paths.InstallDir("go", "1.2.3"), "bin", "go")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("#!/bin/sh\necho fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, _ := plugin.Get("go")
	err := (Installer{Paths: paths}).Install(p, "1.2.3", "", nil)
	if err == nil || !strings.Contains(err.Error(), "安装不完整") {
		t.Fatalf("nonexecutable install was skipped as usable: %v", err)
	}
	if got := findExecutable([]string{filepath.Dir(file)}, "go"); got != "" {
		t.Fatalf("nonexecutable returned: %s", got)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatal("changed existing runtime permissions")
	}
	if err := os.Chmod(file, 0755); err != nil {
		t.Fatal(err)
	}
	if got := findExecutable([]string{filepath.Dir(file)}, "go"); got != file {
		t.Fatalf("repaired executable rejected: %s", got)
	}
	link := filepath.Join(t.TempDir(), "go")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if got := findExecutable([]string{filepath.Dir(link)}, "go"); got != link {
		t.Fatalf("valid executable symlink rejected: %s", got)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if got := findExecutable([]string{filepath.Dir(link)}, "go"); got != "" {
		t.Fatal("broken executable symlink accepted")
	}
}
