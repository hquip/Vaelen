package plugin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func javaFixture(t *testing.T, mutate func(map[string]any)) (*javaPlugin, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v3/assets/feature_releases/21/ga" || r.URL.Query().Get("image_type") != "jdk" || r.URL.Query().Get("vendor") != "eclipse" {
			t.Errorf("wrong metadata endpoint: %s", r.URL)
		}
		arch, platform := r.URL.Query().Get("architecture"), r.URL.Query().Get("os")
		ext := ".tar.gz"
		if platform == "windows" {
			ext = ".zip"
		}
		name := "OpenJDK21U-jdk_" + arch + "_" + platform + "_hotspot_21.0.12_1" + ext
		pkg := map[string]any{"name": name, "link": "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12%2B1/" + name, "checksum": strings.Repeat("a", 64), "size": 205073461}
		binary := map[string]any{"architecture": arch, "os": platform, "image_type": "jdk", "jvm_impl": "hotspot", "heap_size": "normal", "package": pkg}
		if mutate != nil {
			mutate(binary)
		}
		_ = json.NewEncoder(w).Encode([]any{map[string]any{"binaries": []any{binary}, "release_type": "ga"}})
	}))
	t.Cleanup(server.Close)
	previous := httpClient
	httpClient = server.Client()
	transport := httpClient.Transport
	httpClient.Transport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.adoptium.net" {
			t.Errorf("integrity metadata used mirror: %s", r.URL)
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = "http"
		u.Host = strings.TrimPrefix(server.URL, "http://")
		copy.URL = &u
		return transport.RoundTrip(copy)
	})
	t.Cleanup(func() { httpClient = previous })
	return &javaPlugin{}, &calls
}

func TestJavaArtifactPinsOfficialURLSizeAndSHA(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_MIRROR", "cn")
	j, calls := javaFixture(t, nil)
	for _, target := range []Target{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		a, err := j.Artifact("21", target)
		if err != nil {
			t.Fatalf("%+v: %v", target, err)
		}
		if a.Size != 205073461 || a.SHA256 != strings.Repeat("a", 64) || !strings.HasPrefix(a.URL, "https://github.com/adoptium/temurin21-binaries/") {
			t.Fatalf("untrusted artifact: %+v", a)
		}
		if !strings.HasPrefix(MirrorURL(a.URL), javaChinaMirror) {
			t.Fatal("Java did not get dedicated mirror")
		}
	}
	if calls.Load() != 6 {
		t.Fatalf("artifact required multiple metadata reads: %d", calls.Load())
	}
	before := calls.Load()
	for _, version := range []string{"../21", "21.invalid", "-1", "0"} {
		if _, err := j.Artifact(version, Target{"windows", "amd64"}); err == nil {
			t.Fatalf("bad version accepted: %s", version)
		}
	}
	if calls.Load() != before {
		t.Fatal("invalid version made network requests")
	}
}

func TestJavaRejectsMissingDigestWrongPlatformAndForeignURL(t *testing.T) {
	for _, mode := range []string{"digest", "size", "platform", "URL", "filename"} {
		t.Run(mode, func(t *testing.T) {
			j, _ := javaFixture(t, func(b map[string]any) {
				p := b["package"].(map[string]any)
				switch mode {
				case "digest":
					p["checksum"] = ""
				case "size":
					p["size"] = 0
				case "platform":
					b["architecture"] = "aarch64"
				case "URL":
					p["link"] = "https://evil.example/installer.zip"
				case "filename":
					p["name"] = "different.zip"
				}
			})
			if _, err := j.Artifact("21", Target{"windows", "amd64"}); err == nil {
				t.Fatalf("bad %s metadata accepted", mode)
			}
		})
	}
}

func TestJavaHomeAndBinPathsHandleMacBundle(t *testing.T) {
	j := &javaPlugin{}
	root := t.TempDir()
	if j.ExecEnv(root)["JAVA_HOME"] != root {
		t.Fatal("plain JAVA_HOME changed")
	}
	home := filepath.Join(root, "Contents", "Home")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if j.ExecEnv(root)["JAVA_HOME"] != home {
		t.Fatal("macOS JAVA_HOME must use Contents/Home")
	}
	found := false
	for _, dir := range j.BinPaths(root) {
		if dir == filepath.Join(home, "bin") {
			found = true
		}
	}
	if !found {
		t.Fatal("macOS JDK bin not on PATH")
	}
}

func TestJavaDoesNotInstallEarlyAccessAsStable(t *testing.T) {
	previous := httpClient
	httpClient = &http.Client{}
	// A well-formed early-access record must be skipped even when the API
	// accepts an unreleased major version.
	httpClient.Transport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"release_type":"ea","binaries":[{"architecture":"x64","os":"windows","image_type":"jdk","jvm_impl":"hotspot","heap_size":"normal"}]}]`)), Header: http.Header{}}, nil
	})
	t.Cleanup(func() { httpClient = previous })
	if _, err := (&javaPlugin{}).Artifact("21", Target{"windows", "amd64"}); err == nil {
		t.Fatal("early access treated as stable")
	}
}

func TestJavaReleaseIdentityKeepsExactBuild(t *testing.T) {
	for _, tc := range []struct{ name, semver, want string }{
		{"jdk-21.0.12.1+1", "21.0.12+101.0.LTS", "21.0.12.1+1"},
		{"jdk8u482-b08", "8.0.482+8", "8.0.482+8"},
		{"jdk-17.0.16+8", "17.0.16+8.LTS", "17.0.16+8"},
	} {
		if got := javaReleaseIdentity(tc.name, tc.semver); got != tc.want {
			t.Fatalf("identity=%s, want %s", got, tc.want)
		}
	}
}
