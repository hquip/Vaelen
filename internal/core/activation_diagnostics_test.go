package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/config"
	"vsm/internal/logging"
)

func TestActivationSnapshotDistinguishesOverridesAndCommandOrder(t *testing.T) {
	a := activationFixture(t)
	t.Setenv("VSM_LOG_LEVEL", "info")
	a.TraceID = "diagnostic-test-action"
	if err := a.SetGlobalVersion("java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := config.SetIn(filepath.Join(dir, ".tool-versions"), "java", "21.0.1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(a.Paths.InstallDir("java", "21.0.1"), "bin")+string(os.PathListSeparator)+a.Paths.Shims())
	t.Setenv("VSM_TEST_API_TOKEN", "do-not-record-this-value")
	a.LogActivationSnapshot("current-process", "manual", "", dir, []string{"java"})
	store, _ := logging.Default()
	snapshot, err := store.Read(100, "")
	if err != nil {
		t.Fatal(err)
	}
	var environment, route logging.Fields
	for _, entry := range snapshot.Entries {
		if entry.Event == "environment.snapshot" {
			environment = entry.Fields
		}
		if entry.Event == "command.route" {
			route = entry.Fields
		}
	}
	if environment["scope"] != "current-process" || environment["shims_index"] != float64(1) || environment["operation_id"] != a.TraceID {
		t.Fatalf("missing actual routing context: %+v", environment)
	}
	if route["global_version"] != "17.0.12" || route["effective_version"] != "21.0.1" || route["source"] != filepath.Join(dir, ".tool-versions") {
		t.Fatalf("project override lost: %+v", route)
	}
	if candidates, ok := route["candidates"].([]string); !ok || len(candidates) < 2 {
		t.Fatalf("actual command order omitted: %+v", route)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "do-not-record-this-value") || strings.Contains(string(raw), "VSM_TEST_API_TOKEN") {
		t.Fatal("unrelated environment was recorded")
	}
}
