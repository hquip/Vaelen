package plugin

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

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
