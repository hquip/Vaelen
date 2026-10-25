package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/plugin"
)

func TestHostGuideCLIExplainsLimitsAndRefusesVersionActions(t *testing.T) {
	bin, home, cwd := buildVsm(t), t.TempDir(), t.TempDir()
	for _, id := range []string{"vba", "abap", "verse", "plc"} {
		code, out := runVsm(t, bin, home, cwd, "tools", "info", id, "--json")
		var row plugin.CatalogTool
		if code != 0 || json.Unmarshal([]byte(out), &row) != nil || row.Installable || row.Support.Guide == nil {
			t.Fatalf("guide info %s: %d %s", id, code, out)
		}
	}
	for _, args := range [][]string{{"install", "vba"}, {"use", "-g", "abap", "1"}, {"use", "verse", "1"}} {
		code, out := runVsm(t, bin, home, cwd, args...)
		if code == 0 || !strings.Contains(out, "tools info") {
			t.Fatalf("hosted language action accepted or unexplained: %v %d %s", args, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(cwd, ".tool-versions")); !os.IsNotExist(err) {
		t.Fatal("host guide created a project version file")
	}
	if code, _ := runVsm(t, bin, home, cwd, "tools", "info", "unknown-guide"); code == 0 {
		t.Fatal("unknown guide accepted")
	}
}
