package updater

import (
	"io"
	"time"
)

// Progress describes verified-release download progress. Download completion is
// separate from checksum validation, binary validation and replacement.
type Progress struct {
	Stage             string  `json:"stage"`
	Asset             string  `json:"asset,omitempty"`
	Downloaded        int64   `json:"downloaded"`
	Total             int64   `json:"total"`
	OverallDownloaded int64   `json:"overall_downloaded"`
	OverallTotal      int64   `json:"overall_total"`
	BytesPerSecond    float64 `json:"bytes_per_second"`
	Attempt           int     `json:"attempt,omitempty"`
	FileIndex         int     `json:"file_index,omitempty"`
	FileCount         int     `json:"file_count,omitempty"`
}

// WithProgress returns a client configured for one operation. Callbacks run
// synchronously and receive no URLs, credentials or response bodies.
func (c Client) WithProgress(callback func(Progress)) Client { c.progress = callback; return c }

func (c Client) report(p Progress) {
	if c.progress == nil {
		return
	}
	p.FileIndex, p.FileCount = c.fileIndex, c.fileCount
	if p.Asset == "" {
		p.Asset = c.currentAsset
	}
	if p.Total == 0 {
		p.Total = c.currentSize
	}
	p.OverallTotal = c.totalBytes
	if p.OverallTotal == 0 {
		p.OverallTotal = p.Total
	}
	p.OverallDownloaded = c.completedBytes + p.Downloaded
	if p.Stage == "done" {
		p.OverallDownloaded = p.OverallTotal
	}
	c.progress(p)
}

type progressReader struct {
	reader   io.Reader
	started  time.Time
	lastEmit time.Time
	received int64
	update   func(int64, float64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.received += int64(n)
	if n > 0 && (r.received == int64(n) || time.Since(r.lastEmit) >= 150*time.Millisecond) {
		r.emit()
	}
	return n, err
}

func (r *progressReader) emit() {
	now := time.Now()
	speed := float64(r.received) / max(now.Sub(r.started).Seconds(), 0.001)
	r.lastEmit = now
	r.update(r.received, speed)
}
