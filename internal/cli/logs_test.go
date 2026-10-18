package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/logging"
)

func TestCLILogsEndToEnd(t *testing.T) {
	bin := buildVsm(t)
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("VSM_LOG_LEVEL", "info")
	if code, out := runVsm(t, bin, home, cwd, "use", "go", ""); code == 0 {
		t.Fatalf("fixture should fail: %s", out)
	}
	if code, out := runVsm(t, bin, home, cwd, "proxy", "http://private-user:private-password@localhost:7890"); code != 0 {
		t.Fatal(out)
	}
	if code, out := runVsm(t, bin, home, cwd, "proxy", "private-user:private-password@%invalid"); code == 0 {
		t.Fatalf("fixture should fail: %s", out)
	}
	read := func() logging.Snapshot {
		t.Helper()
		cmd := exec.Command(bin, "logs", "--json")
		cmd.Dir, cmd.Env = cwd, append(os.Environ(), "VSM_HOME="+home)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("logs failed: %v", err)
		}
		var snapshot logging.Snapshot
		if err := json.Unmarshal(out, &snapshot); err != nil {
			t.Fatalf("stdout corrupted: %s", out)
		}
		if strings.Contains(string(out), "private-") {
			t.Fatal("credentials/arguments leaked")
		}
		return snapshot
	}
	snapshot := read()
	found := false
	for _, entry := range snapshot.Entries {
		if entry.Event == "command.failed" && entry.Fields["command"] == "use" && entry.Fields["exit_code"] == float64(1) {
			found = true
		}
	}
	if !found {
		t.Fatal("CLI failure was not recorded")
	}
	if len(read().Entries) != len(snapshot.Entries) {
		t.Fatal("reading logs added more log entries")
	}
	// Broken project and global configuration must not block diagnostics.
	if err := os.MkdirAll(filepath.Join(cwd, ".vsm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".vsm", "project.json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "install-root"), []byte(filepath.VolumeName(home)+string(os.PathSeparator)), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(read().Entries) != len(snapshot.Entries) {
		t.Fatal("broken configuration blocked logs")
	}
	dest := filepath.Join(t.TempDir(), "diagnostics.jsonl")
	if code, out := runVsm(t, bin, home, cwd, "logs", "export", dest); code != 0 {
		t.Fatal(out)
	}
	if data, err := os.ReadFile(dest); err != nil || !strings.Contains(string(data), "command.failed") {
		t.Fatalf("export failed: %v", err)
	}
	for _, args := range [][]string{{"--tail", "0"}, {"--tail", "1001"}, {"--tail"}, {"--level", "invalid"}, {"--level"}, {"clear", "extra"}, {"export"}} {
		if code, _ := runVsm(t, bin, home, cwd, append([]string{"logs"}, args...)...); code == 0 {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	if code, out := runVsm(t, bin, home, cwd, "logs", "clear"); code != 0 {
		t.Fatal(out)
	}
	if len(read().Entries) != 0 {
		t.Fatal("clear left log records")
	}
}

func TestLoggingFailureDoesNotChangeCommandOutput(t *testing.T) {
	bin := buildVsm(t)
	home, cwd := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "logs"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "current", "--json")
	cmd.Dir, cmd.Env = cwd, append(os.Environ(), "VSM_HOME="+home)
	out, err := cmd.Output()
	if err != nil || !json.Valid(out) {
		t.Fatalf("log error broke command: %v %s", err, out)
	}
	data, _ := os.ReadFile(filepath.Join(home, "logs"))
	if string(data) != "blocked" {
		t.Fatal("logger overwrote unrelated file")
	}
}
