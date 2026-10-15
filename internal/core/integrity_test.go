package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"vsm/internal/plugin"
)

type integrityPlugin struct {
	url, sum    string
	checksumErr error
}

func (p integrityPlugin) Name() string                                      { return "go" }
func (p integrityPlugin) ListAll() ([]string, error)                        { return []string{"1.0"}, nil }
func (p integrityPlugin) LatestStable() (string, error)                     { return "1.0", nil }
func (p integrityPlugin) DownloadURL(string, plugin.Target) (string, error) { return p.url, nil }
func (p integrityPlugin) BinPaths(dir string) []string                      { return []string{filepath.Join(dir, "bin")} }
func (p integrityPlugin) ExecEnv(string) map[string]string                  { return nil }
func (p integrityPlugin) Checksum(string, plugin.Target) (string, error)    { return p.sum, p.checksumErr }

func TestInstallIntegrityIsFailClosed(t *testing.T) {
	name := "go/bin/go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(testTar(t, name))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	sum := hex.EncodeToString(h[:])
	for _, mode := range []string{"metadata-error", "missing-required", "invalid-metadata", "mismatch", "valid"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write(data) }))
			defer srv.Close()
			p := integrityPlugin{url: srv.URL + "/tool.tar.gz", sum: sum}
			switch mode {
			case "metadata-error":
				p.checksumErr = errors.New("metadata unavailable")
			case "missing-required":
				p.sum = ""
			case "invalid-metadata":
				p.sum = "wrong"
			case "mismatch":
				p.sum = hex.EncodeToString(make([]byte, 32))
			}
			paths := Paths{Root: t.TempDir()}
			err := (Installer{Paths: paths, RequireChecksum: true}).Install(p, "1.0", "", nil)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode != "valid" && dirExists(paths.InstallDir("go", "1.0")) {
				t.Fatal("failed integrity check published an install")
			}
			if mode == "metadata-error" || mode == "missing-required" || mode == "invalid-metadata" {
				if requests.Load() != 0 {
					t.Fatal("download started before verifying integrity policy")
				}
			}
			if mode == "mismatch" {
				entries, _ := os.ReadDir(paths.Downloads())
				if len(entries) != 0 {
					t.Fatal("checksum mismatch left a cached payload")
				}
			}
		})
	}
}
