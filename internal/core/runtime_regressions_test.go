package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vsm/internal/plugin"
)

func TestRangeRequirementsDoNotSelectExcludedVersions(t *testing.T) {
	for _, test := range []struct {
		tool, file, content string
		versions            []string
		want                string
	}{
		{"node", "package.json", `{"engines":{"node":"<20"}}`, []string{"19.9.0", "20.11.1"}, "19.9.0"},
		{"python", "pyproject.toml", "[project]\nrequires-python = \"<3.12\"\n", []string{"3.11.9", "3.12.5"}, "3.11.9"},
	} {
		t.Run(test.tool, func(t *testing.T) {
			p := Paths{Root: t.TempDir()}
			dir := t.TempDir()
			for _, v := range test.versions {
				os.MkdirAll(p.InstallDir(test.tool, v), 0o755)
			}
			os.WriteFile(filepath.Join(dir, test.file), []byte(test.content), 0o644)
			r := Resolver{Paths: p, Registry: Registry{Paths: p}}.Resolve(test.tool, dir)
			if r.Version != test.want {
				t.Fatalf("selected %s, want %s", r.Version, test.want)
			}
		})
	}
}

func TestUninstallAndReshimRemoveLastVersionShim(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	p.EnsureDirs()
	os.MkdirAll(p.InstallDir("go", "1.22.3"), 0o755)
	a := Activator{Paths: p, Registry: Registry{Paths: p}}
	if err := a.GenerateShims(); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{Paths: p}).Uninstall("go", "1.22.3"); err != nil {
		t.Fatal(err)
	}
	if err := a.GenerateShims(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go", "go.cmd"} {
		if fileExists(filepath.Join(p.Shims(), name)) {
			t.Fatal("uninstalled tool still shadows system command")
		}
	}
}

func TestExistingEmptyInstallIsNotSuccess(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	os.MkdirAll(p.InstallDir("go", "1.22.3"), 0o755)
	goPlugin, _ := plugin.Get("go")
	if err := (Installer{Paths: p}).Install(goPlugin, "1.22.3", "", nil); err == nil {
		t.Fatal("empty directory was accepted as installed")
	}
}

func TestGoExecutionOverridesInheritedToolchain(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go99.0.0+auto")
	env := buildEnv([]string{"/selected/go/bin"}, map[string]string{"GOROOT": "/selected/go"})
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOTOOLCHAIN=") {
			count++
			if entry != "GOTOOLCHAIN=local" {
				t.Fatal("inherited toolchain can override selected Go")
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one GOTOOLCHAIN override, got %d", count)
	}
}
