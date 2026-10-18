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

	"vsm/internal/config"
	"vsm/internal/plugin"
)

// The runner launches VSM as a child. This fixture records which execution mode
// the runner actually requested, so a cache hit cannot conceal the difference.
func buildTaskModeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	code := `package main
import ("os"; "path/filepath")
func main() {
	mode := "unlocked"
	for _, arg := range os.Args[1:] {
		if arg == "--locked" { mode = "locked" }
	}
	versions, err := os.ReadDir(filepath.Join(os.Getenv("VSM_HOME"), "installs", "go"))
	if err != nil { panic(err) }
	version := versions[len(versions)-1].Name()
	if err := os.WriteFile("output.txt", []byte(mode+":"+version), 0600); err != nil { panic(err) }
}`
	if err := os.WriteFile(source, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "fixture")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", program, source).CombinedOutput(); err != nil {
		t.Fatalf("build task fixture: %v\n%s", err, out)
	}
	return program
}

func taskCacheWorkspace(t *testing.T) (home, root string) {
	t.Helper()
	home, root = t.TempDir(), t.TempDir()
	t.Setenv("VSM_HOME", home)
	t.Setenv("VSM_PROJECT_BOUNDARY", "")
	t.Setenv("VSM_MCP_REQUEST_ID", "")
	c := config.Workspace{Schema: 1, Tools: map[string]string{"go": "1"}, Tasks: map[string]config.Task{
		"build": {Command: []string{"go", "build"}, Inputs: []string{"input.txt"}, Outputs: []string{"output.txt"}},
	}}
	if err := c.Save(filepath.Join(root, config.WorkspaceName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := FindWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Trust(); err != nil {
		t.Fatal(err)
	}
	return home, root
}

func TestTaskCacheSeparatesLockedExecution(t *testing.T) {
	program := buildTaskModeFixture(t)
	home, root := taskCacheWorkspace(t)
	artifact := plugin.Artifact{URL: "https://example.com/go.zip", SHA256: strings.Repeat("0", 64)}
	install := filepath.Join(home, "installs", "go", "1.0.0")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeArtifactReceipt(install, artifact); err != nil {
		t.Fatal(err)
	}
	p, _ := plugin.Get("go")
	lock := ToolchainLock{Schema: 1, Tools: map[string]LockedTool{"go": {
		Definition: plugin.DefinitionID(p), Requested: "1", Version: "1.0.0",
		Platforms: map[string]plugin.Artifact{runtime.GOOS + "/" + runtime.GOARCH: artifact},
	}}}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, LockName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, locked := range []bool{false, true} {
		report, err := RunTasks(context.Background(), root, program, []string{"build"}, TaskOptions{Locked: locked})
		if err != nil {
			t.Fatal(err)
		}
		want := "unlocked:1.0.0"
		if locked {
			want = "locked:1.0.0"
		}
		output, err := os.ReadFile(filepath.Join(root, "output.txt"))
		if err != nil || string(output) != want || len(report.Tasks) != 1 || report.Tasks[0].State == "cached" {
			t.Fatalf("locked=%v reused a different execution mode: output=%q report=%+v error=%v", locked, output, report, err)
		}
		// Identical subsequent execution should still benefit from the cache.
		report, err = RunTasks(context.Background(), root, program, []string{"build"}, TaskOptions{Locked: locked})
		if err != nil || len(report.Tasks) != 1 || report.Tasks[0].State != "cached" {
			t.Fatalf("locked=%v did not reuse its own cache: report=%+v error=%v", locked, report, err)
		}
	}
}

func TestTaskCacheInvalidatesWhenSelectedToolchainChanges(t *testing.T) {
	program := buildTaskModeFixture(t)
	home, root := taskCacheWorkspace(t)
	for _, version := range []string{"1.0.0", "1.1.0"} {
		if err := os.MkdirAll(filepath.Join(home, "installs", "go", version), 0o755); err != nil {
			t.Fatal(err)
		}
		report, err := RunTasks(context.Background(), root, program, []string{"build"}, TaskOptions{})
		if err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(root, "output.txt"))
		if err != nil || string(output) != "unlocked:"+version || len(report.Tasks) != 1 || report.Tasks[0].State == "cached" {
			t.Fatalf("new selected toolchain %s did not invalidate cache: output=%q report=%+v error=%v", version, output, report, err)
		}
		report, err = RunTasks(context.Background(), root, program, []string{"build"}, TaskOptions{})
		if err != nil || len(report.Tasks) != 1 || report.Tasks[0].State != "cached" {
			t.Fatalf("unchanged toolchain %s did not reuse cache: report=%+v error=%v", version, report, err)
		}
	}
}

func TestTaskFileDigestUnambiguouslyFramesBinaryInputs(t *testing.T) {
	root := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.bin", "X")
	write("b.bin", "Y\x00b.bin\x00Z")
	before, err := hashWorkspaceFiles(root, []string{"*.bin"})
	if err != nil {
		t.Fatal(err)
	}
	// File contents may contain NUL and another file's name. Their boundaries
	// must remain unambiguous even when the old concatenated bytes are equal.
	write("a.bin", "X\x00b.bin\x00Y")
	write("b.bin", "Z")
	after, err := hashWorkspaceFiles(root, []string{"*.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("changed binary files have the same digest after moving boundary-shaped bytes between files")
	}
}
