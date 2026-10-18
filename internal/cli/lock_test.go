package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

func TestLockInstallAndRunUseTheSameArtifactWithoutMetadata(t *testing.T) {
	bin := buildVsm(t)
	home, root := t.TempDir(), t.TempDir()
	fixture := filepath.Join(t.TempDir(), "probe.go")
	if err := os.WriteFile(fixture, []byte("package main;import \"fmt\";func main(){fmt.Println(\"LOCKED-TOOL\")}"), 0o600); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(t.TempDir(), "probe")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", program, fixture).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s %v", out, err)
	}
	binary, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	name := "fixture/bin/fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	header := &zip.FileHeader{Name: name}
	header.SetMode(0o755)
	entry, err := z.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write(binary)
	_ = z.Close()
	data := archive.Bytes()
	sum := sha256.Sum256(data)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/fixture-1.0.0.zip" {
			t.Errorf("unexpected metadata request: %s", r.URL)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method != "HEAD" {
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()
	platform := runtime.GOOS + "/" + runtime.GOARCH
	m := plugin.Manifest{Name: "fixture", Source: plugin.ManifestSource{Type: "github-releases", Repo: "unused/metadata"}, URL: server.URL + "/fixture-{version}.zip", Assets: map[string]string{platform: "unused"}, Bin: "bin", Checksums: map[string]string{"1.0.0/" + platform: hex.EncodeToString(sum[:])}}
	if err := os.MkdirAll(filepath.Join(home, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(home, "plugins", "fixture.json"), definition, 0o600); err != nil {
		t.Fatal(err)
	}
	c := config.Workspace{Schema: 1, Tools: map[string]string{"fixture": "1.0.0"}}
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string) { return runVsm(t, bin, home, root, args...) }
	if code, out := run("lock", "--json"); code != 0 {
		t.Fatal(out)
	}
	if requests.Load() != 0 {
		t.Fatal("locking exact fixed-checksum package performed network lookups")
	}
	if code, out := run("install", "--locked"); code != 0 {
		t.Fatal(out)
	}
	before := requests.Load()
	if code, out := run("run", "--locked", "--", "fixture"); code != 0 || !strings.Contains(out, "LOCKED-TOOL") {
		t.Fatalf("locked run: %d %s", code, out)
	}
	if requests.Load() != before {
		t.Fatal("locked execution did network metadata lookup")
	}
	if code, out := run("install", "--locked"); code != 0 {
		t.Fatal(out)
	}
	if requests.Load() != before {
		t.Fatal("verified installation was downloaded again")
	}
	c.Tools["fixture"] = "2.0.0"
	if err := c.Save(filepath.Join(root, "vsm.json")); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("lock", "--check"); code == 0 {
		t.Fatal("stale requirements were accepted")
	}
	if code, _ := run("install", "--locked"); code == 0 {
		t.Fatal("stale lock was installed")
	}
	c.Tools["fixture"] = "1.0.0"
	_ = c.Save(filepath.Join(root, "vsm.json"))
	m.Bin = "different"
	definition, _ = json.Marshal(m)
	_ = os.WriteFile(filepath.Join(home, "plugins", "fixture.json"), definition, 0o600)
	if code, _ := run("run", "--locked", "--", "fixture"); code == 0 {
		t.Fatal("changed plugin definition was accepted")
	}
}
