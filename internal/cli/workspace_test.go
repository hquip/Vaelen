package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"vsm/internal/config"
	"vsm/internal/core"
)

func TestWorkspaceTasksEndToEnd(t *testing.T) {
	bin := buildVsm(t)
	home, root := t.TempDir(), t.TempDir()
	fixture := filepath.Join(t.TempDir(), "probe.go")
	source := `package main
import("fmt";"os";"time";"os/exec")
func main(){if len(os.Args)<2{return};switch os.Args[1]{case "write":if err:=os.WriteFile(os.Args[2],[]byte(os.Getenv("VALUE")),0600);err!=nil{panic(err)};fmt.Println(os.Getenv("VALUE"));case "copy":b,e:=os.ReadFile(os.Args[2]);if e!=nil{panic(e)};if e=os.WriteFile(os.Args[3],b,0600);e!=nil{panic(e)};case "fail":os.Exit(42);case "sleep":time.Sleep(10*time.Second);case "spawn":c:=exec.Command(os.Args[0],"delayed",os.Args[2]);c.Stdout=os.Stdout;c.Stderr=os.Stderr;if e:=c.Start();e!=nil{panic(e)};time.Sleep(10*time.Second);case "delayed":time.Sleep(3*time.Second);os.WriteFile(os.Args[2],[]byte("leaked"),0600)}}`
	if err := os.WriteFile(fixture, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	tool := filepath.Join(home, "installs", "go", "1.0.0", "bin", name)
	if err := os.MkdirAll(filepath.Dir(tool), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", tool, fixture).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s %v", out, err)
	}
	c := config.Workspace{Schema: 1, Tools: map[string]string{"go": "1"}, Env: map[string]string{"VALUE": "project-value"}, EnvFiles: []string{".env"}, Tasks: map[string]config.Task{
		"prepare": {Command: []string{"go", "write", "base.txt"}},
		"a":       {Depends: []string{"prepare"}, Command: []string{"go", "write", "a.txt"}, Env: map[string]string{"VALUE": "task-value"}},
		"b":       {Depends: []string{"prepare"}, Command: []string{"go", "copy", "base.txt", "b.txt"}},
		"build":   {Depends: []string{"a", "b"}},
		"cached":  {Command: []string{"go", "copy", "input.txt", "output.txt"}, Inputs: []string{"input.txt"}, Outputs: []string{"output.txt"}},
		"fail":    {Command: []string{"go", "fail"}},
		"after":   {Command: []string{"go", "write", "should-not-exist.txt"}, Depends: []string{"fail"}},
		"timeout": {Command: []string{"go", "sleep"}, TimeoutSeconds: 1},
		"tree":    {Command: []string{"go", "spawn", "orphan.txt"}, TimeoutSeconds: 1},
	}}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("VALUE=dotenv-value\nPRIVATE_TOKEN=never-show-this-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string) { return runVsm(t, bin, home, root, args...) }
	if code, _ := run("tasks", "run", "build"); code == 0 {
		t.Fatal("untrusted tasks ran")
	}
	if _, err := os.Stat(filepath.Join(root, "base.txt")); !os.IsNotExist(err) {
		t.Fatal("untrusted task wrote output")
	}
	if code, out := run("project", "trust"); code != 0 {
		t.Fatal(out)
	}
	if code, out := run("tasks", "run", "build", "--jobs", "2", "--json"); code != 0 {
		t.Fatalf("task graph failed: %d %s", code, out)
	} else {
		var report core.TaskReport
		if err := json.Unmarshal([]byte(out), &report); err != nil || !report.Success || len(report.Tasks) != 4 {
			t.Fatalf("invalid task JSON: %s", out)
		}
	}
	for name, want := range map[string]string{"base.txt": "project-value", "a.txt": "task-value", "b.txt": "project-value"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != want {
			t.Fatalf("wrong task env/order: %s=%s %v", name, data, err)
		}
	}
	if code, out := run("project", "inspect", "--json"); code != 0 || strings.Contains(out, "never-show-this-token") || strings.Contains(out, "task-value") || strings.Contains(out, "project-value") {
		t.Fatalf("inspection leaked environment: %s", out)
	}
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := run("tasks", "run", "cached", "--json"); code != 0 {
		t.Fatal(out)
	}
	if code, out := run("tasks", "run", "cached", "--json"); code != 0 || !strings.Contains(out, `"state": "cached"`) {
		t.Fatalf("cache missed: %s", out)
	}
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := run("tasks", "run", "cached", "--json"); code != 0 || strings.Contains(out, `"state": "cached"`) {
		t.Fatalf("stale cache reused: %s", out)
	}
	if code, out := run("tasks", "run", "after", "--json"); code != 42 {
		t.Fatalf("exit code lost: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "should-not-exist.txt")); !os.IsNotExist(err) {
		t.Fatal("failed prerequisite did not stop dependent")
	}
	if code, _ := run("tasks", "run", "timeout"); code == 0 {
		t.Fatal("task timeout ignored")
	}
	if code, _ := run("tasks", "run", "tree"); code == 0 {
		t.Fatal("descendant timeout ignored")
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "orphan.txt")); !os.IsNotExist(err) {
		t.Fatal("task cancellation left a live descendant")
	}

	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("VALUE=changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("tasks", "run", "build"); code == 0 {
		t.Fatal("changed dotenv retained trust")
	}
}
