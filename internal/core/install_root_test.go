package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/config"
)

func TestInstallRootChangeAllowsPreparedTargetAndBrokenRootRecovery(t *testing.T) {
	a := activationFixture(t)
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	originalExe, err := a.Which("java", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "prepared-target")
	rel, _ := filepath.Rel(a.Paths.Installs(), originalExe)
	newExe := filepath.Join(root, rel)
	if err := config.WriteFileAtomic(newExe, []byte("target runtime"), 0755); err != nil {
		t.Fatal(err)
	}
	next, err := a.Paths.SetInstallRoot(root)
	if err != nil || !sameDir(next.Installs(), root) {
		t.Fatalf("prepared root rejected: %+v %v", next, err)
	}
	loaded, err := DefaultPaths()
	if err != nil || loaded.Installs() != root {
		t.Fatalf("new CLI process would use a different root: %+v %v", loaded, err)
	}
	if data, err := os.ReadFile(originalExe); err != nil || string(data) != "fixture" {
		t.Fatalf("original installation moved or modified: %s %v", data, err)
	}
	// Emulate an old release saving an empty root. Restoring the valid default
	// must remain possible even though the active lookup no longer sees Java.
	empty := filepath.Join(t.TempDir(), "empty-root")
	if err := config.WriteFileAtomic(a.Paths.InstallRootFile(), []byte(empty), 0644); err != nil {
		t.Fatal(err)
	}
	broken, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := broken.SetInstallRoot("")
	if err != nil || restored.InstallsRoot != "" || !sameDir(restored.Installs(), a.Paths.Installs()) {
		t.Fatalf("cannot recover original installations: %+v %v", restored, err)
	}
	if _, err := os.Stat(a.Paths.InstallRootFile()); !os.IsNotExist(err) {
		t.Fatalf("restore default did not remove override: %v", err)
	}
}

func TestInstallRootChangeRejectsEmptyDefaultAndIsolation(t *testing.T) {
	a := activationFixture(t)
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	// A populated custom root must receive the same protection on "default".
	paths := a.Paths
	paths.Root = t.TempDir()
	paths.InstallsRoot = a.Paths.Installs()
	if err := config.SetIn(paths.GlobalToolVersions(), "java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	if _, err := paths.SetInstallRoot(""); err == nil {
		t.Fatal("empty default silently hid a working custom installation")
	}
	for _, mode := range []string{"local", "docker"} {
		paths.ProjectMode = mode
		if _, err := paths.SetInstallRoot(t.TempDir()); err == nil {
			t.Fatalf("%s project allowed global root changes", mode)
		}
	}
}

func TestUnavailableVersionIncludesRequirementRootAndVisibleVersions(t *testing.T) {
	a := activationFixture(t)
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "java", "99.0.0"); err != nil {
		t.Fatal(err)
	}
	_, err := a.Which("java", t.TempDir())
	var missing *VersionUnavailableError
	if !errors.As(err, &missing) || missing.Spec != "99.0.0" || missing.Source != "全局" || missing.InstallRoot != a.Paths.Installs() || len(missing.Installed) != 2 {
		t.Fatalf("missing installation misreported as a runtime execution failure: %v", err)
	}
	for _, want := range []string{"99.0.0", a.Paths.Installs(), "17.0.12", "21.0.1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing diagnostic %q: %v", want, err)
		}
	}
}
