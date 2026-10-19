package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/core"
)

type fakeDriver struct {
	m      *Manager
	active string
	events []string
	fail   string
	cancel context.CancelFunc
}

func (f *fakeDriver) running(ctx context.Context, i *Instance) (bool, error) {
	return f.active == i.Version, nil
}
func (f *fakeDriver) start(ctx context.Context, i *Instance) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.events = append(f.events, "start "+i.Version)
	if _, err := f.m.credentials(i); err != nil {
		return err
	}
	v := i.Versions[i.Version]
	v.Allocated = true
	v.ImageID = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(i.Version)))
	v.ActualVersion = i.Version + ".1"
	if err := f.m.saveVersion(i); err != nil {
		return err
	}
	f.active = i.Version
	if i.Version == f.fail {
		if f.cancel != nil {
			f.cancel()
			return ctx.Err()
		}
		return fmt.Errorf("simulated startup failure")
	}
	v.Initialized = true
	return f.m.saveVersion(i)
}
func (f *fakeDriver) stop(ctx context.Context, i *Instance) error {
	f.events = append(f.events, "stop "+i.Version)
	if f.active == i.Version {
		f.active = ""
	}
	return nil
}
func (f *fakeDriver) logs(context.Context, *Instance) (string, error)           { return "sample log", nil }
func (f *fakeDriver) exec(context.Context, *Instance, []string) (string, error) { return "1", nil }
func fakeManager(t *testing.T) (*Manager, *fakeDriver) {
	t.Helper()
	paths := core.Paths{Root: t.TempDir()}
	t.Setenv("VSM_HOME", paths.Root)
	m := New(paths)
	f := &fakeDriver{m: m}
	m.driverFactory = func(*Manager, *Instance) driver { return f }
	return m, f
}
func configureTest(t *testing.T, m *Manager) {
	t.Helper()
	if _, err := m.Use(context.Background(), Request{Name: "dev", Engine: "postgres", Backend: "docker", Version: "17", Port: 15432}); err != nil {
		t.Fatal(err)
	}
}

func TestVersionSwitchRetainsSelectionsAndPinnedHistory(t *testing.T) {
	m, f := fakeManager(t)
	configureTest(t, m)
	if len(f.events) != 0 {
		t.Fatal("configuration started a service")
	}
	ctx := context.Background()
	if _, err := m.Start(ctx, "dev"); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"18", "17"} {
		state, err := m.Switch(ctx, "dev", version)
		if err != nil || !state.Running || state.Version != version {
			t.Fatalf("%+v %v", state, err)
		}
	}
	i, err := m.load("dev")
	if err != nil || len(i.Versions) != 2 || !i.Versions["18"].Initialized {
		t.Fatalf("history lost: %+v %v", i, err)
	}
	if got := strings.Join(f.events, ","); got != "start 17,stop 17,start 18,stop 18,start 17" {
		t.Fatal(got)
	}
}
func TestSwitchFailureAndCancellationRestoreOldServer(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			m, f := fakeManager(t)
			configureTest(t, m)
			ctx := context.Background()
			if _, err := m.Start(ctx, "dev"); err != nil {
				t.Fatal(err)
			}
			f.fail = "18"
			if cancelled {
				ctx, f.cancel = context.WithCancel(ctx)
				defer f.cancel()
			}
			if _, err := m.Switch(ctx, "dev", "18"); err == nil || !strings.Contains(err.Error(), "已恢复旧版本") {
				t.Fatalf("missing rollback: %v", err)
			}
			i, err := m.load("dev")
			if err != nil || i.Version != "17" || f.active != "17" || i.Versions["18"] == nil {
				t.Fatalf("rollback corrupted state: %+v active=%s %v", i, f.active, err)
			}
		})
	}
}
func TestValidationAndOwnershipNeverOverwriteOtherInstances(t *testing.T) {
	m, _ := fakeManager(t)
	ctx := context.Background()
	for _, r := range []Request{{Name: "../escape", Engine: "postgres", Version: "17"}, {Name: "dev", Engine: "unknown", Version: "17"}, {Name: "dev", Engine: "postgres", Version: "latest"}, {Name: "dev", Engine: "postgres", Version: "17", Port: -1}} {
		if _, err := m.Use(ctx, r); err == nil {
			t.Fatalf("invalid request accepted: %+v", r)
		}
	}
	configureTest(t, m)
	if _, err := m.Use(ctx, Request{Name: "dev", Engine: "mysql", Version: "8.4"}); err == nil {
		t.Fatal("changed existing engine")
	}
	if _, err := m.Use(ctx, Request{Name: "dev", Engine: "postgres", Version: "17", Backend: "bad"}); err == nil {
		t.Fatal("invalid backend accepted")
	}
	unlock, err := m.lock("dev")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := m.Start(ctx, "dev"); err == nil {
		t.Fatal("concurrent writer accepted")
	}
}
func TestCredentialsStayPrivateAndNeverSilentlyReset(t *testing.T) {
	m, _ := fakeManager(t)
	configureTest(t, m)
	c, err := m.Connection("dev")
	if err != nil || c.Password == "" {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), c.Password) {
		t.Fatal("status leaked credentials")
	}
	if err := os.Remove(m.path("dev", "credentials.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Connection("dev"); err == nil {
		t.Fatal("missing credentials were regenerated against existing data")
	}
}
func TestStoreRejectsLinkedRoot(t *testing.T) {
	m, _ := fakeManager(t)
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	m.Root = link
	if _, err := m.Use(context.Background(), Request{Name: "dev", Engine: "postgres", Version: "17"}); err == nil {
		t.Fatal("linked database root accepted")
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("linked target modified")
	}
}
func TestReportedVersionMustMatchRequestedVersion(t *testing.T) {
	for _, test := range []struct {
		request, output string
		valid           bool
	}{{"17", "postgres (PostgreSQL) 17.9", true}, {"17", "postgres (PostgreSQL) 18.1", false}, {"7.4-alpine", "Redis server v=7.4.7", true}, {"8.4", "mysqld Ver 8.0.32", false}, {"custom-pg", "postgres 16.9", true}} {
		_, err := checkReportedVersion(test.request, []byte(test.output))
		if (err == nil) != test.valid {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}
