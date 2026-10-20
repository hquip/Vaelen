package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStandaloneGUIFindsMatchingReleaseCLI(t *testing.T) {
	if os.Getenv("VSM_DISCOVERY_CHILD") == "1" {
		got := locateVsmCLI()
		want := os.Getenv("VSM_EXPECT_CLI")
		actualFile, actualErr := os.Stat(got)
		expectedFile, expectedErr := os.Stat(want)
		if actualErr != nil || expectedErr != nil || !os.SameFile(actualFile, expectedFile) {
			t.Fatalf("GUI chose %q; matching unrenamed release CLI is %q", got, want)
		}
		return
	}
	root := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	guiDir, oldDir := filepath.Join(root, "gui"), filepath.Join(root, "old-cli")
	for _, dir := range []string{guiDir, oldDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	correct := filepath.Join(guiDir, "vsm-"+runtime.GOOS+"-"+runtime.GOARCH+ext)
	for _, build := range []struct{ path, version string }{{correct, ""}, {filepath.Join(oldDir, "vsm"+ext), "0.1.0"}} {
		args := []string{"build", "-o", build.path}
		if build.version != "" {
			args = append(args, "-ldflags", "-X vsm/internal/buildinfo.Version="+build.version)
		}
		args = append(args, "vsm")
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("build fixture: %v %s", err, out)
		}
	}
	legacy, err := os.ReadFile(filepath.Join(oldDir, "vsm"+ext))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(guiDir, "vsm"+ext), legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(guiDir, "gui-probe"+ext)
	if err := os.WriteFile(probe, data, 0o755); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(probe, "-test.run=^TestStandaloneGUIFindsMatchingReleaseCLI$", "-test.v")
	child.Env = append(os.Environ(), "VSM_DISCOVERY_CHILD=1", "VSM_HOME="+filepath.Join(root, "home"), "VSM_EXPECT_CLI="+correct, "PATH="+oldDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("standalone GUI discovery: %v\n%s", err, out)
	}
}
