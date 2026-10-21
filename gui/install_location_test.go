package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLocationPreviewMatchesDefaultAndToolSelections(t *testing.T) {
	a := newTestApp(t)
	paths := a.c().paths
	for _, tool := range []string{"go", "java"} {
		want := filepath.Join(paths.Installs(), tool, "<版本>")
		for _, selected := range []string{"", paths.Installs(), paths.ToolInstalls(tool)} {
			got, err := a.InstallLocation(tool, selected)
			if err != nil || got.Target != want || got.Base != "" {
				t.Fatalf("%s selected %s: preview %+v, %v; want %s", tool, selected, got, err, want)
			}
		}
		bad := filepath.Join(paths.ToolInstalls(tool), "nested")
		if _, err := a.InstallLocation(tool, bad); err == nil {
			t.Fatal("preview accepted nested storage")
		}
		// The invalid location must fail before querying any remote versions.
		if err := a.Install(tool, "latest", bad); err == nil || !strings.Contains(err.Error(), "安装位置") {
			t.Fatalf("invalid location did not fail before remote resolution: %v", err)
		}
	}
	if _, err := a.InstallLocation("unknown-tool", ""); err == nil {
		t.Fatal("unknown tool accepted")
	}
}
