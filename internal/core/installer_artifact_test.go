package core

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"vsm/internal/plugin"
)

type pinnedTestPlugin struct {
	artifact plugin.Artifact
	calls    int
}

func (p *pinnedTestPlugin) Name() string                  { return "java" }
func (p *pinnedTestPlugin) ListAll() ([]string, error)    { return []string{"21"}, nil }
func (p *pinnedTestPlugin) LatestStable() (string, error) { return "21", nil }
func (p *pinnedTestPlugin) DownloadURL(string, plugin.Target) (string, error) {
	return "", fmt.Errorf("mutable URL must not be fetched separately")
}
func (p *pinnedTestPlugin) Checksum(string, plugin.Target) (string, error) {
	return "", fmt.Errorf("digest must use same metadata snapshot")
}
func (p *pinnedTestPlugin) Artifact(string, plugin.Target) (plugin.Artifact, error) {
	p.calls++
	return p.artifact, nil
}
func (p *pinnedTestPlugin) BinPaths(dir string) []string     { return []string{filepath.Join(dir, "bin")} }
func (p *pinnedTestPlugin) ExecEnv(string) map[string]string { return nil }

func TestInstallerPinsArtifactInOneMetadataRequest(t *testing.T) {
	t.Setenv("VSM_MIRROR", "off")
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	name := "jdk/bin/java"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	h := &zip.FileHeader{Name: name}
	h.SetMode(0o755)
	w, err := z.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("test-java-binary"))
	_ = z.Close()
	data := archive.Bytes()
	sum := sha256.Sum256(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method != "HEAD" {
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()
	p := &pinnedTestPlugin{artifact: plugin.Artifact{URL: server.URL + "/java.zip", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}}
	paths := Paths{Root: t.TempDir()}
	if err := (Installer{Paths: paths, RequireChecksum: true}).Install(p, "21", "", nil); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 {
		t.Fatalf("URL/digest resolution not atomic: %d", p.calls)
	}
	if _, err := os.Stat(filepath.Join(paths.InstallDir("java", "21"), "bin", filepath.Base(name))); err != nil {
		t.Fatal(err)
	}
}
