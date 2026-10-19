package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/config"
)

func activationFixture(t *testing.T) Activator {
	t.Helper()
	t.Setenv("VSM_JAVA_VERSION", "")
	paths := Paths{Root: t.TempDir()}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"17.0.12", "21.0.1"} {
		bin := filepath.Join(paths.InstallDir("java", version), "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		name := "java"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(bin, name), []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reg := Registry{Paths: paths}
	return Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}, VsmExe: filepath.Join(t.TempDir(), "new cli", "vsm")}
}

func TestSetGlobalVersionRefreshesStaleShims(t *testing.T) {
	a := activationFixture(t)
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	shim := filepath.Join(a.Paths.Shims(), name)
	if err := os.WriteFile(shim, []byte("old-vsm exec java"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SetGlobalVersion("java", "21.0.1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(shim)
	if err != nil || !strings.Contains(string(data), a.VsmExe) || strings.Contains(string(data), "old-vsm") {
		t.Fatalf("stale shim: %s %v", data, err)
	}
	r := a.Resolver.Resolve("java", t.TempDir())
	if r.Version != "21.0.1" || r.Source != "全局" {
		t.Fatalf("%+v", r)
	}
}

func TestSetGlobalVersionFailureDoesNotChangeDefault(t *testing.T) {
	a := activationFixture(t)
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	// A nonempty directory blocks shim replacement on all operating systems.
	shim := filepath.Join(a.Paths.Shims(), name)
	if err := os.Mkdir(shim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shim, "keep"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetGlobalVersion("java", "21.0.1"); err == nil {
		t.Fatal("shim failure ignored")
	}
	tv, _ := config.Load(a.Paths.GlobalToolVersions())
	if got, _ := tv.Get("java"); got != "17.0.12" {
		t.Fatalf("default changed on failure: %s", got)
	}
	for _, mode := range []string{"local", "docker"} {
		a.Paths.ProjectMode = mode
		if err := a.SetGlobalVersion("java", "21.0.1"); err == nil {
			t.Fatalf("%s allowed global mutation", mode)
		}
	}
	a.Paths.ProjectMode = ""
	for _, version := range []string{"", " ", "17\n21"} {
		if err := a.SetGlobalVersion("java", version); err == nil {
			t.Fatal("invalid version accepted")
		}
	}
}

func TestActivationStatusExplainsPATHAndProjectOverrides(t *testing.T) {
	a := activationFixture(t)
	if err := a.SetGlobalVersion("java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := config.SetIn(filepath.Join(project, ".tool-versions"), "java", "21.0.1"); err != nil {
		t.Fatal(err)
	}
	otherBin := filepath.Join(t.TempDir(), "system-java")
	if err := os.Mkdir(otherBin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	other := filepath.Join(otherBin, name)
	if err := os.WriteFile(other, []byte("system java"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", otherBin+string(os.PathListSeparator)+a.Paths.Shims())
	status := a.ActivationStatus("java", project)
	if status.Global != "17.0.12" || status.Current != "21.0.1" || !sameCommandFile(status.Command, other) || len(status.Warnings) != 2 {
		t.Fatalf("%+v", status)
	}
	t.Setenv("VSM_JAVA_VERSION", "17.0.12")
	status = a.ActivationStatus("java", project)
	if status.Current != "17.0.12" || !strings.Contains(strings.Join(status.Warnings, " "), "VSM_JAVA_VERSION") {
		t.Fatalf("%+v", status)
	}
	// A direct executable selected by an activated shell is not a PATH conflict.
	t.Setenv("PATH", filepath.Dir(status.Executable))
	status = a.ActivationStatus("java", t.TempDir())
	if len(status.Warnings) != 1 {
		t.Fatalf("selected executable reported as conflict: %+v", status)
	}
}
