package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/config"
)

func TestInstallRootSwitchDoesNotSilentlyInvalidateGlobalVersions(t *testing.T) {
	a := newTestApp(t)
	if err := a.SetProjectDir(""); err != nil {
		t.Fatal(err)
	}
	original := a.GetInstallRoot()
	for tool, version := range map[string]string{"java": "17.0.14", "node": "22.22.0"} {
		name := tool
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		dir := a.c().paths.InstallDir(tool, version)
		if tool == "java" {
			dir = filepath.Join(dir, "bin")
		}
		if err := config.WriteFileAtomic(filepath.Join(dir, name), []byte("untouched runtime"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := config.SetIn(a.c().paths.GlobalToolVersions(), tool, version); err != nil {
			t.Fatal(err)
		}
	}
	newRoot := filepath.Join(t.TempDir(), "new-install-root")
	err := a.SetInstallRoot(newRoot)
	if err == nil {
		for _, tool := range []string{"java", "node"} {
			_, failure := a.c().act.Which(tool, t.TempDir())
			t.Logf("after changing %s -> %s, %s resolves to: %v", original, newRoot, tool, failure)
		}
		t.Fatal("changing only the lookup root silently stranded the saved global versions")
	}
	if !strings.Contains(err.Error(), original) || !strings.Contains(err.Error(), newRoot) {
		t.Fatalf("root switch did not explain the old and new directories: %v", err)
	}
	if a.GetInstallRoot() != original {
		t.Fatal("failed change still replaced the active root")
	}
	if _, err := os.Stat(newRoot); !os.IsNotExist(err) {
		t.Fatalf("rejected change created its target directory: %v", err)
	}
}
