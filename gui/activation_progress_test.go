package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"vsm/internal/buildinfo"
	"vsm/internal/updater"
)

func TestCompanionPreparationExplainsBusyAndReleasesAfterCompletion(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "cli.go")
	code := fmt.Sprintf("package main\nimport \"fmt\"\nfunc main(){fmt.Println(%q)}\n", "vsm "+buildinfo.DisplayVersion())
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "matching-cli")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); err != nil {
		t.Fatalf("build CLI fixture: %v %s", err, out)
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"ready", nil}, {"failed", errors.New("download fixture failed")}, {"cancelled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			t.Setenv("PATH", t.TempDir())
			c := *a.c()
			c.act.VsmExe = ""
			a.st.Store(&c)
			entered, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(func() {
				unblock()
				select {
				case <-stopped:
				case <-time.After(5 * time.Second):
					t.Error("companion preparation did not finish")
				}
			})
			var progress []updater.Progress
			a.cliProgress = func(p updater.Progress) { progress = append(progress, p) }
			a.cliInstaller = func(context.Context, string, string) (string, error) {
				close(entered)
				<-release
				return bin, tc.err
			}
			result := make(chan error, 1)
			go func() {
				// No Java is installed in the temporary VSM_HOME: the successful
				// preparation case stops before writing any activation settings.
				result <- a.SetGlobal("java", "21")
				close(stopped)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("switch did not reach companion preparation")
			}
			if !a.cliPreparing.Load() {
				t.Fatal("pending companion download was not marked")
			}
			_, err := a.PathInit()
			if !errors.Is(err, errBusy) || !strings.Contains(err.Error(), "配套 CLI") {
				t.Fatalf("PATH activation did not identify the actual pending operation: %v", err)
			}
			if _, err := a.AdoptDetected(nil); !errors.Is(err, errBusy) || !strings.Contains(err.Error(), "配套 CLI") {
				t.Fatalf("adoption did not identify the actual pending operation: %v", err)
			}
			unblock()
			err = <-result
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("lost preparation failure: %v", err)
			}
			wantStage := "failed"
			if tc.err == nil {
				wantStage = "ready"
				if a.c().act.VsmExe != bin {
					t.Fatal("prepared CLI was not selected")
				}
			}
			if len(progress) != 2 || progress[0].Stage != "checking" || progress[1].Stage != wantStage {
				t.Fatalf("missing preparation lifecycle events: %+v", progress)
			}
			if a.cliPreparing.Load() {
				t.Fatal("preparation state remained set after completion")
			}
			done, err := a.begin()
			if err != nil {
				t.Fatalf("write lock remained held: %v", err)
			}
			done()
		})
	}
}
