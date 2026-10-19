package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseCLIConfigurationWithoutDaemon(t *testing.T) {
	bin, home, cwd := buildVsm(t), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, ".vsm"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".vsm", "project.json"), []byte("broken project"), 0600); err != nil {
		t.Fatal(err)
	}
	// Database service controls must remain available independently of project config.
	for _, args := range [][]string{{"db", "use", "postgres", "17", "--name", "dev", "--port", "55432", "--json"}, {"db", "switch", "dev", "18", "--json"}, {"db", "list", "--json"}, {"database", "engines"}} {
		code, out := runVsm(t, bin, home, cwd, args...)
		if code != 0 || !json.Valid([]byte(out)) {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	code, out := runVsm(t, bin, home, cwd, "db", "connection", "dev", "--json")
	if code != 0 {
		t.Fatal(out)
	}
	var connection struct {
		Password string `json:"password"`
		Port     int    `json:"port"`
	}
	if err := json.Unmarshal([]byte(out), &connection); err != nil || connection.Password == "" || connection.Port != 55432 {
		t.Fatalf("invalid connection JSON: %v", err)
	}
	_, status := runVsm(t, bin, home, cwd, "db", "list", "--json")
	if strings.Contains(status, connection.Password) {
		t.Fatal("status leaked database password")
	}
	for _, args := range [][]string{{"db", "use", "postgres", "latest"}, {"db", "use", "postgres", "17", "--name", "../escape"}, {"db", "use", "mysql", "8.4", "--port", "bad"}, {"db", "exec", "dev", "--", "--help"}, {"db", "switch", "dev"}} {
		if code, out := runVsm(t, bin, home, cwd, args...); code == 0 {
			t.Fatalf("invalid/inactive operation accepted: %v %s", args, out)
		}
	}
	if code, out := runVsm(t, bin, home, cwd, "db", "stop", "dev"); code != 0 {
		t.Fatal(out)
	}
}
