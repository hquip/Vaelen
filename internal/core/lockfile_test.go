package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

const lockReviewMissingTool = "lockreview_missing"

// This source represents a custom plugin available when the old lock was made,
// but absent from the registry in the current process.
type lockReviewArtifactPlugin struct{ name string }

func (p lockReviewArtifactPlugin) Name() string { return p.name }
func (lockReviewArtifactPlugin) ListAll() ([]string, error) {
	return nil, fmt.Errorf("unexpected version metadata lookup")
}
func (lockReviewArtifactPlugin) LatestStable() (string, error) {
	return "", fmt.Errorf("unexpected version metadata lookup")
}
func (lockReviewArtifactPlugin) DownloadURL(string, plugin.Target) (string, error) {
	return "", fmt.Errorf("artifact must be resolved atomically")
}
func (lockReviewArtifactPlugin) BinPaths(dir string) []string     { return []string{dir} }
func (lockReviewArtifactPlugin) ExecEnv(string) map[string]string { return nil }
func (p lockReviewArtifactPlugin) Artifact(version string, _ plugin.Target) (plugin.Artifact, error) {
	return plugin.Artifact{
		URL:    "http://127.0.0.1:1/" + p.name + "-" + version + ".zip",
		SHA256: strings.Repeat("a", 64),
		Format: "zip",
	}, nil
}

func lockReviewRegisteredTool(t *testing.T, name string) plugin.Plugin {
	t.Helper()
	// Manifest plugins implement ArtifactProvider, and exact versions plus fixed
	// checksums avoid all metadata and download requests during lock generation.
	m := plugin.Manifest{
		Name:      name,
		Source:    plugin.ManifestSource{Type: "github-releases", Repo: "unused/metadata"},
		URL:       "http://127.0.0.1:1/" + name + "-{version}.zip",
		Assets:    map[string]string{"linux/amd64": "unused"},
		Checksums: map[string]string{"1.0.0/linux/amd64": strings.Repeat("a", 64)},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(os.Getenv("VSM_HOME"), "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	plugin.ReloadExternalTools()
	p, ok := plugin.Get(name)
	if !ok {
		t.Fatalf("fixture source %s was not registered", name)
	}
	if _, ok := p.(plugin.ArtifactProvider); !ok {
		t.Fatal("fixture source does not provide pinned artifacts")
	}
	return p
}

func lockReviewEntry(t *testing.T, p plugin.Plugin) LockedTool {
	t.Helper()
	a, err := ResolveArtifact(p, "1.0.0", plugin.Target{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	return LockedTool{
		Definition: plugin.DefinitionID(p),
		Requested:  "1.0.0",
		Version:    "1.0.0",
		Platforms:  map[string]plugin.Artifact{"linux/amd64": a},
	}
}

func lockReviewWrite(t *testing.T, root string, lock ToolchainLock) []byte {
	t.Helper()
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, LockName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCreateLockDropsRemovedCustomTool(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	keep := lockReviewRegisteredTool(t, "lockreview_keep")
	registeredRemoved := lockReviewRegisteredTool(t, "lockreview_removed")
	missing := lockReviewArtifactPlugin{name: lockReviewMissingTool}
	if _, exists := plugin.Get(missing.Name()); exists {
		t.Fatal("removed source fixture must not be registered")
	}
	for _, removed := range []plugin.Plugin{registeredRemoved, missing} {
		for _, update := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/update=%t", removed.Name(), update), func(t *testing.T) {
				root := t.TempDir()
				workspace := config.Workspace{Schema: 1, Tools: map[string]string{
					keep.Name(): "1.0.0", removed.Name(): "1.0.0",
				}}
				workspacePath := filepath.Join(root, config.WorkspaceName)
				if err := workspace.Save(workspacePath); err != nil {
					t.Fatal(err)
				}
				keepEntry := lockReviewEntry(t, keep)
				freshArtifact := keepEntry.Platforms["linux/amd64"]
				oldArtifact := keepEntry.Platforms["linux/amd64"]
				oldArtifact.URL = "http://127.0.0.1:1/previous-pinned-build.zip"
				keepEntry.Platforms["linux/amd64"] = oldArtifact
				lockReviewWrite(t, root, ToolchainLock{Schema: 1, Tools: map[string]LockedTool{
					keep.Name(): keepEntry, removed.Name(): lockReviewEntry(t, removed),
				}})
				delete(workspace.Tools, removed.Name())
				if err := workspace.Save(workspacePath); err != nil {
					t.Fatal(err)
				}
				lock, err := CreateLock(root, []plugin.Target{{OS: "linux", Arch: "amd64"}}, update, nil)
				if err != nil {
					t.Fatalf("removed declaration/source prevented regeneration: %v", err)
				}
				if len(lock.Tools) != 1 {
					t.Fatalf("regenerated lock retained removed tool: %#v", lock.Tools)
				}
				entry, ok := lock.Tools[keep.Name()]
				if !ok {
					t.Fatal("regenerated lock lost the remaining tool")
				}
				wantArtifact := oldArtifact
				if update {
					wantArtifact = freshArtifact
				}
				if got := entry.Platforms["linux/amd64"]; got != wantArtifact {
					t.Fatalf("update=%t resolved artifact %#v; want %#v", update, got, wantArtifact)
				}
				persisted, err := LoadLock(root)
				if err != nil || persisted == nil || len(persisted.Tools) != 1 {
					t.Fatalf("regenerated lock was not persisted correctly: %#v, %v", persisted, err)
				}
				if _, exists := persisted.Tools[removed.Name()]; exists {
					t.Fatal("persisted lock retained removed tool")
				}
			})
		}
	}
}

func TestLoadLockRejectsRemovedToolSource(t *testing.T) {
	root := t.TempDir()
	p := lockReviewArtifactPlugin{name: lockReviewMissingTool}
	if _, exists := plugin.Get(p.Name()); exists {
		t.Fatal("removed source fixture must not be registered")
	}
	before := lockReviewWrite(t, root, ToolchainLock{Schema: 1, Tools: map[string]LockedTool{
		p.Name(): lockReviewEntry(t, p),
	}})
	if _, err := LoadLock(root); err == nil || !strings.Contains(err.Error(), p.Name()+" is not registered") {
		t.Fatalf("execution accepted a missing tool source: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, LockName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("loading a missing source changed the lock: %v", err)
	}
}

func TestCreateLockRejectsMalformedPreviousLockWithoutOverwrite(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	keep := lockReviewRegisteredTool(t, "lockreview_keep")
	removed := lockReviewArtifactPlugin{name: lockReviewMissingTool}
	valid := ToolchainLock{Schema: 1, Tools: map[string]LockedTool{
		removed.Name(): lockReviewEntry(t, removed),
	}}
	validJSON, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	platformsJSON, err := json.Marshal(valid.Tools[removed.Name()].Platforms)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, old, replacement string }{
		{"invalid-json", string(validJSON), "{"},
		{"unknown-field", `"schema_version":1`, `"schema_version":1,"unexpected":true`},
		{"duplicate-key", `"schema_version":1`, `"schema_version":1,"schema_version":1`},
		{"unsupported-schema", `"schema_version":1`, `"schema_version":2`},
		{"null-tools", string(validJSON), `{"schema_version":1,"tools":null}`},
		{"invalid-tool", removed.Name(), "../removed"},
		{"invalid-version", `"version":"1.0.0"`, `"version":"../1.0.0"`},
		{"missing-request", `"requested":"1.0.0"`, `"requested":""`},
		{"unsupported-platform", `"linux/amd64"`, `"plan9/amd64"`},
		{"missing-platforms", string(platformsJSON), `{}`},
		{"invalid-url", "http://127.0.0.1:1/", "file:///"},
		{"invalid-checksum", strings.Repeat("a", 64), "invalid"},
		{"invalid-size", `"format":"zip"`, `"format":"zip","size":-1`},
		{"unsupported-format", `"format":"zip"`, `"format":"executable-script"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := []byte(strings.Replace(string(validJSON), test.old, test.replacement, 1))
			if bytes.Equal(data, validJSON) {
				t.Fatal("fixture did not corrupt the previous lock")
			}
			for _, update := range []bool{false, true} {
				t.Run(fmt.Sprintf("update=%t", update), func(t *testing.T) {
					root := t.TempDir()
					workspace := config.Workspace{Schema: 1, Tools: map[string]string{keep.Name(): "1.0.0"}}
					if err := workspace.Save(filepath.Join(root, config.WorkspaceName)); err != nil {
						t.Fatal(err)
					}
					lockPath := filepath.Join(root, LockName)
					if err := os.WriteFile(lockPath, data, 0o600); err != nil {
						t.Fatal(err)
					}
					if _, err := LoadLock(root); err == nil || strings.Contains(err.Error(), "is not registered") {
						t.Fatalf("malformed lock was not structurally rejected: %v", err)
					}
					if _, err := CreateLock(root, []plugin.Target{{OS: "linux", Arch: "amd64"}}, update, nil); err == nil {
						t.Fatal("regeneration accepted a malformed previous lock")
					}
					after, err := os.ReadFile(lockPath)
					if err != nil || !bytes.Equal(data, after) {
						t.Fatalf("failed regeneration overwrote the old lock: %v", err)
					}
				})
			}
		})
	}
}
