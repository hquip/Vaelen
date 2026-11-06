package plugin

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPythonMetadataRecoversFromProxyRateLimit(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_PROXY", "http://127.0.0.1:7897")
	previous := metadataDirectClient
	defer func() { metadataDirectClient = previous }()
	var directCalls atomic.Int32
	metadataDirectClient = &http.Client{Transport: metadataTransport(func(r *http.Request) (*http.Response, error) {
		directCalls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Hostname() != "api.github.com" {
			t.Errorf("metadata left the official API: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":42}`)), Header: make(http.Header)}, nil
	})}
	limited := &http.Client{Transport: metadataTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	var out struct{ Value int }
	if err := fetchJSONOnce(limited, "https://api.github.com/repos/astral-sh/python-build-standalone/releases", &out); err != nil || out.Value != 42 || directCalls.Load() != 1 {
		t.Fatalf("rate-limited Python metadata did not recover: %+v %v calls=%d", out, err, directCalls.Load())
	}
	t.Setenv("VSM_PROXY", "off")
	if err := fetchJSONOnce(limited, "https://api.github.com/repos/astral-sh/python-build-standalone/releases", &out); err == nil || directCalls.Load() != 1 {
		t.Fatal("direct requests must not pretend another route was available")
	}
	t.Setenv("VSM_PROXY", "http://127.0.0.1:7897")
	if err := fetchJSONOnce(limited, "https://example.com/metadata", &out); err == nil || directCalls.Load() != 1 {
		t.Fatal("non-GitHub/mirrored metadata unexpectedly bypassed the proxy")
	}
}

func TestPythonInstallStopsAfterMatchingReleaseWithoutTruncatingCatalog(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	t.Setenv("VSM_MIRROR", "off")
	previous := slowHTTPClient
	defer func() { slowHTTPClient = previous }()
	var pages atomic.Int32
	slowHTTPClient = &http.Client{Transport: metadataTransport(func(r *http.Request) (*http.Response, error) {
		pages.Add(1)
		body := `[{"assets":[{"name":"cpython-3.12.10+20261013-x86_64-pc-windows-msvc-install_only.tar.gz","browser_download_url":"https://github.com/test/python.tar.gz"}]},{},{},{},{}]`
		if r.URL.Query().Get("page") != "1" {
			body = `[{"assets":[{"name":"cpython-3.11.9+20251013-x86_64-pc-windows-msvc-install_only.tar.gz"}]}]`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	p := &pythonPlugin{}
	for i := 0; i < 2; i++ {
		if _, err := p.DownloadURL("3.12.10", Target{"windows", "amd64"}); err != nil {
			t.Fatal(err)
		}
	}
	if pages.Load() != 1 {
		t.Fatalf("install fetched unrelated history: %d pages", pages.Load())
	}
	versions, err := p.ListAll()
	if err != nil || len(versions) != 2 || pages.Load() != 3 {
		t.Fatalf("partial install cache hid older versions: %v %v pages=%d", versions, err, pages.Load())
	}
}

func TestMetadataTooLargeIsNotUnexpectedEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"padding":"`+strings.Repeat("x", maxMetadataBytes)+`"}`)
	}))
	defer server.Close()
	var output map[string]any
	if err := fetchJSONWithClient(server.Client(), server.URL, &output); !errors.Is(err, errMetadataTooLarge) {
		t.Fatalf("wrong oversized response error: %v", err)
	}
}

func TestMetadataRetriesBrokenJSON(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			fmt.Fprint(w, `{"value":`)
			return
		}
		fmt.Fprint(w, `{"value":42}`)
	}))
	defer server.Close()
	var output struct{ Value int }
	if err := fetchJSONWithClient(server.Client(), server.URL, &output); err != nil || output.Value != 42 || attempts.Load() != 2 {
		t.Fatalf("no recovery: %+v %v", output, err)
	}
}

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPythonUsesSmallPagesAndReusesMetadata(t *testing.T) {
	t.Setenv("VSM_MIRROR", "off")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("per_page") != "5" {
			t.Errorf("Python page is too large: %s", r.URL)
		}
		fmt.Fprint(w, `[{"tag_name":"20261013","assets":[{"name":"cpython-3.12.10+20261013-x86_64-pc-windows-msvc-install_only.tar.gz","browser_download_url":"https://github.com/test/python/releases/download/v/file.tar.gz"}]}]`)
	}))
	defer server.Close()
	previous := slowHTTPClient
	defer func() { slowHTTPClient = previous }()
	slowHTTPClient = server.Client()
	transport := slowHTTPClient.Transport
	slowHTTPClient.Transport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = "http"
		u.Host = strings.TrimPrefix(server.URL, "http://")
		copy.URL = &u
		return transport.RoundTrip(copy)
	})
	p := &pythonPlugin{}
	versions, err := p.ListAll()
	if err != nil || len(versions) != 1 || versions[0] != "3.12.10" {
		t.Fatalf("Python list: %v %v", versions, err)
	}
	if _, err := p.DownloadURL("3.12.10", Target{"windows", "amd64"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("version resolution refetched huge metadata: %d", calls.Load())
	}
}
