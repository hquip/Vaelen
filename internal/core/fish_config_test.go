package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFishConfigUsesXDGAndKeepsExistingContent(t *testing.T) {
	home, custom := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", custom)
	want := filepath.Join(custom, "fish", "config.fish")
	if got, err := ShellRCFile("fish", home); err != nil || got != want {
		t.Fatalf("XDG config ignored: %s %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.MkdirAll(filepath.Dir(want), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(want, []byte("# existing fish config\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if got, _, err := PersistShellHook("fish", "vsm"); err != nil || got != want {
			t.Fatalf("wrong persisted config: %s %v", got, err)
		}
		if data, err := os.ReadFile(want); err != nil || !strings.HasPrefix(string(data), "# existing fish config\n") {
			t.Fatalf("existing config changed: %s %v", data, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".config", "fish", "config.fish")); !os.IsNotExist(err) {
			t.Fatal("wrote unused config")
		}
	}
	t.Setenv("XDG_CONFIG_HOME", "relative-is-not-valid-xdg")
	if got, err := ShellRCFile("fish", home); err != nil || got != filepath.Join(home, ".config", "fish", "config.fish") {
		t.Fatalf("relative XDG not ignored: %s %v", got, err)
	}
}
