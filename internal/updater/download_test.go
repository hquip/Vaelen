package updater

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpdateDownloadSwitchesOfficialRouteOnNoResponse(t *testing.T) {
	data := bytes.Repeat([]byte("release"), 1000)
	client, info := releaseFixture(t, data)
	info.Assets[0].ID = 123
	client.downloadConfig = &assetDownloadConfig{idle: 80 * time.Millisecond, parallelFrom: 1 << 30, connections: 4}
	var direct, api atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/assets/123") {
			api.Add(1)
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Error("API route did not request binary content")
			}
			w.Write(data)
			return
		}
		direct.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dest := filepath.Join(t.TempDir(), "verified")
	if err := client.download(ctx, info, cliAsset(), dest); err != nil {
		t.Fatalf("stalled route was not bypassed: %v (direct %d, API %d)", err, direct.Load(), api.Load())
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("fallback data: %v", err)
	}
	if direct.Load() != 1 || api.Load() != 1 {
		t.Fatalf("unexpected routes: %d / %d", direct.Load(), api.Load())
	}
}

func TestUpdateDownloadParallelRetainsCompletedChunks(t *testing.T) {
	data := bytes.Repeat([]byte("parallel-content-"), 4096)
	client, info := releaseFixture(t, data)
	client.downloadConfig = &assetDownloadConfig{idle: time.Second, parallelFrom: 1024, connections: 4}
	var active, peak atomic.Int32
	var drop atomic.Bool
	var resumed atomic.Bool
	gate := make(chan struct{})
	var open sync.Once
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Range")
		var start, end int
		if _, err := fmt.Sscanf(raw, "bytes=%d-%d", &start, &end); err != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		requests[raw]++
		mu.Unlock()
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		if start != 0 {
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			if n >= 4 {
				open.Do(func() { close(gate) })
			}
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(206)
		if start == 1 && drop.CompareAndSwap(false, true) {
			w.Write(data[start : start+37])
			return
		}
		if start == 38 {
			resumed.Store(true)
		}
		w.Write(data[start : end+1])
	}))
	defer server.Close()
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	var progress []Progress
	client = client.WithProgress(func(p Progress) { progress = append(progress, p) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dest := filepath.Join(t.TempDir(), "parallel")
	if err := client.download(ctx, info, cliAsset(), dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) || peak.Load() != 4 || !resumed.Load() {
		t.Fatalf("parallel data/connection/resume mismatch: %v peak=%d resumed=%v", err, peak.Load(), resumed.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if requests[fmt.Sprintf("bytes=1-%d", (len(data)-1)/4)] != 1 {
		t.Fatalf("initial partial segment was restarted: %v", requests)
	}
	var previous int64
	for _, p := range progress {
		if p.Stage == "downloading" {
			if p.Downloaded < previous {
				t.Fatalf("completed bytes lost: %+v", progress)
			}
			previous = p.Downloaded
		}
	}
}

func TestUpdateDownloadBodyStallResumesThroughOfficialAPI(t *testing.T) {
	data := bytes.Repeat([]byte("body"), 1000)
	client, info := releaseFixture(t, data)
	info.Assets[0].ID = 123
	client.downloadConfig = &assetDownloadConfig{idle: 80 * time.Millisecond, parallelFrom: 1 << 30, connections: 4}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/assets/123") {
			if r.Header.Get("Range") != fmt.Sprintf("bytes=100-%d", len(data)-1) {
				t.Errorf("lost partial body: %s", r.Header.Get("Range"))
				w.WriteHeader(400)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 100-%d/%d", len(data)-1, len(data)))
			w.WriteHeader(206)
			w.Write(data[100:])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data[:100])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.download(ctx, info, cliAsset(), filepath.Join(t.TempDir(), "body-resumed")); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateDownloadRangeFallbackDoesNotMixPartialBodies(t *testing.T) {
	data := bytes.Repeat([]byte("full-body"), 1024)
	client, info := releaseFixture(t, data)
	client.downloadConfig = &assetDownloadConfig{idle: time.Second, parallelFrom: 1024, connections: 4}
	var full atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", len(data)))
			w.WriteHeader(206)
			w.Write(data[:1])
			return
		}
		if r.Header.Get("Range") == "" {
			full.Add(1)
		}
		w.Write(data)
	}))
	defer server.Close()
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	dest := filepath.Join(t.TempDir(), "fallback")
	if err := client.download(context.Background(), info, cliAsset(), dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) || full.Load() != 1 {
		t.Fatalf("range fallback: %v full %d", err, full.Load())
	}
}

func TestUpdateDownloadBadRangeAndHashCannotWriteDestination(t *testing.T) {
	data := bytes.Repeat([]byte("correct"), 1024)
	for _, mode := range []string{"range", "hash", "extra", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			client, info := releaseFixture(t, data)
			client.downloadConfig = &assetDownloadConfig{idle: time.Second, parallelFrom: 1024, connections: 4}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var start, end int
				if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
					w.WriteHeader(400)
					return
				}
				total := len(data)
				if mode == "range" {
					total++
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
				w.WriteHeader(206)
				body := append([]byte(nil), data[start:end+1]...)
				if mode == "hash" {
					body[0] ^= 0xff
				}
				if mode == "extra" {
					body = append(body, 42)
				}
				w.Write(body)
			}))
			defer server.Close()
			client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				client = client.WithProgress(func(p Progress) {
					if p.Downloaded > 0 {
						cancel()
					}
				})
			}
			dest := filepath.Join(t.TempDir(), "untouched")
			if err := client.download(ctx, info, cliAsset(), dest); err == nil {
				t.Fatal("invalid response accepted")
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatalf("invalid data staged: %v", err)
			}
		})
	}
}

func TestUpdateDownloadResumesAfterDroppedBody(t *testing.T) {
	data := bytes.Repeat([]byte("resume"), 5000)
	client, info := releaseFixture(t, data)
	client.downloadConfig = &assetDownloadConfig{idle: time.Second, parallelFrom: 1 << 30, connections: 4}
	var count atomic.Int32
	var resumed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Write(data[:100])
			return
		}
		if r.Header.Get("Range") != "bytes=100-"+strconv.Itoa(len(data)-1) {
			w.WriteHeader(400)
			return
		}
		resumed.Store(true)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 100-%d/%d", len(data)-1, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-100))
		w.WriteHeader(206)
		w.Write(data[100:])
	}))
	defer server.Close()
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	dest := filepath.Join(t.TempDir(), "verified")
	if err := client.download(context.Background(), info, cliAsset(), dest); err != nil {
		t.Fatalf("remaining-byte download failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) || !resumed.Load() {
		t.Fatalf("resume lost bytes: %v", err)
	}
}

func TestUpdateDownloadReusesResolvedCDNAfterStalledRoute(t *testing.T) {
	data := bytes.Repeat([]byte("cdn"), 4096)
	client, info := releaseFixture(t, data)
	info.Assets[0].ID = 123
	client.downloadConfig = &assetDownloadConfig{idle: 80 * time.Millisecond, parallelFrom: 1024, connections: 4}
	var direct, api, cdn atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/download/") {
			direct.Add(1)
			<-r.Context().Done()
			return
		}
		if strings.Contains(r.URL.Path, "/assets/123") {
			api.Add(1)
			http.Redirect(w, r, "/cdn", 302)
			return
		}
		cdn.Add(1)
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(206)
		w.Write(data[start : end+1])
	}))
	defer server.Close()
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.download(ctx, info, cliAsset(), filepath.Join(t.TempDir(), "cdn")); err != nil {
		t.Fatal(err)
	}
	if direct.Load() != 1 || api.Load() != 1 || cdn.Load() != 5 {
		t.Fatalf("range workers repeated the slow redirect: direct=%d API=%d CDN=%d", direct.Load(), api.Load(), cdn.Load())
	}
}
