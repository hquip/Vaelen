package core

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"vsm/internal/plugin"
)

type testInstallLayout struct {
	plugin.Plugin
	prepare func(string) (string, error)
}

func (p testInstallLayout) PrepareInstall(root, _ string, _ plugin.Target) (string, error) {
	return p.prepare(root)
}

func TestPreparedInstallRemainsInsideExtraction(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_MIRROR", "off")
	name := "go/bin/go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(testTarMode(t, 0755, name))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	t.Cleanup(server.Close)
	artifact := plugin.Artifact{URL: server.URL + "/tool.tar.gz", SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data)), Format: "tar.gz"}
	base, _ := plugin.Get("go")
	for _, mode := range []string{"valid", "outside", "ancestor-link", "missing"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "ancestor-link" && runtime.GOOS == "windows" {
				t.Skip("Unix verifies extracted symlink ancestor containment")
			}
			outside := t.TempDir()
			if err := os.Mkdir(filepath.Join(outside, "nested"), 0755); err != nil {
				t.Fatal(err)
			}
			p := testInstallLayout{Plugin: base, prepare: func(root string) (string, error) {
				switch mode {
				case "valid":
					return filepath.Join(root, "go"), nil
				case "outside":
					return outside, nil
				case "missing":
					return filepath.Join(root, "absent"), nil
				default:
					if err := os.Symlink(outside, filepath.Join(root, "pivot")); err != nil {
						return "", err
					}
					return filepath.Join(root, "pivot", "nested"), nil
				}
			}}
			paths := Paths{Root: t.TempDir()}
			err := (Installer{Paths: paths, Pinned: &artifact, RequireChecksum: true}).Install(p, "1.2.3", "", nil)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || pathExists(paths.InstallDir("go", "1.2.3")) {
				t.Fatalf("invalid prepared layout installed: %s %v", mode, err)
			}
		})
	}
}
