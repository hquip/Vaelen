package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/logging"
)

func TestLogsSurviveBrokenProjectAndRedactProxy(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("VSM_LOG_LEVEL", "info")
	project := t.TempDir()
	if err := a.SetProjectDir(project); err != nil {
		t.Fatal(err)
	}
	if _, err := a.InitProjectEnvironment("local", ""); err != nil {
		t.Fatal(err)
	}
	// The generated error contains a malformed, credential-bearing proxy URL.
	if err := a.SetProxy("https://private-user:private-password@%invalid"); err == nil {
		t.Fatal("expected invalid proxy")
	}
	if err := os.WriteFile(filepath.Join(project, ".vsm", "project.json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProjectDir(project); err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.Logs(200, "error")
	if err != nil || len(snapshot.Entries) == 0 {
		t.Fatalf("could not diagnose broken project: %v", err)
	}
	if strings.HasPrefix(snapshot.Directory, project+string(os.PathSeparator)) {
		t.Fatal("logs were stored inside the project environment")
	}
	for _, entry := range snapshot.Entries {
		if strings.Contains(entry.Fields["error"].(string), "private-") {
			t.Fatal("proxy credentials leaked")
		}
	}
	logging.Record("info", "test", "retained", nil)
	if err := a.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = a.Logs(200, "")
	if err != nil || len(snapshot.Entries) != 0 {
		t.Fatalf("clear failed: %+v %v", snapshot, err)
	}
	if _, err := a.ExportLogs(); err == nil {
		t.Fatal("export dialog accepted missing Wails context")
	}
}
