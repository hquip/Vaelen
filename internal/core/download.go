package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vsm/internal/netcfg"
)

// 下载子系统：支持分块并行下载（HTTP Range）、进度回报、失败重试与 sha256 校验。
// 服务器不支持 Range 或无 Content-Length 时，自动回退为单连接流式下载。

const (
	dlAttempts    = 3
	dlConnections = 4
	dlMinParallel = 8 << 20 // 小于 8MB 不分块，避免徒增连接开销
	dlMaxBytes    = 8 << 30
)

// downloadResolve 下载 url（自动跟随重定向），按“最终 URL”的后缀决定压缩格式扩展名，
// 保存为 dir/<base><ext>。失败自动重试。
func downloadResolve(url, dir, base string, progress ProgressFunc) (string, error) {
	if progress == nil {
		progress = func(string) {}
	}
	var lastErr error
	for attempt := 1; attempt <= dlAttempts; attempt++ {
		if attempt > 1 {
			progress(fmt.Sprintf("下载失败，第 %d/%d 次重试：%v", attempt, dlAttempts, lastErr))
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		path, err := downloadOnce(url, dir, base, progress)
		if err == nil {
			return path, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("下载失败（已重试 %d 次）: %w", dlAttempts, lastErr)
}

func downloadOnce(url, dir, base string, progress ProgressFunc) (string, error) {
	if err := netcfg.ValidateURL(url); err != nil {
		return "", err
	}
	if err := validateComponent(base); err != nil {
		return "", err
	}
	finalURL, size, acceptRanges, etag := probe(url)
	if size > dlMaxBytes {
		return "", fmt.Errorf("download exceeds size limit")
	}
	// 扩展名优先取原始 URL（含真实资产名），再退回重定向后的 URL；都没有则暂定 tar.gz，
	// 最后用内容魔数纠正——因为很多源重定向后会丢掉 .zip/.tar.gz 后缀（GitHub objects、Adoptium）。
	ext := archiveExt(url)
	if ext == "" {
		ext = archiveExt(finalURL)
	}
	if ext == "" {
		ext = ".tar.gz"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "."+base+"-*.partial")
	if err != nil {
		return "", err
	}
	dest := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(dest)
		return "", err
	}
	defer func() { os.Remove(tmp.Name()) }()

	done := false
	if acceptRanges && size >= dlMinParallel && etag != "" && !strings.HasPrefix(etag, "W/") {
		if err := downloadParallel(finalURL, dest, size, dlConnections, progress, etag); err == nil {
			done = true
		} else {
			progress(fmt.Sprintf("并行下载失败，回退单连接：%v", err))
		}
	}
	if !done {
		if err := downloadStream(finalURL, dest, size, progress); err != nil {
			return "", err
		}
	}
	// 用内容魔数纠正扩展名，确保 Extract 用对解压器。
	sniff := sniffArchiveExt(dest)
	if sniff == "" {
		return "", fmt.Errorf("download is not a supported archive (possible HTML/error response)")
	}
	ext = sniff
	nd := filepath.Join(dir, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(dest), "."), ".partial")+ext)
	if err := os.Rename(dest, nd); err != nil {
		return "", err
	}
	return nd, nil
}

// sniffArchiveExt 读取文件头部魔数判断压缩格式，返回 .zip / .tar.gz / .tar.xz；无法判断返回 ""。
func sniffArchiveExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 6)
	n, _ := io.ReadFull(f, buf)
	b := buf[:n]
	switch {
	case len(b) >= 2 && b[0] == 0x50 && b[1] == 0x4B: // PK -> zip
		return ".zip"
	case len(b) >= 2 && b[0] == 0x1F && b[1] == 0x8B: // gzip
		return ".tar.gz"
	case len(b) >= 6 && b[0] == 0xFD && b[1] == 0x37 && b[2] == 0x7A && b[3] == 0x58 && b[4] == 0x5A && b[5] == 0x00: // xz
		return ".tar.xz"
	}
	return ""
}

// probe 用 HEAD 探测最终 URL、内容大小与是否支持 Range；HEAD 不可用时返回 size=-1。
func probe(url string) (finalURL string, size int64, acceptRanges bool, etag string) {
	finalURL = url
	client := netcfg.Client(30 * time.Second)
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return url, -1, false, ""
	}
	req.Header.Set("User-Agent", "vsm")
	resp, err := client.Do(req)
	if err != nil {
		return url, -1, false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return url, -1, false, ""
	}
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return finalURL, resp.ContentLength, strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes"), resp.Header.Get("ETag")
}

func downloadStream(url, dest string, size int64, progress ProgressFunc) error {
	client := netcfg.Client(30 * time.Minute)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vsm")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if size <= 0 {
		size = resp.ContentLength
	}
	if size > dlMaxBytes || (size > 0 && resp.ContentLength >= 0 && size != resp.ContentLength) {
		return fmt.Errorf("download length changed or exceeds limit")
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	cw := &countingWriter{total: size, label: "下载中", progress: progress}
	n, copyErr := io.Copy(out, io.LimitReader(io.TeeReader(resp.Body, cw), dlMaxBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > dlMaxBytes || (size >= 0 && n != size) {
		return fmt.Errorf("download size mismatch: got %d, expected %d", n, size)
	}
	cw.finish()
	return nil
}

func downloadParallel(url, dest string, size int64, conns int, progress ProgressFunc, etag string) error {
	if size <= 0 || size > dlMaxBytes || conns <= 0 || int64(conns) > size {
		return fmt.Errorf("invalid download range layout")
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if err := out.Truncate(size); err != nil {
		out.Close()
		return err
	}
	out.Close()

	chunk := size / int64(conns)
	cw := &countingWriter{total: size, label: "并行下载", progress: progress}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i := 0; i < conns; i++ {
		start := int64(i) * chunk
		end := start + chunk - 1
		if i == conns-1 {
			end = size - 1
		}
		wg.Add(1)
		go func(start, end int64) {
			defer wg.Done()
			if err := downloadChunk(url, dest, start, end, size, cw, etag); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(start, end)
	}
	wg.Wait()
	if firstErr != nil {
		os.Remove(dest)
		return firstErr
	}
	cw.finish()
	return nil
}

func downloadChunk(url, dest string, start, end, total int64, cw *countingWriter, etag string) error {
	client := netcfg.Client(30 * time.Minute)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vsm")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("Accept-Encoding", "identity")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("服务器不支持 Range: HTTP %d", resp.StatusCode)
	}
	want := end - start + 1
	if resp.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, total) || (resp.ContentLength >= 0 && resp.ContentLength != want) || (etag != "" && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != etag) {
		return fmt.Errorf("invalid Content-Range, length, or entity tag")
	}
	f, err := os.OpenFile(dest, os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.CopyN(f, io.TeeReader(resp.Body, cw), want); err != nil {
		return err
	}
	var extra [1]byte
	if n, err := resp.Body.Read(extra[:]); n != 0 || err != io.EOF {
		return fmt.Errorf("range response exceeds expected length")
	}
	return f.Close()
}

// countingWriter 聚合下载字节数并按节流频率回报进度，既可作为 io.Writer（TeeReader），
// 也可被并行分块通过 add 累加（带锁，可并发）。
type countingWriter struct {
	total    int64
	label    string
	progress ProgressFunc
	mu       sync.Mutex
	n        int64
	lastPct  int
	last     time.Time
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.add(int64(len(p)))
	return len(p), nil
}

func (c *countingWriter) add(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += n
	now := time.Now()
	if c.total > 0 {
		pct := int(c.n * 100 / c.total)
		if pct != c.lastPct && (now.Sub(c.last) > 200*time.Millisecond || pct >= 100) {
			c.lastPct = pct
			c.last = now
			c.progress(fmt.Sprintf("%s %d%% (%s / %s)", c.label, pct, humanBytes(c.n), humanBytes(c.total)))
		}
	} else if now.Sub(c.last) > 500*time.Millisecond {
		c.last = now
		c.progress(fmt.Sprintf("%s %s", c.label, humanBytes(c.n)))
	}
}

func (c *countingWriter) finish() {
	if c.total > 0 {
		c.progress(fmt.Sprintf("%s 完成 %s", c.label, humanBytes(c.total)))
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// archiveExt 由 URL（去掉 query/fragment）推断压缩包扩展名。
func archiveExt(url string) string {
	lower := strings.ToLower(url)
	if i := strings.IndexAny(lower, "?#"); i >= 0 {
		lower = lower[:i]
	}
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".tgz"):
		return ".tgz"
	case strings.HasSuffix(lower, ".tar.xz"):
		return ".tar.xz"
	case strings.HasSuffix(lower, ".zip"):
		return ".zip"
	default:
		return filepath.Ext(lower)
	}
}

// verifyChecksum 计算文件 sha256 并与期望值（小写十六进制）比较。
func verifyChecksum(path, expected string) error {
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid SHA256 metadata")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("sha256 校验失败：期望 %s，实际 %s", expected, got)
	}
	return nil
}
