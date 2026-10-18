package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubBackendSelectsOneVerifiedPlatformAsset(t *testing.T) {
	for _, mode := range []string{"valid", "ambiguous", "digest", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/v1.2.3") {
					w.WriteHeader(404)
					return
				}
				asset := func(name string) map[string]any {
					return map[string]any{"name": name, "size": 100, "digest": "sha256:" + strings.Repeat("a", 64), "browser_download_url": "https://github.com/example/tool/releases/download/v1.2.3/" + name}
				}
				chosen := asset("tool-x86_64-pc-windows-msvc.zip")
				assets := []any{chosen, asset("tool-aarch64-apple-darwin.tar.gz"), asset("tool-x86_64-unknown-linux-gnu.tar.gz"), asset("tool-x86_64-unknown-linux-musl.tar.gz")}
				if mode == "ambiguous" {
					assets = append(assets, asset("tool-amd64-windows.zip"))
				}
				if mode == "digest" {
					chosen["digest"] = ""
				}
				if mode == "foreign" {
					chosen["browser_download_url"] = "https://evil.example/tool.zip"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.3", "assets": assets})
			}))
			defer server.Close()
			previous := officialHTTPClient
			officialHTTPClient = server.Client()
			transport := officialHTTPClient.Transport
			officialHTTPClient.Transport = metadataTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.github.com" {
					return nil, fmt.Errorf("wrong metadata host")
				}
				copy := r.Clone(r.Context())
				u := *r.URL
				u.Scheme = "http"
				u.Host = strings.TrimPrefix(server.URL, "http://")
				copy.URL = &u
				return transport.RoundTrip(copy)
			})
			defer func() { officialHTTPClient = previous }()
			m, err := GitHubManifest("tool", "example/tool", "tool", "", "")
			if err != nil {
				t.Fatal(err)
			}
			a, err := (manifestPlugin{m}).Artifact("1.2.3", Target{"windows", "amd64"})
			if mode != "valid" {
				if err == nil {
					t.Fatalf("bad %s release accepted: %+v", mode, a)
				}
				return
			}
			if err != nil || a.Format != "zip" || a.Version != "v1.2.3" || a.Size != 100 {
				t.Fatalf("bad asset selection: %+v %v", a, err)
			}
		})
	}
}

func TestGenericPlatformMatchingDoesNotCrossArchitectures(t *testing.T) {
	for _, tc := range []struct {
		name string
		t    Target
		want bool
	}{
		{"tool-x86_64-unknown-linux-gnu.tar.gz", Target{"linux", "amd64"}, true},
		{"tool-x86_64-unknown-linux-musl.tar.gz", Target{"linux", "amd64"}, false},
		{"tool-aarch64-apple-darwin.tar.gz", Target{"darwin", "arm64"}, true},
		{"tool-aarch64-apple-darwin.tar.gz", Target{"darwin", "amd64"}, false},
		{"tool-linux-amd64.sha256", Target{"linux", "amd64"}, false},
	} {
		if got := matchesReleasePlatform(tc.name, tc.t); got != tc.want {
			t.Fatalf("%s %+v matched=%v", tc.name, tc.t, got)
		}
	}
}
