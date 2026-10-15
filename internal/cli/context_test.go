package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
