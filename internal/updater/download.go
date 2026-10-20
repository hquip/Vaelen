package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"vsm/internal/logging"
)

var errAssetIdle = errors.New("更新下载连接持续没有收到数据")
var errAssetRange = errors.New("下载源未正确支持分块")
var errAssetResponse = errors.New("更新包响应无效")

type assetDownloadConfig struct {
	idle         time.Duration
	parallelFrom int64
	connections  int
}

func (c Client) assetConfig() assetDownloadConfig {
	cfg := assetDownloadConfig{idle: 15 * time.Second, parallelFrom: 8 << 20, connections: 4}
	if c.downloadConfig != nil {
		if c.downloadConfig.idle > 0 {
			cfg.idle = c.downloadConfig.idle
		}
		if c.downloadConfig.parallelFrom > 0 {
			cfg.parallelFrom = c.downloadConfig.parallelFrom
		}
		if c.downloadConfig.connections > 0 {
			cfg.connections = min(4, c.downloadConfig.connections)
		}
	}
	return cfg
}

// Callbacks are serialized across range workers. Byte counts include retained
// partial segments; a source that ignores Range explicitly resets that data.
type assetProgress struct {
	mu            sync.Mutex
	client        Client
	asset         Asset
	received      int64
	started, last time.Time
	connections   int
	resolvedURL   string
}

func (p *assetProgress) emit(stage string, delta int64, attempt int, source string, force bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	previous := p.received
	p.received += delta
	now := time.Now()
	if !force && previous > 0 && now.Sub(p.last) < 150*time.Millisecond && p.received < p.asset.Size {
		return
	}
	p.last = now
	p.client.report(Progress{Stage: stage, Asset: p.asset.Name, Downloaded: p.received, Total: p.asset.Size, Attempt: attempt, Source: source, Connections: p.connections, BytesPerSecond: float64(p.received) / max(now.Sub(p.started).Seconds(), 0.001)})
}

type assetResponse struct {
	*http.Response
	ctx    context.Context
	timer  *time.Timer
	cancel context.CancelCauseFunc
}

func (r *assetResponse) close() { r.timer.Stop(); r.Body.Close(); r.cancel(nil) }
func (r *assetResponse) failure(err error) error {
	if errors.Is(context.Cause(r.ctx), errAssetIdle) {
		return fmt.Errorf("%w: %w", errAssetIdle, err)
	}
	return err
}

func (c Client) assetRoute(a Asset, attempt int) (string, string) {
	raw, source := a.URL, "github"
	if a.resolvedURL != "" {
		raw, source = a.resolvedURL, "github-cdn"
	}
	if a.ID > 0 && attempt%2 == 0 {
		raw = c.api + "/assets/" + strconv.FormatInt(a.ID, 10)
		source = "github-api"
	}
	return raw, source
}

func (c Client) openAsset(ctx context.Context, a Asset, start, end int64, ranged bool, attempt int) (*assetResponse, string, error) {
	raw, source := c.assetRoute(a, attempt)
	requestCtx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(c.assetConfig().idle, func() { cancel(errAssetIdle) })
	stop := func() { timer.Stop(); cancel(nil) }
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, raw, nil)
	if err != nil {
		stop()
		return nil, source, err
	}
	req.Header.Set("User-Agent", "vsm-updater")
	req.Header.Set("Accept-Encoding", "identity")
	if source == "github-api" {
		req.Header.Set("Accept", "application/octet-stream")
	}
	if ranged {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(context.Cause(requestCtx), errAssetIdle) {
			err = fmt.Errorf("%w: %w", errAssetIdle, err)
		}
		stop()
		return nil, source, err
	}
	return &assetResponse{Response: resp, ctx: requestCtx, timer: timer, cancel: cancel}, source, nil
}

func validAssetRange(value string, start, end, total int64) bool {
	return value == fmt.Sprintf("bytes %d-%d/%d", start, end, total)
}

func retryAsset(err error) bool {
	if errors.Is(err, errAssetIdle) {
		return true
	}
	if errors.Is(err, errAssetRange) || errors.Is(err, errAssetResponse) || errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

// Each worker retains its received bytes after a broken connection. Mixing
// range responses is allowed only because the complete official SHA256 is
// mandatory before these bytes can be staged or replace a running program.
func (c Client) fetchAssetSegment(ctx context.Context, a Asset, data []byte, start, end int64, ranged, allowFull bool, p *assetProgress) (whole bool, err error) {
	pos := start
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		_, source := c.assetRoute(a, attempt)
		stage := "connecting"
		if attempt > 1 {
			stage = "retrying"
		}
		p.emit(stage, 0, attempt, source, true)
		resp, source, openErr := c.openAsset(ctx, a, pos, end, ranged || pos > start, attempt)
		if openErr != nil {
			last = openErr
			logging.Record("warn", "updater", "download.retry", logging.Fields{"asset": a.Name, "attempt": attempt, "source": source, "stalled": errors.Is(last, errAssetIdle), "retained_bytes": pos - start})
			continue
		}
		last = func() error {
			defer resp.close()
			switch resp.StatusCode {
			case http.StatusOK:
				if start != 0 || !allowFull {
					return errAssetRange
				}
				if pos > 0 {
					p.emit("downloading", -pos, attempt, source, true)
					pos = 0
				}
				end = a.Size - 1
				whole = true
			case http.StatusPartialContent:
				if !validAssetRange(resp.Header.Get("Content-Range"), pos, end, a.Size) {
					return fmt.Errorf("%w: Content-Range", errAssetRange)
				}
			default:
				return fmt.Errorf("GitHub 下载返回 HTTP %d", resp.StatusCode)
			}
			if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
				return fmt.Errorf("%w: 官方接口返回了元数据而非安装包", errAssetResponse)
			}
			if resp.ContentLength >= 0 && resp.ContentLength != end-pos+1 {
				return fmt.Errorf("%w: 响应长度与所请求区间不符", errAssetResponse)
			}
			p.emit("downloading", 0, attempt, source, true)
			for pos <= end {
				readEnd := min(end+1, pos+(32<<10))
				n, readErr := resp.Body.Read(data[pos:readEnd])
				if n > 0 {
					pos += int64(n)
					resp.timer.Reset(c.assetConfig().idle)
					p.emit("downloading", int64(n), attempt, source, false)
				}
				if readErr != nil {
					if readErr == io.EOF && pos == end+1 {
						return nil
					}
					if readErr == io.EOF {
						readErr = io.ErrUnexpectedEOF
					}
					return resp.failure(readErr)
				}
			}
			var extra [1]byte
			n, readErr := resp.Body.Read(extra[:])
			if n > 0 {
				return fmt.Errorf("%w: 更新包响应超出区间", errAssetResponse)
			}
			if readErr != io.EOF {
				return resp.failure(fmt.Errorf("读取更新包末尾失败: %w", readErr))
			}
			return nil
		}()
		if last == nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if start == 0 && allowFull && resp.Request != nil && resp.Request.URL != nil {
				p.resolvedURL = resp.Request.URL.String()
			}
			return whole, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if !retryAsset(last) {
			return false, last
		}
		logging.Record("warn", "updater", "download.retry", logging.Fields{"asset": a.Name, "attempt": attempt, "source": source, "stalled": errors.Is(last, errAssetIdle), "retained_bytes": pos - start})
		// All bytes in this range arrived; a trailing read error does not permit
		// a request outside its bounds. Re-read just this segment on retry.
		if pos > end {
			p.emit("retrying", -(pos - start), attempt, source, true)
			pos = start
		}
	}
	p.emit("downloading", 0, 3, "", true)
	return false, fmt.Errorf("更新包下载失败（3 次，检查代理或网络）: %w", last)
}

func (c Client) downloadAsset(ctx context.Context, a Asset) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	data := make([]byte, a.Size)
	p := &assetProgress{client: c, asset: a, started: time.Now(), connections: 1}
	cfg := c.assetConfig()
	if a.Size < cfg.parallelFrom || a.Size < 2 || cfg.connections == 1 {
		_, err := c.fetchAssetSegment(ctx, a, data, 0, a.Size-1, false, true, p)
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	whole, err := c.fetchAssetSegment(ctx, a, data, 0, 0, true, true, p)
	if err != nil {
		return nil, err
	}
	if whole {
		return data, nil
	}
	a.resolvedURL = p.resolvedURL
	count := int(min(int64(cfg.connections), a.Size-1))
	logging.Record("info", "updater", "download.parallel", logging.Fields{"asset": a.Name, "connections": count, "bytes": a.Size})
	p.connections = count
	p.emit("parallel", 0, 1, "github", true)
	parallelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var first error
	var once sync.Once
	for i := 0; i < count; i++ {
		start := 1 + int64(i)*(a.Size-1)/int64(count)
		end := int64(i+1) * (a.Size - 1) / int64(count)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.fetchAssetSegment(parallelCtx, a, data, start, end, true, false, p); err != nil {
				once.Do(func() { first = err; cancel() })
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if first == nil {
		return data, nil
	}
	if !errors.Is(first, errAssetRange) {
		return nil, first
	}
	// Some proxies accept the one-byte probe but ignore real ranges. Discard
	// parallel data before falling back to the bounded full-response reader.
	p.connections = 1
	logging.Record("warn", "updater", "download.range.fallback", logging.Fields{"asset": a.Name})
	p.emit("fallback", -p.received, 1, "github", true)
	_, err = c.fetchAssetSegment(ctx, a, data, 0, a.Size-1, false, true, p)
	if err != nil {
		return nil, err
	}
	return data, nil
}
