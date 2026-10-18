package main

import (
	"os"
	"path/filepath"
	"testing"

	"vsm/internal/config"
)

func TestWorkbenchCanRepairInvalidConfigAndTrustIsInvalidated(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	if err := a.SetProjectDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vsm.json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := a.Workbench()
	if err != nil || !status.Configured || status.ConfigError == "" {
		t.Fatalf("invalid config not repairable: %+v %v", status, err)
	}
	content, err := a.WorkspaceConfiguration()
	if err != nil || content != "broken" {
		t.Fatal("could not view broken document")
	}
	valid := `{"schema_version":1,"tools":{},"tasks":{"test":{"command":["go","test"]}}}`
	status, err = a.SaveWorkspaceConfiguration(valid)
	if err != nil || status.ConfigError != "" || len(status.Tasks) != 1 || status.Trusted {
		t.Fatalf("repair failed: %+v %v", status, err)
	}
	status, err = a.TrustWorkspace()
	if err != nil || !status.Trusted {
		t.Fatalf("trust failed: %v", err)
	}
	status, err = a.SaveWorkspaceConfiguration(`{"schema_version":1,"env":{"APP_MODE":"new"}}`)
	if err != nil || status.Trusted {
		t.Fatal("editing configuration retained trust")
	}
	if _, err := a.SaveWorkspaceConfiguration(`{"schema_version":1,"env":{"PATH":"outside"}}`); err == nil {
		t.Fatal("unsafe editor configuration accepted")
	}
	w, err := config.LoadWorkspace(filepath.Join(dir, "vsm.json"))
	if err != nil || w.Env["APP_MODE"] != "new" {
		t.Fatal("invalid edit damaged existing file")
	}
}

func TestWorkbenchCancellationIsIndependentOfMutationLock(t *testing.T) {
	a := newTestApp(t)
	called := false
	a.workMu.Lock()
	a.taskCancel = func() { called = true }
	a.workMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.CancelWorkspaceTask() || !called {
		t.Fatal("busy mutation lock prevented cancellation")
	}
}
