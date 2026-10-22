package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommandMatchesChecksOwnedDispatcherContents(t *testing.T) {
	paths := Paths{Root: filepath.Join(t.TempDir(), "owner's tools")}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(paths.InstallDir("java", "17"), "bin", "java")
	if runtime.GOOS == "windows" {
		expected += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(expected), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expected, []byte("runtime"), 0755); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(t.TempDir(), "vsm")
	a := Activator{Paths: paths, VsmExe: cli}
	if err := a.writeShim("java"); err != nil {
		t.Fatal(err)
	}
	name, script := a.shimScript("java", "")
	actual := filepath.Join(paths.Shims(), name)
	if !a.CommandMatches("java", actual, expected) || !a.CommandMatches("java", expected, expected) {
		t.Fatal("valid owned shim or binary rejected")
	}
	other := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(other, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if a.CommandMatches("java", other, expected) {
		t.Fatal("foreign shim directory accepted")
	}
	foreignHome := a
	foreignHome.Paths.Root += "-other"
	_, foreignScript := foreignHome.shimScript("java", "")
	for _, bad := range []string{
		strings.ReplaceAll(script, cli, cli+"-old"),
		foreignScript,
		strings.ReplaceAll(script, " exec java ", " exec python "),
		script + "echo injected\n",
	} {
		if err := os.WriteFile(actual, []byte(bad), 0755); err != nil {
			t.Fatal(err)
		}
		if a.CommandMatches("java", actual, expected) {
			t.Fatalf("unowned or modified dispatcher accepted: %s", bad)
		}
	}
}

func TestCommandMatchesRecognizesGeneratedAlias(t *testing.T) {
	paths := Paths{Root: t.TempDir()}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	a := Activator{Paths: paths, VsmExe: filepath.Join(t.TempDir(), "vsm")}
	if err := a.writeCommandShim("python", "python3"); err != nil {
		t.Fatal(err)
	}
	name, _ := a.shimScript("python", "python3")
	actual := filepath.Join(paths.Shims(), name)
	expected := filepath.Join(t.TempDir(), "python")
	if err := os.WriteFile(expected, []byte("runtime"), 0755); err != nil {
		t.Fatal(err)
	}
	if !a.CommandMatches("python", actual, expected) {
		t.Fatal("valid command alias rejected")
	}
}
