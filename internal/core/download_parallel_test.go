package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"vsm/internal/netcfg"
)

func TestProbeUsesOneByteRangeWhenHEADUnsupported(t *testing.T) {
	var head, get atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			head.Add(1)
			w.WriteHeader(405)
			return
		}
		get.Add(1)
		if r.Header.Get("Range") != "bytes=0-0" {
			t.Errorf("probe fetched whole package: %s", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 0-0/205073461")
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(206)
		_, _ = w.Write([]byte("P"))
	}))
	defer server.Close()
	_, size, ranges, _ := probe(server.URL)
	if size != 205073461 || !ranges || head.Load() != 1 || get.Load() != 1 {
		t.Fatalf("probe failed: %d %v %d %d", size, ranges, head.Load(), get.Load())
	}
}

func TestParallelUsesChecksumWhenETagIsAbsent(t *testing.T) {
	data := bytes.Repeat([]byte("abcd"), 3<<20)
	copy(data, []byte("PK\x03\x04"))
	digest := sha256.Sum256(data)
	var ranges, streams atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			return
		}
		if r.Header.Get("Range") == "" {
			streams.Add(1)
			_, _ = w.Write(data)
			return
		}
		ranges.Add(1)
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(206)
		_, _ = w.Write(data[start : end+1])
	}))
	defer server.Close()
	dir := t.TempDir()
	path, err := downloadOnce(server.URL+"/jdk.zip", dir, "jdk", func(string) {}, downloadIntegrity{SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) || ranges.Load() != 4 || streams.Load() != 0 {
		t.Fatalf("no verified 4-way download: ranges=%d streams=%d err=%v", ranges.Load(), streams.Load(), err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err = downloadOnce(server.URL+"/jdk.zip", dir, "bad", func(string) {}, downloadIntegrity{SHA256: strings.Repeat("0", 64), Size: int64(len(data))})
	if err == nil {
		t.Fatal("bad digest accepted")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("checksum failure retained unverified archive")
	}

	// No strong validator and no publisher digest: keep the original safe stream fallback.
	path, err = downloadOnce(server.URL+"/jdk.zip", dir, "unverified", func(string) {}, downloadIntegrity{})
	if err != nil {
		t.Fatal(err)
	}
	if streams.Load() != 1 || ranges.Load() != 8 {
		t.Fatal("unverified entity used parallel assembly")
	}
	_ = os.Remove(path)
}

func TestInterruptedChunkResumesOnlyRemainingBytes(t *testing.T) {
	data := []byte("abcdefghijklmnop")
	var mu sync.Mutex
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.Header.Get("Range"))
		n := len(requested)
		mu.Unlock()
		if r.Header.Get("If-Match") != "\"stable\"" {
			t.Error("resume lost entity guard")
		}
		var start, end int
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(206)
		if n == 1 {
			_, _ = w.Write(data[:4])
			return
		}
		_, _ = w.Write(data[start : end+1])
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "partial")
	if err := os.WriteFile(dest, make([]byte, len(data)), 0o600); err != nil {
		t.Fatal(err)
	}
	cw := &countingWriter{total: int64(len(data)), progress: func(string) {}}
	if err := downloadChunk(server.URL, dest, 0, int64(len(data)-1), int64(len(data)), cw, "\"stable\""); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) || cw.n != int64(len(data)) {
		t.Fatalf("resume corrupted bytes/count: %s %d", got, cw.n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requested) != 2 || requested[0] != "bytes=0-15" || requested[1] != "bytes=4-15" {
		t.Fatalf("whole chunk redownloaded: %v", requested)
	}
}

func TestFailedChunkCancelsOtherConnections(t *testing.T) {
	started := make(chan struct{}, 4)
	var active atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		started <- struct{}{}
		if r.Header.Get("Range") == "bytes=0-3" {
			for i := 0; i < 4; i++ {
				select {
				case <-started:
				case <-r.Context().Done():
					return
				}
			}
			w.WriteHeader(412)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "cancel")
	start := time.Now()
	if err := downloadParallel(server.URL, dest, 16, 4, func(string) {}, "\"stable\""); err == nil {
		t.Fatal("changed entity accepted")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("other connections did not stop")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failed parallel download kept partial file")
	}
}

func TestRangeResumeRejectsChangedETagAndEncoding(t *testing.T) {
	for _, mode := range []string{"changed-etag", "gzip"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Range", "bytes 0-3/4")
				if mode == "changed-etag" {
					w.Header().Set("ETag", "\"changed\"")
				} else {
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.WriteHeader(206)
				_, _ = w.Write([]byte("data"))
			}))
			defer server.Close()
			dest := filepath.Join(t.TempDir(), "part")
			_ = os.WriteFile(dest, []byte("____"), 0o600)
			client := netcfg.Client(time.Second)
			_, err := readDownloadRange(context.Background(), client, server.URL, dest, 0, 3, 4, &countingWriter{progress: func(string) {}}, "\"stable\"")
			if err == nil {
				t.Fatal("mismatched range representation accepted")
			}
			got, _ := os.ReadFile(dest)
			if string(got) != "____" {
				t.Fatal("invalid response wrote disk")
			}
		})
	}
}

func TestMirrorFailureFallsBackWithTheSamePublisherDigest(t *testing.T) {
	good := []byte("PK\x03\x04verified-publisher-package")
	bad := []byte("PK\x03\x04corrupted-mirror--package--")
	// Keep exact length so this exercises SHA256 rather than only size guards.
	bad = append(bad, make([]byte, len(good))...)[:len(good)]
	sum := sha256.Sum256(good)
	var mirrorCalls, officialCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := good
		if strings.Contains(r.URL.Path, "mirror") {
			body = bad
			if r.Method == "GET" {
				mirrorCalls.Add(1)
			}
		} else if r.Method == "GET" {
			officialCalls.Add(1)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != "HEAD" {
			_, _ = w.Write(body)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	path, err := downloadCandidates([]string{server.URL + "/mirror.zip", server.URL + "/official.zip"}, dir, "java", nil, downloadIntegrity{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(good))})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, good) {
		t.Fatal("fallback accepted mirror corruption")
	}
	if mirrorCalls.Load() != 2 || officialCalls.Load() != 2 {
		t.Fatalf("corrupt mirror was retried or fallback missing: %d %d", mirrorCalls.Load(), officialCalls.Load())
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("corrupt mirror left files: %v", files)
	}
}
