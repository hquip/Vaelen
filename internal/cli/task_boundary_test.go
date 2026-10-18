package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"vsm/internal/config"
	"vsm/internal/core"
)

func TestWorkspaceTaskCacheUsesExecutionBoundary(t *testing.T) {
	bin := buildVsm(t)
	t.Setenv("VSM_GO_VERSION", "")
	t.Setenv("VSM_PROJECT_BOUNDARY", "")
	home, ancestor := t.TempDir(), t.TempDir()
	root := filepath.Join(ancestor, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ancestor, ".tool-versions"), []byte("go 1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Each fake Go installation records its own directory version, so the
	// assertion observes the actual selected executable rather than its env.
	fixtureDir := t.TempDir()
	fixtureSource := filepath.Join(fixtureDir, "probe.go")
	source := `package main
import("os";"path/filepath")
func main(){
 exe,err:=os.Executable();if err!=nil{panic(err)}
 version:=filepath.Base(filepath.Dir(filepath.Dir(exe)))
 if err:=os.WriteFile("output.txt",[]byte(version),0600);err!=nil{panic(err)}
}`
	if err := os.WriteFile(fixtureSource, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fixture := filepath.Join(fixtureDir, name)
	cmd := exec.Command("go", "build", "-o", fixture, fixtureSource)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake Go: %v\n%s", err, out)
	}
	program, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		path := filepath.Join(home, "installs", "go", version, "bin", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, program, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workspace := config.Workspace{Schema: 1, Tasks: map[string]config.Task{
		"build": {Command: []string{"go"}, Inputs: []string{"input.txt"}, Outputs: []string{"output.txt"}},
	}}
	if err := workspace.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("unchanged input"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := runVsm(t, bin, home, root, "project", "trust"); code != 0 {
		t.Fatalf("trust workspace: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, ".vsm", "project.json")); !os.IsNotExist(err) {
		t.Fatalf("fixture must use vsm.json without an initialized project: %v", err)
	}

	for i, step := range []struct{ version, state string }{
		{"1.1.0", "success"},
		{"1.1.0", "cached"},
		{"1.2.0", "success"},
		{"1.2.0", "cached"},
	} {
		if err := os.WriteFile(filepath.Join(home, ".tool-versions"), []byte("go "+step.version+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out := runVsm(t, bin, home, root, "tasks", "run", "build", "--json")
		if code != 0 {
			t.Fatalf("step %d: task failed: %d %s", i, code, out)
		}
		var report core.TaskReport
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("step %d: invalid task report: %v\n%s", i, err, out)
		}
		if !report.Success || len(report.Tasks) != 1 || report.Tasks[0].State != step.state {
			t.Fatalf("step %d: expected %s using global Go %s: %s", i, step.state, step.version, out)
		}
		output, err := os.ReadFile(filepath.Join(root, "output.txt"))
		if err != nil || string(output) != step.version {
			t.Fatalf("step %d: task used %q instead of global Go %s: %v", i, output, step.version, err)
		}
	}
}
