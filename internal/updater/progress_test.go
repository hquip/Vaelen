package updater

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStreamingProgressArrivesBeforeDownloadFinishes(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 64<<10)
	client, info := releaseFixture(t, data)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data[:len(data)/2])
		w.(http.Flusher).Flush()
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Write(data[len(data)/2:])
	}))
	defer server.Close()
	defer release() // unblock the handler before server.Close on failure
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	progress := make(chan Progress, 32)
	client = client.WithProgress(func(p Progress) { progress <- p })
	dest := filepath.Join(t.TempDir(), "verified-download")
	done := make(chan error, 1)
	go func() { done <- client.download(context.Background(), info, cliAsset(), dest) }()
	var intermediate bool
	deadline := time.After(4 * time.Second)
	for !intermediate {
		select {
		case p := <-progress:
			if p.Stage == "downloading" && p.Downloaded > 0 {
				if p.Downloaded >= int64(len(data)) || p.Total != int64(len(data)) || p.BytesPerSecond <= 0 || math.IsInf(p.BytesPerSecond, 0) {
					t.Fatalf("invalid live progress: %+v", p)
				}
				intermediate = true
			}
		case <-deadline:
			t.Fatal("no progress before server finished")
		}
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("unverified download was written to destination")
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	close(progress)
	verified := false
	for p := range progress {
		if p.Stage == "verifying" && p.Downloaded == int64(len(data)) {
			verified = true
		}
	}
	if !verified {
		t.Fatal("checksum phase not reported")
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("progress changed download contents")
	}
}

func TestRetryProgressResetsBytesAndReportsAttempt(t *testing.T) {
	data := bytes.Repeat([]byte("retry"), 1000)
	client, info := releaseFixture(t, data)
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if attempt == 1 {
			w.Write(data[:100])
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	client.http.Transport = redirectTransport{server.Client().Transport, strings.TrimPrefix(server.URL, "http://")}
	var seen []Progress
	client = client.WithProgress(func(p Progress) { seen = append(seen, p) })
	if err := client.download(context.Background(), info, cliAsset(), filepath.Join(t.TempDir(), "retry")); err != nil {
		t.Fatal(err)
	}
	reset, complete := false, false
	for _, p := range seen {
		if p.Stage == "downloading" && p.Attempt == 2 && p.Downloaded == 0 {
			reset = true
		}
		if p.Stage == "downloading" && p.Attempt == 2 && p.Downloaded == int64(len(data)) {
			complete = true
		}
	}
	if !reset || !complete {
		t.Fatalf("missing retry progress: %+v", seen)
	}
}

func TestProgressCannotTurnChecksumFailureIntoSuccess(t *testing.T) {
	client, info := releaseFixture(t, []byte("damaged payload"))
	info.Assets[0].Digest = "sha256:" + strings.Repeat("0", 64)
	var seen []Progress
	client = client.WithProgress(func(p Progress) { seen = append(seen, p) })
	target := filepath.Join(t.TempDir(), "vsm")
	if err := os.WriteFile(target, []byte("old-program"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Apply(context.Background(), info, target, false); err == nil {
		t.Fatal("bad checksum accepted")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old-program" {
		t.Fatal("progress caused unverified replacement")
	}
	for _, p := range seen {
		if p.Stage == "done" || p.Stage == "replacing" {
			t.Fatalf("reported success on failure: %+v", seen)
		}
	}
	if len(seen) == 0 || seen[len(seen)-1].Stage != "failed" {
		t.Fatal("missing failure event")
	}
}

func TestProgressRespectsCancellation(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 64<<10)
	client, info := releaseFixture(t, data)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client = client.WithProgress(func(p Progress) {
		if p.Downloaded > 0 {
			cancel()
		}
	})
	dest := filepath.Join(t.TempDir(), "cancelled")
	err := client.download(ctx, info, cliAsset(), dest)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("cancelled download was staged")
	}
}

func TestAggregateProgressAndCompletion(t *testing.T) {
	var seen []Progress
	c := Client{progress: func(p Progress) { seen = append(seen, p) }, fileIndex: 2, fileCount: 2, currentAsset: "GUI", currentSize: 200, completedBytes: 100, totalBytes: 300}
	c.report(Progress{Stage: "downloading", Downloaded: 50})
	c.report(Progress{Stage: "done"})
	if seen[0].OverallDownloaded != 150 || seen[0].OverallTotal != 300 || seen[0].FileIndex != 2 || seen[0].FileCount != 2 {
		t.Fatalf("incorrect overall bytes: %+v", seen[0])
	}
	if seen[1].OverallDownloaded != 300 {
		t.Fatal("completion did not reach 100 percent")
	}
}
