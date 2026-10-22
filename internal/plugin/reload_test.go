package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func externalFixture(t *testing.T) string {
	t.Helper()
	registryMu.RLock()
	previous := externalTools
	registryMu.RUnlock()
	t.Cleanup(func() { registryMu.Lock(); externalTools = previous; registryMu.Unlock() })
	home := t.TempDir()
	t.Setenv("VSM_HOME", home)
	dir := filepath.Join(home, "plugins")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}
func writeManifestFixture(t *testing.T, file, name, command string) {
	t.Helper()
	data, err := json.Marshal(Manifest{Name: name, Commands: []string{command}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReloadRemovesDeletedToolsAndRestoresBuiltin(t *testing.T) {
	dir := externalFixture(t)
	before, _ := Get("go")
	baseID := DefinitionID(before)
	file := filepath.Join(dir, "tools.json")
	data := `[{"name":"audit_reload","commands":["audit"]},{"name":"go","commands":["customgo"]}]`
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ReloadExternalTools(); err != nil {
		t.Fatal(err)
	}
	if _, ok := Get("audit_reload"); !ok {
		t.Fatal("tool not loaded")
	}
	if p, _ := Get("go"); DefinitionID(p) == baseID {
		t.Fatal("override not applied")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := ReloadExternalTools(); err != nil {
		t.Fatal(err)
	}
	if _, ok := Get("audit_reload"); ok {
		t.Fatal("deleted plugin retained")
	}
	if p, _ := Get("go"); DefinitionID(p) != baseID {
		t.Fatal("deleted override did not restore native Go")
	}
}

func TestReloadInvalidDocumentPreservesCompletePreviousSet(t *testing.T) {
	dir := externalFixture(t)
	first := filepath.Join(dir, "a.json")
	second := filepath.Join(dir, "b.json")
	writeManifestFixture(t, first, "audit_a", "first")
	writeManifestFixture(t, second, "audit_b", "second")
	if err := ReloadExternalTools(); err != nil {
		t.Fatal(err)
	}
	writeManifestFixture(t, first, "audit_new", "third")
	if err := os.WriteFile(second, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ReloadExternalTools(); err == nil || !strings.Contains(err.Error(), "b.json") {
		t.Fatalf("invalid JSON hidden: %v", err)
	}
	for _, name := range []string{"audit_a", "audit_b"} {
		if _, ok := Get(name); !ok {
			t.Fatalf("partial replacement lost %s", name)
		}
	}
	if _, ok := Get("audit_new"); ok {
		t.Fatal("partial replacement exposed new tool")
	}
	writeManifestFixture(t, second, "audit_b", "newsecond")
	if err := ReloadExternalTools(); err != nil {
		t.Fatal(err)
	}
	if _, ok := Get("audit_a"); ok {
		t.Fatal("old definition survived successful reload")
	}
	if p, ok := Get("audit_b"); !ok || p.(manifestPlugin).m.Commands[0] != "newsecond" {
		t.Fatal("valid edit not applied")
	}
}

func TestReloadConcurrentReadersSeeWholeNameSets(t *testing.T) {
	dir := externalFixture(t)
	file := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(file, []byte(`[{"name":"audit_pair_a"},{"name":"audit_pair_b"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				seen := map[string]bool{}
				for _, name := range Names() {
					seen[name] = true
				}
				if seen["audit_pair_a"] != seen["audit_pair_b"] {
					t.Error("reader observed a partial name set")
				}
				_ = Catalog("audit_pair")
				_ = DetectCommands()
				_, _ = Get("audit_pair_a")
			}
		}()
	}
	for i := 0; i < 20; i++ {
		if i%2 == 0 {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(file, []byte(`[{"name":"audit_pair_a"},{"name":"audit_pair_b"}]`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := ReloadExternalTools(); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
