package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vsm/internal/core"
)

func TestIssue10NetworkSettingsAndCurrent(t *testing.T) {
	bin := buildVsm(t)
	home, dir := t.TempDir(), t.TempDir()
	if _, err := core.InitProject(dir, "local", ""); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mirror", "set", "github", "https://mirror.example/"}, {"mirror", "set", "github-api", "https://api.example/"}, {"proxy", "socks5h://127.0.0.1:1080"}} {
		if code, out := runVsm(t, bin, home, dir, args...); code != 0 {
			t.Fatalf("%v: %s", args, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "mirror-sources.json")); err != nil {
		t.Fatal("custom sources not saved globally")
	}
	if mode, err := os.ReadFile(filepath.Join(home, "mirror")); err != nil || strings.TrimSpace(string(mode)) != "custom" {
		t.Fatal("custom sources not enabled or reported")
	}
	if _, err := os.Stat(filepath.Join(dir, ".vsm", "env", "proxy")); !os.IsNotExist(err) {
		t.Fatal("proxy written into project cache")
	}
	for _, args := range [][]string{{"mirror", "list", "--json"}, {"proxy", "--json"}} {
		code, out := runVsm(t, bin, home, dir, args...)
		if code != 0 || !json.Valid([]byte(out)) {
			t.Fatalf("not valid JSON: %s", out)
		}
	}
	if code, out := runVsm(t, bin, home, dir, "current"); code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("unused tools were shown: %s", out)
	}
	if code, out := runVsm(t, bin, home, dir, "current", "--all"); code != 0 || !strings.Contains(out, "go") {
		t.Fatal("--all did not show unused tools")
	}
}

func TestUpdateRejectsBadOptionsBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"--version"}} {
		if err := runUpdate(args); err == nil {
			t.Fatalf("invalid update options accepted: %v", args)
		}
	}
}

func TestColorsCanBeDisabled(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	if got := colorize(os.Stdout, "32", "version"); got != "version" {
		t.Fatal("NO_COLOR ignored")
	}
}
