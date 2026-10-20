package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/logging"
)

func TestLogsDiagnoseRecordsCurrentProcessWithoutExecutingTools(t *testing.T) {
	bin, home, dir := buildVsm(t), t.TempDir(), t.TempDir()
	t.Setenv("VSM_HOME", home)
	t.Setenv("VSM_LOG_LEVEL", "info")
	t.Setenv("VSM_JAVA_VERSION", "")
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	toolDir := filepath.Join(home, "installs", "java", "17.0.14", "bin")
	if err := os.MkdirAll(toolDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Deliberately not executable code: diagnostic collection must only inspect
	// routing, without launching arbitrary language installations.
	if err := os.WriteFile(filepath.Join(toolDir, name), []byte("metadata-only fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if code, out := runVsm(t, bin, home, dir, "use", "-g", "java", "17.0.14"); code != 0 {
		t.Fatal(out)
	}
	if code, out := runVsm(t, bin, home, dir, "logs", "diagnose", "java"); code != 0 || !strings.Contains(out, "编号") {
		t.Fatalf("current terminal diagnostics failed: code=%d %s", code, out)
	}
	store, _ := logging.Default()
	snapshot, err := store.Read(100, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range snapshot.Entries {
		if entry.Event == "command.route" && entry.Fields["scope"] == "current-process" && entry.Fields["effective_version"] == "17.0.14" {
			found = true
		}
	}
	if !found {
		t.Fatal("current process routing not recorded")
	}
	// Broken configuration must still produce a capture failure record, while
	// the startup-only capture remains silent and successful for its caller.
	if err := os.WriteFile(filepath.Join(home, "install-root"), []byte(home), 0600); err != nil {
		t.Fatal(err)
	}
	if code, out := runVsm(t, bin, home, dir, "logs", "_capture-cmd", "after", "ON"); code != 0 || out != "" {
		t.Fatalf("logging failure affected cmd startup: code=%d %q", code, out)
	}
	snapshot, _ = store.Read(100, "")
	found = false
	for _, entry := range snapshot.Entries {
		if entry.Event == "capture.failed" && entry.Fields["scope"] == "cmd-session" {
			found = true
		}
	}
	if !found {
		t.Fatal("broken startup configuration was not diagnosed")
	}
	before := len(snapshot.Entries)
	cmd := exec.Command(bin, "logs", "_capture-cmd", "before", "OFF")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VSM_HOME="+home, "VSM_LOG_LEVEL=off")
	if out, err := cmd.CombinedOutput(); err != nil || len(out) != 0 {
		t.Fatalf("disabled capture failed: %v %s", err, out)
	}
	snapshot, _ = store.Read(100, "")
	if len(snapshot.Entries) != before {
		t.Fatal("VSM_LOG_LEVEL=off still recorded startup diagnostics")
	}
}
