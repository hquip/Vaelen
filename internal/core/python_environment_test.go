package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

func TestProjectPython3AliasWorksWithoutAnUnversionedInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix packages commonly provide only python3")
	}
	a := activationFixture(t)
	t.Setenv("VIRTUAL_ENV", "")
	file := filepath.Join(a.Paths.InstallDir("python", "3.12.10"), "bin", "python3")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("#!/bin/sh\nprintf 'selected-python3\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "python", "3.12.10"); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"python", "python3"} {
		var output strings.Builder
		if err := a.RunProjectContext(context.Background(), t.TempDir(), command, nil, nil, &output, &output); err != nil || !strings.Contains(output.String(), "selected-python3") {
			t.Fatalf("%s did not use the selected interpreter: %v %s", command, err, output.String())
		}
	}
}

func pythonVenvFixture(t *testing.T, parent string) (string, string) {
	t.Helper()
	root := filepath.Join(parent, "IDE project & space", ".venv")
	bin := filepath.Join(root, "bin")
	command := "python"
	if runtime.GOOS == "windows" {
		bin = filepath.Join(root, "Scripts")
		command += ".exe"
	}
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for file, content := range map[string]string{
		filepath.Join(root, "pyvenv.cfg"): "home = base\nversion = 3.12.10\n",
		filepath.Join(bin, command):       "test interpreter; never execute",
	} {
		if err := os.WriteFile(file, []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return root, bin
}

func TestActiveIDEVenvTakesPrecedenceOverManagedPython(t *testing.T) {
	a := activationFixture(t)
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("_VSM_MANAGED_VIRTUAL_ENV", "")
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "python", "3.12.10"); err != nil {
		t.Fatal(err)
	}
	venv, bin := pythonVenvFixture(t, t.TempDir())
	t.Setenv("VIRTUAL_ENV", venv)
	plan, err := a.PlanEnvironment(t.TempDir())
	if err != nil {
		t.Fatalf("an activated venv must work without installing the global Python: %v", err)
	}
	if len(plan.Paths) == 0 || !sameDir(plan.Paths[0], bin) {
		t.Fatalf("IDE venv was shadowed: %+v", plan)
	}
	if _, managed := plan.Variables["VIRTUAL_ENV"]; managed {
		t.Fatal("VSM must leave activation/deactivation of an IDE venv to its owner")
	}
	which, err := a.Which("python", t.TempDir())
	if err != nil || !sameDir(filepath.Dir(which), bin) {
		t.Fatalf("shim resolution disagrees with the active venv: %s %v", which, err)
	}
}

func TestBrokenActiveVenvDoesNotFallBackToGlobalPython(t *testing.T) {
	a := activationFixture(t)
	venv, bin := pythonVenvFixture(t, t.TempDir())
	t.Setenv("VIRTUAL_ENV", venv)
	t.Setenv("_VSM_MANAGED_VIRTUAL_ENV", "")
	for _, entry := range mustReadDir(t, bin) {
		if err := os.Remove(filepath.Join(bin, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	_, err := a.PlanEnvironment(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "venv") {
		t.Fatalf("broken active environment could fall back to shared Python: %v", err)
	}
	if _, err := a.Which("python", t.TempDir()); err == nil {
		t.Fatal("which silently fell back from the damaged venv")
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestAdoptedVenvKeepsItsInterpreterAndPackagePrefix(t *testing.T) {
	venv, bin := pythonVenvFixture(t, t.TempDir())
	p, _ := plugin.Get("python")
	dirs := p.BinPaths(venv)
	if len(dirs) == 0 || !sameDir(dirs[0], bin) {
		t.Fatalf("adopted venv interpreter is not in its bin directory: %v", dirs)
	}
	if env := p.ExecEnv(venv); binaryIdentity(env["VIRTUAL_ENV"]) != binaryIdentity(venv) || env["PYTHONHOME"] != "" {
		t.Fatalf("adopted venv lost its package prefix: %v", env)
	}
	other, otherBin := pythonVenvFixture(t, t.TempDir())
	first := describeRuntime("python", "python", findExecutable([]string{bin}, "python"), "3.12.10")
	second := describeRuntime("python", "python", findExecutable([]string{otherBin}, "python"), "3.12.10")
	if first.RegistryVersion() == second.RegistryVersion() || !strings.HasPrefix(first.RegistryVersion(), "venv-") {
		t.Fatalf("same Python version collapsed independent venvs: %+v %+v", first, second)
	}
	a := activationFixture(t)
	in := Installer{Paths: a.Paths}
	for _, s := range []SystemVersion{first, second} {
		if err := in.Adopt(p, s.RegistryVersion(), s.Path); err != nil {
			t.Fatal(err)
		}
	}
	if r := a.Resolver.match("python", "latest", "test"); r.Version != "" {
		t.Fatalf("a numeric/global base requirement silently selected a venv: %+v", r)
	}
	if r := a.Resolver.match("python", first.RegistryVersion(), "test"); r.Version != first.RegistryVersion() {
		t.Fatalf("explicit venv label did not resolve: %+v", r)
	}
	if err := in.Uninstall("python", first.RegistryVersion()); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{venv, other} {
		if !fileExists(filepath.Join(root, "pyvenv.cfg")) {
			t.Fatal("unregistering a venv removed its original environment")
		}
	}
}
