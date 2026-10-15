package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRealPythonVenvIsProjectLocal(t *testing.T) {
	python := os.Getenv("VSM_TEST_PYTHON")
	if python == "" {
		t.Skip("set VSM_TEST_PYTHON to a Python with venv (enabled on CI)")
	}
	query := exec.Command(python, "-c", "import json,sys; print(json.dumps({'root':sys.base_prefix,'version':'.'.join(map(str,sys.version_info[:3]))}))")
	data, err := query.Output()
	if err != nil {
		t.Fatal(err)
	}
	var base struct {
		Root    string `json:"root"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		t.Fatal(err)
	}
	p, err := InitProject(t.TempDir(), "local", "")
	if err != nil {
		t.Fatal(err)
	}
	paths := p.Paths(Paths{})
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	dest := paths.InstallDir("python", base.Version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	// Test fixture links to a real interpreter; the product refuses adopting a
	// shared interpreter in local mode and independently installs it instead.
	if err := linkDir(base.Root, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Root, ".tool-versions"), []byte("python "+base.Version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := Registry{Paths: paths}
	a := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
	t.Setenv("PYTHONHOME", filepath.Join(t.TempDir(), "wrong-parent-python"))
	if err := a.PrepareProject(context.Background(), p.Root); err != nil {
		t.Fatal(err)
	}
	if err := a.PrepareProject(context.Background(), p.Root); err != nil {
		t.Fatalf("venv setup is not idempotent: %v", err)
	}
	plan, err := a.PlanProjectEnvironment(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Variables["VIRTUAL_ENV"] != p.PythonVenv(base.Version) {
		t.Fatal("venv is not active in plan")
	}
	command := "python"
	if runtime.GOOS == "windows" {
		command += ".exe"
	}
	exe := filepath.Join(plan.Paths[0], command)
	cmd := exec.Command(exe, "-c", "import sys;print(sys.prefix);print(sys.base_prefix)")
	cmd.Env = buildEnv(plan.Paths, plan.Variables)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("venv cannot run: %v %s", err, output)
	}
	if !strings.Contains(string(output), p.PythonVenv(base.Version)) {
		t.Fatalf("Python uses another environment: %s", output)
	}
}
