package updater

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadMirrorFallbackPreservesOfficialChecksum(t *testing.T) {
	for _, mode := range []string{"good", "bad-hash", "html", "unavailable", "all-corrupt"} {
		t.Run(mode, func(t *testing.T) {
			good := bytes.Repeat([]byte("verified-official-release"), 100)
			client, info := releaseFixture(t, good)
			var mirrorCalls, officialCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/mirror/") {
					mirrorCalls.Add(1)
					switch mode {
					case "bad-hash", "all-corrupt":
						w.Write(bytes.Repeat([]byte("x"), len(good)))
					case "html":
						ioText := []byte("<html>not a release</html>")
						w.Write(ioText)
					case "unavailable":
						w.WriteHeader(502)
					default:
						w.Write(good)
					}
					return
				}
				officialCalls.Add(1)
				if mode == "all-corrupt" {
					w.Write(bytes.Repeat([]byte("x"), len(good)))
				} else {
					w.Write(good)
				}
			}))
			defer server.Close()
			client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
			client.networkPolicy = func(context.Context, Asset) downloadNetwork {
				return downloadNetwork{proxy: "direct", mirrors: []string{"https://mirror.example/mirror/release"}}
			}
			var fallback bool
			client = client.WithProgress(func(p Progress) {
				if p.Stage == "official-fallback" {
					fallback = true
				}
			})
			dest := filepath.Join(t.TempDir(), "verified")
			err := client.download(context.Background(), info, cliAsset(), dest)
			if mode == "all-corrupt" {
				if err == nil || !strings.Contains(err.Error(), "SHA256") {
					t.Fatalf("corruption accepted: %v", err)
				}
				if _, err := os.Stat(dest); !os.IsNotExist(err) {
					t.Fatal("unverified mirror content was staged")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(dest)
			if err != nil || !bytes.Equal(got, good) {
				t.Fatal("wrong verified payload")
			}
			if mirrorCalls.Load() != 1 {
				t.Fatalf("mirror retry loop: %d", mirrorCalls.Load())
			}
			if mode == "good" && officialCalls.Load() != 0 {
				t.Fatal("working mirror was ignored")
			}
			if mode != "good" && officialCalls.Load() != 1 {
				t.Fatalf("official fallback missing: %d", officialCalls.Load())
			}
			if mode == "bad-hash" && !fallback {
				t.Fatal("checksum fallback was not visible")
			}
		})
	}
}

func TestAutomaticNetworkPolicyPrefersProxyAndHonorsMirrorOff(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_PROXY", "http://127.0.0.1:7897")
	t.Setenv("VSM_MIRROR", "auto")
	asset := Asset{URL: "https://github.com/hquip/Vaelen/releases/download/v1.1.5/vsm.exe"}
	network := automaticDownloadNetwork(context.Background(), asset)
	if network.proxy != "configured-proxy" || len(network.mirrors) != 0 {
		t.Fatalf("proxy must win: %+v", network)
	}
	t.Setenv("VSM_PROXY", "off")
	network = automaticDownloadNetwork(context.Background(), asset)
	if network.proxy != "direct" || len(network.mirrors) != 2 {
		t.Fatalf("no proxy must prefer domestic sources: %+v", network)
	}
	t.Setenv("VSM_MIRROR", "off")
	if network := automaticDownloadNetwork(context.Background(), asset); len(network.mirrors) != 0 {
		t.Fatalf("mirror off ignored: %+v", network)
	}
}

func TestOfficialMetadataCanBypassRateLimitedProxyWithoutMirroring(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_PROXY", "http://127.0.0.1:7897")
	client, _ := releaseFixture(t, []byte("payload"))
	client.metadataDirect = client.http
	var limitedCalls atomic.Int32
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limitedCalls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer limited.Close()
	client.http = &http.Client{Transport: redirectTransport{limited.Client().Transport, strings.TrimPrefix(limited.URL, "http://")}}
	info, err := client.Check(context.Background(), "v1.0.4", "")
	if err != nil || info.Latest != "v1.0.5" || limitedCalls.Load() != 1 {
		t.Fatalf("official API direct fallback: %+v %v calls=%d", info, err, limitedCalls.Load())
	}
	t.Setenv("VSM_PROXY", "off")
	if _, err := client.Check(context.Background(), "v1.0.4", ""); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("non-proxied 403 was silently retried through another source: %v", err)
	}
}
