package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/config"
)

func TestWorkspaceTrustTracksDotenvAndCannotBeGrantedByMCP(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	root := t.TempDir()
	c := config.Workspace{Schema: 1, EnvFiles: []string{".env"}, Env: map[string]string{"APP_MODE": "test"}}
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=private-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := FindWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if w.Trusted() {
		t.Fatal("unreviewed configuration trusted")
	}
	if _, err := w.Environment(); err == nil {
		t.Fatal("untrusted dotenv loaded")
	}
	t.Setenv("VSM_MCP_REQUEST_ID", "request")
	if err := w.Trust(); err == nil {
		t.Fatal("MCP granted trust")
	}
	t.Setenv("VSM_MCP_REQUEST_ID", "")
	if err := w.Trust(); err != nil {
		t.Fatal(err)
	}
	env, err := w.Environment()
	if err != nil || env["TOKEN"] != "private-token" || env["APP_MODE"] != "test" {
		t.Fatalf("environment missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if w.Trusted() {
		t.Fatal("trust survived dotenv change")
	}
}

func TestWorkspaceCannotUseExternalEnvironmentFileOrCacheDirectory(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	root, outside := t.TempDir(), t.TempDir()
	if err := linkDir(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	c := config.Workspace{Schema: 1, EnvFiles: []string{"linked/.env"}}
	_ = os.WriteFile(filepath.Join(outside, ".env"), []byte("TOKEN=secret\n"), 0o600)
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	w, err := FindWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Trust(); err == nil {
		t.Fatal("external env file was trusted")
	}
	if err := os.MkdirAll(filepath.Join(root, ".vsm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(outside, filepath.Join(root, ".vsm", "task-cache")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out.txt"), []byte("output"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = writeTaskCache(*w, PlannedTask{Name: "build", Definition: config.Task{Outputs: []string{"out.txt"}}}, "key")
	if err == nil {
		t.Fatal("task cache escaped workspace")
	}
	files, _ := os.ReadDir(outside)
	if len(files) != 1 {
		t.Fatal("wrote outside project")
	}
}

func TestImportMisePreviewAndApplyDoNotOverwrite(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "mise.json")
	_ = os.WriteFile(source, []byte(`{"go":[{"version":"1.25.11","requested_version":"1.25"}]}`), 0o600)
	preview, err := ImportMise(root, source, false)
	if err != nil || preview.Applied || preview.Tools["go"] != "1.25.11" {
		t.Fatalf("bad preview: %+v %v", preview, err)
	}
	if _, err := os.Stat(filepath.Join(root, "vsm.json")); !os.IsNotExist(err) {
		t.Fatal("preview wrote configuration")
	}
	if _, err := ImportMise(root, source, true); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportMise(root, source, true); err == nil {
		t.Fatal("existing configuration overwritten")
	}
}

func TestTaskListingRedactsEnvironmentValues(t *testing.T) {
	root := t.TempDir()
	c := config.Workspace{Schema: 1, Tasks: map[string]config.Task{"test": {Command: []string{"go", "test"}, Env: map[string]string{"API_TOKEN": "private-secret"}}}}
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanTasks(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(plan)
	if err != nil || strings.Contains(string(data), "private-secret") {
		t.Fatal("task listing leaked environment values")
	}
}

func TestStaleWorkspaceSnapshotCannotUseNewTrust(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "vsm.json")
	c := config.Workspace{Schema: 1, Env: map[string]string{"MODE": "old"}}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	old, err := FindWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	c.Env["MODE"] = "new"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	current, err := FindWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := current.Trust(); err != nil {
		t.Fatal(err)
	}
	if old.Trusted() {
		t.Fatal("old execution plan accepted trust for a different configuration")
	}
	if _, err := old.Environment(); err == nil {
		t.Fatal("stale snapshot loaded newly trusted environment")
	}
}

func TestRecursiveTaskInputsMatchRootAndNestedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"main.go", "src/child.go"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file)), []byte("package test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := hashWorkspaceFiles(root, []string{"**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "child.go"), []byte("package changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	other, err := hashWorkspaceFiles(root, []string{"**/*.go"})
	if err != nil || other == digest {
		t.Fatal("recursive task input change was ignored")
	}
}
