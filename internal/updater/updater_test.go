package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type redirectTransport struct {
	base http.RoundTripper
	host string
}

func (t redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = "http"
	u.Host = t.host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}
func releaseFixture(t *testing.T, data []byte) (Client, Info) {
	return releaseFixtureAsset(t, data, cliAsset())
}
func releaseFixtureAsset(t *testing.T, data []byte, name string) (Client, Info) {
	t.Helper()
	sum := sha256.Sum256(data)
	tag := "v1.0.5"
	asset := Asset{Name: name, URL: repository + "/releases/download/" + tag + "/" + name, Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:])}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/download/") {
			w.Write(data)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []Asset{asset}})
	}))
	t.Cleanup(server.Close)
	httpClient := server.Client()
	httpClient.Transport = redirectTransport{httpClient.Transport, strings.TrimPrefix(server.URL, "http://")}
	client := Client{httpClient, "https://api.github.com/repos/hquip/Vaelen/releases"}
	info, err := client.Check(context.Background(), "v1.0.4", "")
	if err != nil {
		t.Fatal(err)
	}
	return client, info
}

func TestMacOSWholeBundleUpdate(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native macOS codesign/bundle replacement")
	}
	work := t.TempDir()
	newApp := filepath.Join(work, "new", "VSM.app")
	macos := filepath.Join(newApp, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-ldflags", "-X vsm/internal/buildinfo.Version=1.0.5", "-o", filepath.Join(macos, "vsm"), "vsm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	cli, err := os.ReadFile(filepath.Join(macos, "vsm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macos, "vsm-gui"), cli, 0755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>vsm-gui</string><key>CFBundleIdentifier</key><string>io.github.hquip.vsm</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(newApp, "Contents", "Info.plist"), []byte(plist), 0644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Join(macos, "vsm"), newApp} {
		if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", target).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v %s", err, out)
		}
	}
	archive := filepath.Join(work, "update.zip")
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", newApp, archive).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v %s", err, out)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	client, info := releaseFixtureAsset(t, data, "vsm-gui-macos-universal.zip")
	currentApp := filepath.Join(work, "installed", "VSM.app")
	currentExe := filepath.Join(currentApp, "Contents", "MacOS", "vsm-gui")
	os.MkdirAll(filepath.Dir(currentExe), 0755)
	os.WriteFile(currentExe, []byte("old"), 0755)
	result, err := client.Apply(context.Background(), info, currentExe, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != currentApp || len(result.Backups) != 1 {
		t.Fatalf("not a whole-bundle update: %+v", result)
	}
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", currentApp).CombinedOutput(); err != nil {
		t.Fatalf("updated signature: %v %s", err, out)
	}
	if old, err := os.ReadFile(filepath.Join(result.Backups[0], "Contents", "MacOS", "vsm-gui")); err != nil || string(old) != "old" {
		t.Fatal("old running app backup lost")
	}
}

func TestUpdateVerifiesBeforeReplacingAndRollsBack(t *testing.T) {
	// Build a real release CLI so validation checks actual platform/Go metadata.
	bin := filepath.Join(t.TempDir(), "new-vsm")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-ldflags", "-X vsm/internal/buildinfo.Version=1.0.5", "-o", bin, "vsm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	client, info := releaseFixture(t, data)
	for _, mode := range []string{"corrupt", "success"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "vsm")
			if runtime.GOOS == "windows" {
				target += ".exe"
			}
			if err := os.WriteFile(target, []byte("old-program"), 0755); err != nil {
				t.Fatal(err)
			}
			candidate := info
			candidate.Assets = append([]Asset(nil), info.Assets...)
			if mode == "corrupt" {
				candidate.Assets[0].Digest = "sha256:" + strings.Repeat("0", 64)
			}
			result, err := client.Apply(context.Background(), candidate, target, false)
			if mode == "corrupt" {
				if err == nil {
					t.Fatal("checksum mismatch updated program")
				}
				got, _ := os.ReadFile(target)
				if string(got) != "old-program" {
					t.Fatal("checksum failure touched old program")
				}
				return
			}
			if err != nil || len(result.Updated) != 1 {
				t.Fatalf("update: %+v %v", result, err)
			}
			out, err := exec.Command(target, "version").Output()
			if err != nil || strings.TrimSpace(string(out)) != "vsm v1.0.5" {
				t.Fatalf("updated executable: %v %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(dir, ".vsm-update.lock")); !os.IsNotExist(err) {
				t.Fatal("update lock retained")
			}
		})
	}
	t.Run("rollback-second-file", func(t *testing.T) {
		dir := t.TempDir()
		stage := filepath.Join(dir, ".vsm-update-owned")
		os.Mkdir(stage, 0700)
		first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
		os.WriteFile(first, []byte("first-old"), 0755)
		os.WriteFile(second, []byte("second-old"), 0755)
		newFile := filepath.Join(stage, "new")
		os.WriteFile(newFile, []byte("new"), 0755)
		_, err := replaceFiles("v1.0.5", stage, []replacement{{Target: first, Staged: newFile}, {Target: second, Staged: filepath.Join(stage, "missing")}})
		if err == nil {
			t.Fatal("missing staged file succeeded")
		}
		for file, want := range map[string]string{first: "first-old", second: "second-old"} {
			got, _ := os.ReadFile(file)
			if string(got) != want {
				t.Fatalf("rollback lost %s: %s", file, got)
			}
		}
	})
}

func TestUpdateSourceAndVersionBoundaries(t *testing.T) {
	_, info := releaseFixture(t, []byte("payload"))
	if !info.Available || info.Latest != "v1.0.5" {
		t.Fatal(info)
	}
	for _, raw := range []string{"https://evil.example/file", repository + "/releases/download/v1.0.4/" + cliAsset()} {
		bad := info
		bad.Assets = append([]Asset(nil), info.Assets...)
		bad.Assets[0].URL = raw
		if _, err := bad.asset(cliAsset()); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	client, _ := releaseFixture(t, []byte("payload"))
	for _, version := range []string{"../v1", "v1.0.5-rc.1", "latest", ""} {
		if version == "" {
			continue
		}
		if _, err := client.Check(context.Background(), "1.0.4", version); err == nil {
			t.Fatalf("invalid requested version %s", version)
		}
	}
	info.Current = "v1.0.6"
	if _, err := client.Apply(context.Background(), info, "unused", false); err == nil {
		t.Fatal("allowed downgrade")
	}
	if got := macBundle(filepath.Join("/Applications", "VSM.app", "Contents", "MacOS", "vsm")); got != filepath.Join("/Applications", "VSM.app") {
		t.Fatal(got)
	}
}

func TestChecksumManifestFallback(t *testing.T) {
	data := []byte("verified payload")
	digest := sha256.Sum256(data)
	manifest := []byte(hex.EncodeToString(digest[:]) + "  " + cliAsset() + "\n")
	manifestDigest := sha256.Sum256(manifest)
	client, info := releaseFixture(t, data)
	info.Assets[0].Digest = ""
	info.Assets = append(info.Assets, Asset{Name: "SHA256SUMS", URL: repository + "/releases/download/v1.0.5/SHA256SUMS", Size: int64(len(manifest)), Digest: "sha256:" + hex.EncodeToString(manifestDigest[:])})
	base := client.http.Transport
	client.http.Transport = manifestTransport{base, manifest}
	dest := filepath.Join(t.TempDir(), "download")
	if err := client.download(context.Background(), info, cliAsset(), dest); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(data) {
		t.Fatal("manifest-selected artifact changed")
	}
	info.Assets[1].Digest = "sha256:" + strings.Repeat("0", 64)
	if err := client.download(context.Background(), info, cliAsset(), dest); err == nil {
		t.Fatal("invalid manifest checksum accepted")
	}
}

type manifestTransport struct {
	base http.RoundTripper
	data []byte
}

func (t manifestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(t.data)), ContentLength: int64(len(t.data)), Header: make(http.Header)}, nil
	}
	return t.base.RoundTrip(r)
}

func TestRunningExecutableCanBeReplaced(t *testing.T) {
	if os.Getenv("VSM_TEST_REPLACE_RUNNING") == "1" {
		target, _ := os.Executable()
		stage := os.Getenv("VSM_TEST_REPLACE_STAGE")
		data, err := os.ReadFile(target)
		if err != nil {
			os.Exit(2)
		}
		file := filepath.Join(stage, "new")
		if err := os.WriteFile(file, data, 0755); err != nil {
			os.Exit(3)
		}
		_, err = replaceFiles("v1.0.5", stage, []replacement{{Target: target, Staged: file}})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		os.Exit(0)
	}
	exe, _ := os.Executable()
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copy := filepath.Join(dir, "running-test")
	if runtime.GOOS == "windows" {
		copy += ".exe"
	}
	if err := os.WriteFile(copy, data, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(dir, ".vsm-update-running")
	os.Mkdir(stage, 0700)
	cmd := exec.Command(copy, "-test.run=^TestRunningExecutableCanBeReplaced$")
	cmd.Env = append(os.Environ(), "VSM_TEST_REPLACE_RUNNING=1", "VSM_TEST_REPLACE_STAGE="+stage)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("self replacement: %v %s", err, out)
	}
	if _, err := os.Stat(copy); err != nil {
		t.Fatal("running executable was not replaced")
	}
}
