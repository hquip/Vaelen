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
	"vsm/internal/plugin"
)

func TestDamagedPythonVenvStopsBeforeBaseInterpreter(t *testing.T) {
	p, err := InitProject(t.TempDir(), "local", "")
	if err != nil {
		t.Fatal(err)
	}
	paths := p.Paths(Paths{})
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	version := "3.12.10"
	python, _ := plugin.Get("python")
	base := python.BinPaths(paths.InstallDir("python", version))[0]
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	name := "python"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(base, name), []byte("must never execute this base interpreter"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Root, ".tool-versions"), []byte("python "+version+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	venv := p.PythonVenv(version)
	if err := os.MkdirAll(venv, 0755); err != nil {
		t.Fatal(err)
	}
	for file, content := range map[string]string{".vsm-python-version": version, "pyvenv.cfg": "home = test\n", "dependency-to-keep": "existing package data"} {
		if err := os.WriteFile(filepath.Join(venv, file), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	reg := Registry{Paths: paths}
	a := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
	for _, run := range []func() error{
		func() error { return a.PrepareProject(context.Background(), p.Root) },
		func() error { return a.RunProject(p.Root, "python", []string{"--version"}) },
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "venv 缺少可执行文件") {
			t.Fatalf("damaged venv reached a base interpreter or failed without a repair hint: %v", err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(venv, "dependency-to-keep")); err != nil || string(data) != "existing package data" {
		t.Fatal("damaged venv lost existing dependencies")
	}
}

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
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if err := a.RunProject(p.Root, "python", []string{"--version"}); err == nil || !strings.Contains(err.Error(), "venv 缺少可执行文件") {
		t.Fatalf("damaged real venv fell back to base Python: %v", err)
	}
	// Re-running the managed base interpreter's venv command repairs its entry
	// point. The product does not delete existing project packages to do so.
	repair := exec.Command(python, "-m", "venv", p.PythonVenv(base.Version))
	repair.Env = buildEnv(nil, p.Variables())
	if out, err := repair.CombinedOutput(); err != nil {
		t.Fatalf("repair: %v %s", err, out)
	}
	if err := a.PrepareProject(context.Background(), p.Root); err != nil {
		t.Fatalf("repaired venv unusable: %v", err)
	}
}
