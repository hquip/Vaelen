package core

import (
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/plugin"
)

func TestSHA512SDKInstallAndLockReceipt(t *testing.T) {
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
	hash := sha512.Sum512(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	good := plugin.Artifact{URL: server.URL + "/sdk.tar.gz", SHA512: hex.EncodeToString(hash[:]), Size: int64(len(data)), Format: "tar.gz"}
	for _, mode := range []string{"valid", "mismatch", "bad-metadata"} {
		t.Run(mode, func(t *testing.T) {
			paths := Paths{Root: t.TempDir()}
			artifact := good
			if mode == "mismatch" {
				artifact.SHA512 = strings.Repeat("0", 128)
			}
			if mode == "bad-metadata" {
				artifact.SHA512 = "bad"
			}
			p, _ := plugin.Get("go")
			err := (Installer{Paths: paths, Pinned: &artifact, RequireChecksum: true}).Install(p, "1.2.3", "", nil)
			if mode != "valid" {
				if err == nil || pathExists(paths.InstallDir("go", "1.2.3")) {
					t.Fatalf("invalid checksum installed SDK: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckInstalledArtifact(paths.InstallDir("go", "1.2.3"), good); err != nil {
				t.Fatal(err)
			}
			changed := good
			changed.SHA512 = strings.Repeat("f", 128)
			if err := CheckInstalledArtifact(paths.InstallDir("go", "1.2.3"), changed); err == nil {
				t.Fatal("lock ignored SHA512 difference")
			}
			if !fileExists(filepath.Join(paths.InstallDir("go", "1.2.3"), artifactReceiptName)) {
				t.Fatal("SHA512 install has no provenance receipt")
			}
		})
	}
}

func TestGlobalJSONSelectsDotnetSDKAndRejectsMalformedJSON(t *testing.T) {
	paths := Paths{Root: t.TempDir()}
	dir := t.TempDir()
	if err := os.MkdirAll(paths.InstallDir("dotnet", "8.0.408"), 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "global.json")
	if err := os.WriteFile(file, []byte(`{"sdk":{"version":"8.0.408"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	r := Resolver{Paths: paths, Registry: Registry{Paths: paths}}.Resolve("dotnet", dir)
	if r.Version != "8.0.408" || r.Source != file {
		t.Fatalf("wrong SDK selection: %+v", r)
	}
	if err := os.WriteFile(file, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	r = Resolver{Paths: paths, Registry: Registry{Paths: paths}}.Resolve("dotnet", dir)
	if r.Error == "" {
		t.Fatal("broken global.json silently ignored")
	}
}
