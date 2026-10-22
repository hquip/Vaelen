package main

import (
	"os"
	"path/filepath"
	"testing"

	"vsm/internal/plugin"
)

func TestGUIRefreshRemovesDeletedExternalTool(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(a.c().paths.Root, "plugins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "refresh-test.json")
	defer func() { _ = os.Remove(file); _ = plugin.ReloadExternalTools() }()
	if err := os.WriteFile(file, []byte(`{"name":"gui_reload_fixture","commands":["gui-reload-fixture"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.ReloadTools(); err != nil {
		t.Fatal(err)
	}
	if _, ok := plugin.Get("gui_reload_fixture"); !ok {
		t.Fatal("tool was not loaded")
	}
	if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.ReloadTools(); err == nil {
		t.Fatal("invalid definition was silently accepted")
	}
	if _, ok := plugin.Get("gui_reload_fixture"); !ok {
		t.Fatal("failed refresh partially replaced registry")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := a.ReloadTools(); err != nil {
		t.Fatal(err)
	}
	for _, name := range a.Tools() {
		if name == "gui_reload_fixture" {
			t.Fatal("deleted plugin still visible after refresh")
		}
	}
}
