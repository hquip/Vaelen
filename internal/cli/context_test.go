package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/core"
)

func TestContextReportsWithoutChangingProject(t *testing.T) {
	bin := buildVsm(t)
	home, cwd := t.TempDir(), t.TempDir()
	content := []byte("# project requirement\ngo 1.23.0\n")
	file := filepath.Join(cwd, ".tool-versions")
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSM_GO_VERSION", "")
	t.Setenv("_VSM_ACTIVE_VERSIONS", "")
	code, out := runVsm(t, bin, home, cwd, "context", "--json")
	if code != 0 {
		t.Fatal(out)
	}
	var response struct {
		Schema   int           `json:"schema_version"`
		ReadOnly bool          `json:"read_only"`
		Tools    []contextTool `json:"tools"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err, out)
	}
	if response.Schema != 1 || !response.ReadOnly || len(response.Tools) != 1 {
		t.Fatalf("bad context: %s", out)
	}
	goTool := response.Tools[0]
	if goTool.Tool != "go" || goTool.State != "missing_version" || goTool.ProjectRequired != "1.23.0" || len(goTool.Suggestions) == 0 {
		t.Fatalf("missing agent guidance: %s", out)
	}
	if code, _ := runVsm(t, bin, home, cwd, "context", "--json", "--check"); code == 0 {
		t.Fatal("--check must fail for missing versions")
	}
	if _, err := os.Stat(filepath.Join(home, "installs")); !os.IsNotExist(err) {
		t.Fatal("context created installation state")
	}
	got, _ := os.ReadFile(file)
	if string(got) != string(content) {
		t.Fatal("context changed project file")
	}
}

func TestContextAcceptsOwnedShimAndRejectsForeignOrChangedShim(t *testing.T) {
	bin, home, dir := buildVsm(t), t.TempDir(), t.TempDir()
	paths := core.Paths{Root: home}
	reg := core.Registry{Paths: paths}
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	file := filepath.Join(paths.InstallDir("java", "17"), "bin", name)
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("must not execute during context query"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.GlobalToolVersions(), []byte("java 17\n"), 0644); err != nil {
		t.Fatal(err)
	}
	a := core.Activator{Paths: paths, Registry: reg, VsmExe: bin}
	if err := a.GenerateShims(); err != nil {
		t.Fatal(err)
	}
	shimName := "java"
	if runtime.GOOS == "windows" {
		shimName += ".cmd"
	}
	shim := filepath.Join(paths.Shims(), shimName)
	original, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"owned-no-label", "owned-stale-label", "modified", "stale-cli", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			content := original
			search := paths.Shims()
			if mode == "modified" {
				content = append(append([]byte{}, original...), []byte("echo changed\n")...)
			}
			if mode == "stale-cli" {
				content = []byte(strings.ReplaceAll(string(original), bin, bin+"-old"))
			}
			if mode == "foreign" {
				search = t.TempDir()
				if err := os.WriteFile(filepath.Join(search, shimName), original, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(shim, content, 0755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, "context", "--json", "--check")
			cmd.Dir = dir
			label := ""
			if mode == "owned-stale-label" {
				label = "java@21"
			}
			cmd.Env = append(os.Environ(), "VSM_HOME="+home, "VSM_JAVA_VERSION=", "_VSM_ACTIVE_VERSIONS="+label, "PATH="+search+string(os.PathListSeparator)+os.Getenv("PATH"))
			var out, stderr strings.Builder
			cmd.Stdout = &out
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			wantReady := strings.HasPrefix(mode, "owned")
			if (runErr == nil) != wantReady {
				t.Fatalf("mode %s: %v %s %s", mode, runErr, out.String(), stderr.String())
			}
			var response struct {
				Tools []contextTool `json:"tools"`
			}
			if err := json.Unmarshal([]byte(out.String()), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Tools) != 1 || (response.Tools[0].State == "ready") != wantReady {
				t.Fatalf("incorrect context: %s", out.String())
			}
			if wantReady && response.Tools[0].Active != "17" {
				t.Fatalf("context retained stale active label: %s", out.String())
			}
		})
	}
}
