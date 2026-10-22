package updater

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateLockRecoversOldFileAndPreventsConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".vsm-update.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireUpdateLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireUpdateLock(dir); err == nil {
		second()
		t.Fatal("concurrent writer acquired lock")
	}
	unlock()
	unlock, err = acquireUpdateLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestUpdateLockReleasesAfterProcessDeath(t *testing.T) {
	if dir := os.Getenv("VSM_TEST_UPDATE_LOCK_CHILD"); dir != "" {
		unlock, err := acquireUpdateLock(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		fmt.Println("LOCKED")
		for {
			time.Sleep(time.Second)
		}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateLockReleasesAfterProcessDeath$")
	cmd.Env = append(os.Environ(), "VSM_TEST_UPDATE_LOCK_CHILD="+dir)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	if line, err := bufio.NewReader(pipe).ReadString('\n'); err != nil || line != "LOCKED\n" {
		t.Fatalf("child lock: %q %v", line, err)
	}
	if unlock, err := acquireUpdateLock(dir); err == nil {
		unlock()
		t.Fatal("child lock ignored")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	unlock, err := acquireUpdateLock(dir)
	if err != nil {
		t.Fatalf("dead process retained lock: %v", err)
	}
	unlock()
}
