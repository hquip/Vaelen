package core

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"vsm/internal/plugin"
)

func TestInstallLocationDoesNotNestToolDirectories(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_MIRROR", "off")
	data := []byte("MZportable-runtime-fixture")
	digest := sha256.Sum256(data)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	artifact := plugin.Artifact{URL: server.URL + "/runtime", Format: "binary", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))}
	for _, tool := range []string{"go", "java"} {
		for _, location := range []string{"root", "tool", "nested", "other-tool", "external"} {
			t.Run(tool+"/"+location, func(t *testing.T) {
				paths := Paths{Root: t.TempDir(), InstallsRoot: filepath.Join(t.TempDir(), "sdk")}
				custom := paths.Installs()
				switch location {
				case "tool":
					custom = paths.ToolInstalls(tool)
				case "nested":
					custom = filepath.Join(paths.ToolInstalls(tool), "extra")
				case "other-tool":
					custom = paths.ToolInstalls("python")
				case "external":
					custom = t.TempDir()
				}
				p, _ := plugin.Get(tool)
				before := requests.Load()
				err := (Installer{Paths: paths, Pinned: &artifact}).Install(p, "1.2.3", custom, nil)
				if location == "nested" || location == "other-tool" {
					if err == nil || !strings.Contains(err.Error(), "安装") {
						t.Fatalf("nested location accepted: %v", err)
					}
					if requests.Load() != before {
						t.Fatal("invalid path triggered download")
					}
					if pathExists(paths.Installs()) {
						t.Fatal("invalid path created installation directories")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				versions, err := (Registry{Paths: paths}).Installed(tool)
				if err != nil || !slices.Equal(versions, []string{"1.2.3"}) {
					t.Fatalf("extra versions: %v %v", versions, err)
				}
				if location != "external" && isLink(paths.InstallDir(tool, "1.2.3")) {
					t.Fatal("default location became a nested custom link")
				}
				if location == "external" && !isLink(paths.InstallDir(tool, "1.2.3")) {
					t.Fatal("external location lost its managed link")
				}
			})
		}
	}
}

func legacyNestedInstall(t *testing.T, paths Paths, tool, version string) string {
	t.Helper()
	name := tool
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	source := t.TempDir()
	p, _ := plugin.Get(tool)
	bin := filepath.Join(p.BinPaths(source)[0], name)
	mkTree(t, source, map[string]string{mustRelative(t, source, bin): "existing-runtime"})
	// Recreate the old installer layout without calling the corrected installer.
	target := filepath.Join(paths.ToolInstalls(tool), tool, version)
	dest := paths.InstallDir(tool, version)
	if _, err := createReceipt(source, dest, target, tool, version); err != nil {
		t.Fatal(err)
	}
	if err := moveInto(source, target); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(target, dest); err != nil {
		t.Fatal(err)
	}
	return target
}

func mustRelative(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestLegacyNestedInstallContainerIsNotVersionOrUninstallTarget(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	for _, tool := range []string{"go", "java"} {
		t.Run(tool, func(t *testing.T) {
			paths := Paths{Root: t.TempDir()}
			target := legacyNestedInstall(t, paths, tool, "1.2.3")
			second := legacyNestedInstall(t, paths, tool, "2.0.0")
			registry := Registry{Paths: paths}
			versions, err := registry.Installed(tool)
			if err != nil || !slices.Equal(versions, []string{"1.2.3", "2.0.0"}) {
				t.Fatalf("storage directory listed as a version: %v %v", versions, err)
			}
			if registry.IsInstalled(tool, tool) {
				t.Fatal("storage container can be selected as an installed version")
			}
			in := Installer{Paths: paths}
			if err := in.Uninstall(tool, tool); err == nil {
				t.Fatal("storage directory uninstall accepted")
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("existing install was removed: %v", err)
			}
			if err := in.Uninstall(tool, "1.2.3"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(second); err != nil {
				t.Fatalf("uninstall deleted the other nested version: %v", err)
			}
			versions, err = registry.Installed(tool)
			if err != nil || !slices.Equal(versions, []string{"2.0.0"}) {
				t.Fatalf("remaining version changed: %v %v", versions, err)
			}
			if err := in.Uninstall(tool, "2.0.0"); err != nil {
				t.Fatal(err)
			}
			versions, err = registry.Installed(tool)
			if err != nil || len(versions) != 0 {
				t.Fatalf("empty storage container returned after uninstall: %v %v", versions, err)
			}
		})
	}
}

func TestInstallLocationResolvesDirectoryAliasesAndKeepsNamedVersions(t *testing.T) {
	paths := Paths{Root: t.TempDir()}
	if err := os.MkdirAll(paths.ToolInstalls("go"), 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "sdk-alias")
	if err := linkDir(paths.Installs(), alias); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{alias, filepath.Join(alias, "go")} {
		base, err := paths.NormalizeInstallBase("go", selection)
		if err != nil || base != "" {
			t.Fatalf("default alias %s not normalized: %s %v", selection, base, err)
		}
	}
	if _, err := paths.NormalizeInstallBase("go", filepath.Join(alias, "go", "nested")); err == nil {
		t.Fatal("directory alias bypassed nesting rejection")
	}
	// A real named installation is still allowed, even if its alias is the tool name.
	external := t.TempDir()
	if err := linkDir(external, paths.InstallDir("go", "go")); err != nil {
		t.Fatal(err)
	}
	versions, err := (Registry{Paths: paths}).Installed("go")
	if err != nil || !slices.Equal(versions, []string{"go"}) {
		t.Fatalf("named external version was hidden: %v %v", versions, err)
	}
}

func TestLocalInstallLocationCannotEscapeThroughDirectoryAlias(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	project, outside := t.TempDir(), t.TempDir()
	alias := filepath.Join(project, "external-tools")
	if err := linkDir(outside, alias); err != nil {
		t.Fatal(err)
	}
	paths := Paths{Root: filepath.Join(project, ".vsm"), ProjectRoot: project, ProjectMode: "local"}
	err := (Installer{Paths: paths}).Install(integrityPlugin{}, "1.2.3", alias, nil)
	if err == nil || !strings.Contains(err.Error(), "项目目录外") {
		t.Fatalf("local install followed an external directory alias: %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("invalid location wrote outside project: %v %v", entries, err)
	}
}
