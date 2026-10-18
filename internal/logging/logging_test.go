package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) Store {
	t.Helper()
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_LOG_LEVEL", "info")
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLevelsRedactionAndFailedOperation(t *testing.T) {
	s := testStore(t)
	t.Setenv("VSM_TEST_API_TOKEN", "secret-from-environment")
	Record("debug", "test", "hidden", nil)
	finish := Start("test", "download", Fields{"url": "https://user:private@example.org/archive?signature=secret-query#secret-fragment", "access_token": "private-token"})
	finish(fmt.Errorf("Bearer abcdefgh; password=private-password; secret-from-environment"))
	snapshot, err := s.Read(100, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 2 || snapshot.Entries[1].Level != "error" || snapshot.Entries[1].Event != "download.failed" {
		t.Fatalf("wrong records: %+v", snapshot)
	}
	raw, _ := json.Marshal(snapshot)
	for _, secret := range []string{"user:", "private", "secret-query", "secret-fragment", "abcdefgh", "secret-from-environment"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	for _, entry := range snapshot.Entries {
		if entry.PID != os.Getpid() || entry.Version == "" || entry.Time == "" {
			t.Fatal("missing diagnostic context")
		}
	}
	t.Setenv("VSM_LOG_LEVEL", "off")
	Record("error", "test", "disabled", nil)
	snapshot, err = s.Read(100, "")
	if err != nil || len(snapshot.Entries) != 2 {
		t.Fatalf("off failed: %+v %v", snapshot, err)
	}
	t.Setenv("VSM_LOG_LEVEL", "debug")
	Record("debug", "test", "visible", nil)
	snapshot, err = s.Read(100, "error")
	if err != nil || len(snapshot.Entries) != 1 {
		t.Fatalf("filter failed: %+v %v", snapshot, err)
	}
}

func TestRotationExportAndClear(t *testing.T) {
	s := testStore(t)
	s.maxBytes, s.backups = 650, 2
	for i := 0; i < 30; i++ {
		if err := s.write("info", "test", "entry", Fields{"index": i}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := s.Read(3, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 3 || len(snapshot.Entries) != 3 || snapshot.Entries[2].Fields["index"] != float64(29) {
		t.Fatalf("rotation/tail failed: %+v", snapshot)
	}
	for _, file := range snapshot.Files {
		if file.Bytes > s.maxBytes {
			t.Fatal("file grew beyond limit")
		}
	}
	path := filepath.Join(t.TempDir(), "中文 diagnostics.jsonl")
	if err := s.Export(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) <= len(snapshot.Entries) {
		t.Fatal("export only included displayed tail")
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal("invalid exported JSONL")
		}
	}
	if err := s.Export(path); err == nil {
		t.Fatal("export overwrote existing file")
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != string(data) {
		t.Fatal("existing export changed")
	}
	if err := s.Export(filepath.Join(s.dir, "other.jsonl")); err == nil {
		t.Fatal("export into active log directory accepted")
	}
	keep := filepath.Join(s.dir, "unrelated.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.Read(100, "")
	if err != nil || len(snapshot.Entries) != 0 || len(snapshot.Files) != 0 {
		t.Fatalf("clear failed: %+v %v", snapshot, err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("clear removed unrelated data")
	}
}

func TestConcurrentProcessesAndGoroutines(t *testing.T) {
	s := testStore(t)
	var cmds []*exec.Cmd
	for i := 0; i < 4; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLogWriterProcess$")
		cmd.Env = append(os.Environ(), "VSM_TEST_LOG_WRITER=1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if err := s.write("info", "test", "concurrent", nil); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	}
	snapshot, err := s.Read(1000, "")
	if err != nil || len(snapshot.Entries) != 160 {
		t.Fatalf("records lost/interleaved: %d %v", len(snapshot.Entries), err)
	}
}

func TestLogWriterProcess(t *testing.T) {
	if os.Getenv("VSM_TEST_LOG_WRITER") != "1" {
		return
	}
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := s.write("info", "test", "child", nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBoundsPartialLinesAndPermissions(t *testing.T) {
	s := testStore(t)
	if err := s.write("error", "test", "large", Fields{"error": strings.Repeat("a", maxEntryBytes*2)}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Read(100, "")
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Fields["truncated"] != true {
		t.Fatalf("unbounded entry: %+v %v", snapshot, err)
	}
	f, err := os.OpenFile(filepath.Join(s.dir, fileName), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"truncated`)
	_ = f.Close()
	snapshot, err = s.Read(100, "")
	if err != nil || len(snapshot.Entries) != 1 {
		t.Fatalf("partial line prevented recovery: %v", err)
	}
	if err := s.write("info", "test", "after_crash", nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.Read(100, "")
	if err != nil || len(snapshot.Entries) != 2 || snapshot.Entries[1].Event != "after_crash" {
		t.Fatalf("next event lost after crash: %+v %v", snapshot, err)
	}
	for _, tail := range []int{-1, 0, 1001} {
		if _, err := s.Read(tail, ""); err == nil {
			t.Fatal("invalid tail accepted")
		}
	}
	if _, err := s.Read(1, "invalid"); err == nil {
		t.Fatal("invalid level accepted")
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{s.dir: 0o700, filepath.Join(s.dir, fileName): 0o600} {
			fi, err := os.Stat(path)
			if err != nil || fi.Mode().Perm() != mode {
				t.Fatalf("insecure permissions: %s %v", path, err)
			}
		}
	}
}

func TestRefusesLinkedLogDirectoryAndFiles(t *testing.T) {
	s := testStore(t)
	outside := t.TempDir()
	var err error
	if runtime.GOOS == "windows" {
		err = exec.Command("cmd", "/c", "mklink", "/J", s.dir, outside).Run()
	} else {
		err = os.Symlink(outside, s.dir)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := s.write("error", "test", "unsafe", nil); err == nil {
		t.Fatal("linked directory accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("wrote through linked directory")
	}
	// Remove only the link created above, never its target.
	if err := os.Remove(s.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.dir, fileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err == nil {
		t.Fatal("clear accepted a directory in place of a log file")
	}
}
