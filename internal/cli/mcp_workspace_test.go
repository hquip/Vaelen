package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/config"
	"vsm/internal/core"
)

func TestMCPWorkspaceToolsRespectTrustAndProjectBoundary(t *testing.T) {
	bin := buildVsm(t)
	t.Setenv("VSM_HOME", t.TempDir())
	root, err := core.ProjectDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.InitProject(root, "versions", ""); err != nil {
		t.Fatal(err)
	}
	c := config.Workspace{Schema: 1, Tasks: map[string]config.Task{"all": {Depends: []string{"noop"}}, "noop": {Depends: []string{"leaf"}}, "leaf": {Command: []string{"echo", "literal"}, Env: map[string]string{"API_TOKEN": "never-leak"}}}}
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	result, err := mcpCall(context.Background(), bin, root, "vsm_project_inspect", json.RawMessage(`{}`))
	if err != nil || result.IsError {
		t.Fatalf("inspect failed: %+v %v", result, err)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "never-leak") {
		t.Fatal("MCP inspection leaked task environment")
	}
	result, err = mcpCall(context.Background(), bin, root, "vsm_task_run", json.RawMessage(`{"task":"all"}`))
	if err != nil || !result.IsError {
		t.Fatal("MCP ran untrusted task")
	}
	if _, err := mcpCall(context.Background(), bin, root, "vsm_task_run", json.RawMessage(`{"task":"all","project":"outside"}`)); err == nil {
		t.Fatal("MCP accepted project redirection")
	}
	if _, err := mcpCall(context.Background(), bin, root, "vsm_task_run", json.RawMessage(`{"task":"all","jobs":0}`)); err == nil {
		t.Fatal("MCP accepted zero job count")
	}
	if _, err := mcpCall(context.Background(), bin, root, "vsm_task_run", json.RawMessage(`{"task":"all","trust":true}`)); err == nil {
		t.Fatal("MCP granted trust")
	}
	if _, err := os.Stat(filepath.Join(root, "leaf")); !os.IsNotExist(err) {
		t.Fatal("untrusted task changed project")
	}
	c.Tasks["version"] = config.Task{Command: []string{"go", "version"}}
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	w, err := core.FindWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Trust(); err != nil {
		t.Fatal(err)
	}
	result, err = mcpCall(context.Background(), bin, root, "vsm_task_run", json.RawMessage(`{"task":"version"}`))
	if err != nil || result.IsError {
		t.Fatalf("trusted MCP task failed: %+v %v", result, err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	result, err = mcpCall(cancelled, bin, root, "vsm_task_run", json.RawMessage(`{"task":"version"}`))
	if err != nil || !result.IsError {
		t.Fatal("MCP cancellation ignored")
	}
	readOnly := mcpTools(true)
	if len(readOnly) != 7 {
		t.Fatalf("wrong read-only surface: %d", len(readOnly))
	}
	for _, tool := range readOnly {
		if tool.Annotations["readOnlyHint"] != true {
			t.Fatal("mutation exposed in read-only mode")
		}
	}
}
