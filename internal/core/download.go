package core

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"vsm/internal/logging"
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

type downloadIntegrity struct {
	Format string
	SHA256 string
	SHA512 string
	Size   int64
}

var errDownloadIntegrity = errors.New("下载完整性检查失败")
var errInvalidRange = errors.New("invalid range response")

type downloadHTTPError int

func (e downloadHTTPError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }
func retryableDownload(err error) bool {
	if errors.Is(err, errDownloadIntegrity) || errors.Is(err, errInvalidRange) || errors.Is(err, context.Canceled) {
		return false
	}
	var status downloadHTTPError
	if errors.As(err, &status) {
		return status == 408 || status == 429 || status >= 500
	}
	var disk *os.PathError
	return !errors.As(err, &disk)
}

func downloadCandidates(candidates []string, dir, base string, progress ProgressFunc, integrity downloadIntegrity) (string, error) {
	if progress == nil {
		progress = func(string) {}
	}
	var last error
	for n, candidate := range candidates {
		if n > 0 {
			progress("镜像下载失败，回退官方原始包（保持同一版本和 SHA256）")
		}
		progress("下载 " + candidate)
		path, err := downloadResolve(candidate, dir, base, progress, integrity)
		if err == nil {
			return path, nil
		}
		last = err
		var disk *os.PathError
		if errors.As(err, &disk) {
			return "", err
		}
	}
	if last == nil {
		last = fmt.Errorf("没有可用下载源")
	}
	return "", last
}

// downloadResolve 下载 url（自动跟随重定向），按“最终 URL”的后缀决定压缩格式扩展名，
// 保存为 dir/<base><ext>。失败自动重试。
func downloadResolve(url, dir, base string, progress ProgressFunc, integrity downloadIntegrity) (path string, err error) {
	finish := logging.Start("download", "archive", logging.Fields{"url": url})
	defer func() { finish(err) }()
	if progress == nil {
		progress = func(string) {}
	}
	var lastErr error
	for attempt := 1; attempt <= dlAttempts; attempt++ {
		if attempt > 1 {
			progress(fmt.Sprintf("下载失败，第 %d/%d 次重试：%v", attempt, dlAttempts, lastErr))
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		path, err := downloadOnce(url, dir, base, progress, integrity)
		if err == nil {
			return path, nil
		}
		lastErr = err
		logging.Record("warn", "download", "attempt.failed", logging.Fields{"attempt": attempt, "error": err.Error(), "url": url})
		if !retryableDownload(err) {
			break
		}
	}
	return "", fmt.Errorf("下载失败: %w", lastErr)
}

func downloadOnce(url, dir, base string, progress ProgressFunc, integrity downloadIntegrity) (string, error) {
	if integrity.SHA256 != "" {
		decoded, err := hex.DecodeString(integrity.SHA256)
		if err != nil || len(decoded) != sha256.Size {
			return "", fmt.Errorf("%w: invalid publisher SHA256", errDownloadIntegrity)
		}
	}
	if integrity.SHA512 != "" {
		decoded, err := hex.DecodeString(integrity.SHA512)
		if err != nil || len(decoded) != sha512.Size {
			return "", fmt.Errorf("%w: invalid publisher SHA512", errDownloadIntegrity)
		}
	}
	if integrity.Size < 0 || integrity.Size > dlMaxBytes {
		return "", fmt.Errorf("%w: invalid publisher size", errDownloadIntegrity)
	}

	if err := netcfg.ValidateURL(url); err != nil {
		return "", err
	}
	if err := validateComponent(base); err != nil {
		return "", err
	}
	progress("连接下载源并探测分块支持…")
	finalURL, size, acceptRanges, etag := probe(url)
	if integrity.Size > 0 {
		if size > 0 && size != integrity.Size {
			return "", fmt.Errorf("%w: 下载源大小 %d，与官方 %d 不符", errDownloadIntegrity, size, integrity.Size)
		}
		size = integrity.Size
	}
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
	strongETag := etag != "" && !strings.HasPrefix(etag, "W/")
	if acceptRanges && size >= dlMinParallel && (strongETag || integrity.SHA256 != "" || integrity.SHA512 != "") {
		if !strongETag {
			etag = ""
		}
		progress(fmt.Sprintf("启用 %d 连接分块下载（%s）", dlConnections, humanBytes(size)))
		if err := downloadParallel(finalURL, dest, size, dlConnections, progress, etag); err == nil {
			done = true
		} else {
			logging.Record("warn", "download", "parallel.fallback", logging.Fields{"error": err.Error()})
			progress(fmt.Sprintf("并行下载失败，回退单连接：%v", err))
		}
	}
	if !done {
		if err := downloadStream(finalURL, dest, size, progress); err != nil {
			return "", err
		}
	}
	if integrity.SHA256 != "" {
		progress("校验 sha256 …")
		if err := verifyChecksum(dest, integrity.SHA256); err != nil {
			return "", err
		}
		progress("校验通过")
	}
	if integrity.SHA512 != "" {
		progress("校验 sha512 …")
		if err := verifyDigest(dest, integrity.SHA512, sha512.New()); err != nil {
			return "", err
		}
		progress("校验通过")
	}
	// 用内容魔数纠正扩展名，确保 Extract 用对解压器。
	sniff := sniffArchiveExt(dest)
	if integrity.Format == "binary" && (integrity.SHA256 != "" || integrity.SHA512 != "") {
		if !sniffNativeBinary(dest) {
			return "", fmt.Errorf("download is not a native executable")
		}
		sniff = ".bin"
	}

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

func sniffNativeBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [4]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return false
	}
	return b[0] == 'M' && b[1] == 'Z' || b == [4]byte{0x7f, 'E', 'L', 'F'} || b == [4]byte{0xcf, 0xfa, 0xed, 0xfe} || b == [4]byte{0xfe, 0xed, 0xfa, 0xcf} || b == [4]byte{0xca, 0xfe, 0xba, 0xbe}
}

// Use a one-byte range when HEAD is missing or omits range support. Never
// buffer a whole JDK just to discover its size or range capability.
func probe(raw string) (finalURL string, size int64, acceptRanges bool, etag string) {
	finalURL, size = raw, -1
	client := netcfg.Client(15 * time.Second)
	defer client.CloseIdleConnections()
	request := func(method string, ranged bool) (*http.Response, error) {
		req, err := http.NewRequest(method, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "vsm")
		req.Header.Set("Accept-Encoding", "identity")
		if ranged {
			req.Header.Set("Range", "bytes=0-0")
		}
		return client.Do(req)
	}
	if resp, err := request(http.MethodHead, false); err == nil {
		if resp.StatusCode == http.StatusOK {
			if resp.Request != nil && resp.Request.URL != nil {
				finalURL = resp.Request.URL.String()
			}
			size, etag = resp.ContentLength, resp.Header.Get("ETag")
			acceptRanges = strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")
		}
		resp.Body.Close()
		if acceptRanges && size > 0 {
			return
		}
	}
	if resp, err := request(http.MethodGet, true); err == nil {
		defer resp.Body.Close()
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}
		if resp.StatusCode == http.StatusPartialContent {
			cr := resp.Header.Get("Content-Range")
			if strings.HasPrefix(cr, "bytes 0-0/") {
				total, err := strconv.ParseInt(strings.TrimPrefix(cr, "bytes 0-0/"), 10, 64)
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2))
				if err == nil && readErr == nil && total > 0 && len(body) == 1 && (resp.ContentLength < 0 || resp.ContentLength == 1) {
					return finalURL, total, true, resp.Header.Get("ETag")
				}
			}
		} else if resp.StatusCode == http.StatusOK {
			return finalURL, resp.ContentLength, false, resp.Header.Get("ETag")
		}
	}
	return
}

func downloadStream(url, dest string, size int64, progress ProgressFunc) error {
	client := netcfg.Client(30 * time.Minute)
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vsm")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return downloadHTTPError(resp.StatusCode)
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
	cw := &countingWriter{total: size, label: "下载中", progress: progress, started: time.Now()}
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
	if err := out.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	client := netcfg.Client(30 * time.Minute)
	defer client.CloseIdleConnections()
	chunk := size / int64(conns)
	cw := &countingWriter{total: size, label: "并行下载", progress: progress, started: time.Now()}
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
			if err := downloadChunkWithClient(ctx, client, url, dest, start, end, size, cw, etag); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
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
	defer client.CloseIdleConnections()
	return downloadChunkWithClient(context.Background(), client, url, dest, start, end, total, cw, etag)
}

func downloadChunkWithClient(ctx context.Context, client *http.Client, url, dest string, start, end, total int64, cw *countingWriter, etag string) error {
	offset := start
	var last error
	for attempt := 1; attempt <= dlAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := readDownloadRange(ctx, client, url, dest, offset, end, total, cw, etag)
		if err == nil {
			return nil
		}
		last = err
		offset += n
		if offset > end || !retryableDownload(err) {
			break
		}
		if attempt < dlAttempts {
			logging.Record("warn", "download", "chunk.resume", logging.Fields{"offset": offset, "attempt": attempt + 1, "error": err.Error()})
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
	}
	return fmt.Errorf("分块 %d-%d 下载失败（已接收 %d 字节）: %w", start, end, offset-start, last)
}

func readDownloadRange(ctx context.Context, client *http.Client, url, dest string, start, end, total int64, cw *countingWriter, etag string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "vsm")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("Accept-Encoding", "identity")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, downloadHTTPError(resp.StatusCode)
	}
	want := end - start + 1
	encoding := resp.Header.Get("Content-Encoding")
	if resp.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, total) ||
		(resp.ContentLength >= 0 && resp.ContentLength != want) ||
		(etag != "" && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != etag) ||
		(encoding != "" && !strings.EqualFold(encoding, "identity")) {
		return 0, errInvalidRange
	}
	f, err := os.OpenFile(dest, os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return 0, err
	}
	n, copyErr := io.CopyN(f, io.TeeReader(resp.Body, cw), want)
	closeErr := f.Close()
	if copyErr != nil {
		return n, copyErr
	}
	if closeErr != nil {
		return n, closeErr
	}
	var extra [1]byte
	if n, err := resp.Body.Read(extra[:]); n != 0 || err != io.EOF {
		return want, errInvalidRange
	}
	return want, nil
}

// countingWriter 聚合下载字节数并按节流频率回报进度，既可作为 io.Writer（TeeReader），
// 也可被并行分块通过 add 累加（带锁，可并发）。
type countingWriter struct {
	total    int64
	label    string
	progress ProgressFunc
	mu       sync.Mutex
	n        int64
	started  time.Time
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
	if c.started.IsZero() {
		c.started = now
	}
	if now.Sub(c.last) < 250*time.Millisecond && (c.total <= 0 || c.n < c.total) {
		return
	}
	c.last = now
	speed := int64(float64(c.n) / max(now.Sub(c.started).Seconds(), 0.001))
	if c.total > 0 {
		pct := min(100, int(c.n*100/c.total))
		c.progress(fmt.Sprintf("%s %d%% (%s / %s) · %s/s", c.label, pct, humanBytes(c.n), humanBytes(c.total), humanBytes(speed)))
	} else {
		c.progress(fmt.Sprintf("%s %s · %s/s", c.label, humanBytes(c.n), humanBytes(speed)))
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
	return verifyDigest(path, expected, sha256.New())
}

func verifyDigest(path, expected string, h hash.Hash) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("%w: SHA%d 校验失败：期望 %s，实际 %s", errDownloadIntegrity, h.Size()*8, expected, got)
	}
	return nil
}
