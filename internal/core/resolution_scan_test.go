package core

import (
	"os"
	"path/filepath"
	"testing"

	"vsm/internal/config"
)

func TestResolutionSnapshotIsConsistentAndNextOperationSeesChanges(t *testing.T) {
	paths := Paths{Root: t.TempDir()}
	dir := t.TempDir()
	for _, tool := range []string{"go", "java"} {
		for _, v := range []string{"1.0", "2.0"} {
			if err := os.MkdirAll(paths.InstallDir(tool, v), 0755); err != nil {
				t.Fatal(err)
			}
		}
	}
	file := filepath.Join(dir, ".tool-versions")
	if err := os.WriteFile(file, []byte("go 1.0\njava 1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := Resolver{Paths: paths, Registry: Registry{Paths: paths}}
	snapshot := r.Snapshot(dir)
	if got := snapshot.Resolve("go", dir); got.Version != "1.0" {
		t.Fatal(got)
	}
	if err := config.SetIn(file, "java", "2.0"); err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Resolve("java", dir); got.Version != "1.0" {
		t.Fatalf("one operation mixed different config reads: %+v", got)
	}
	if got := r.Snapshot(dir).Resolve("java", dir); got.Version != "2.0" {
		t.Fatalf("next operation retained stale config: %+v", got)
	}
	other := t.TempDir()
	if err := config.SetIn(filepath.Join(other, ".tool-versions"), "java", "2.0"); err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Resolve("java", other); got.Version != "2.0" {
		t.Fatalf("snapshot crossed directory scope: %+v", got)
	}
}

func TestResolutionSnapshotKeepsPriorityAndBoundary(t *testing.T) {
	paths := Paths{Root: t.TempDir()}
	root := t.TempDir()
	dir := filepath.Join(root, "child")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"1.0", "2.0", "3.0"} {
		if err := os.MkdirAll(paths.InstallDir("go", v), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetIn(paths.GlobalToolVersions(), "go", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetIn(filepath.Join(root, ".tool-versions"), "go", "2.0"); err != nil {
		t.Fatal(err)
	}
	r := Resolver{Paths: paths, Registry: Registry{Paths: paths}, Boundary: dir}
	if got := r.Snapshot(dir).Resolve("go", dir); got.Version != "1.0" {
		t.Fatalf("project boundary ignored: %+v", got)
	}
	t.Setenv("VSM_GO_VERSION", "3.0")
	if got := r.Snapshot(dir).Resolve("go", dir); got.Version != "3.0" {
		t.Fatal(got)
	}
	r.Pinned = map[string]string{"go": "2.0"}
	if got := r.Snapshot(dir).Resolve("go", dir); got.Version != "2.0" {
		t.Fatal(got)
	}
}
