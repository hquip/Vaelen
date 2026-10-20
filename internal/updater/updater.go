// Package updater installs verified releases of hquip/Vaelen. It never uses
// project tool directories or changes language installations/client settings.
package updater

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"vsm/internal/core"
	"vsm/internal/logging"
	"vsm/internal/netcfg"
)

const repository = "https://github.com/hquip/Vaelen"

var stableVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)

type Asset struct {
	ID             int64  `json:"id,omitempty"`
	Name           string `json:"name"`
	URL            string `json:"browser_download_url"`
	Size           int64  `json:"size"`
	Digest         string `json:"digest"`
	resolvedURL    string
	resolvedSource string
	mirrors        []string
}
type Info struct {
	Current   string  `json:"current"`
	Latest    string  `json:"latest"`
	Available bool    `json:"available"`
	URL       string  `json:"url"`
	Notes     string  `json:"notes"`
	Assets    []Asset `json:"assets"`
}
type Result struct {
	Version         string   `json:"version"`
	Updated         []string `json:"updated"`
	Backups         []string `json:"backups,omitempty"`
	RestartRequired bool     `json:"restart_required"`
}
type Client struct {
	http                                    *http.Client
	metadataDirect                          *http.Client
	api                                     string
	progress                                func(Progress)
	fileIndex, fileCount                    int
	currentAsset                            string
	currentSize, totalBytes, completedBytes int64
	downloadConfig                          *assetDownloadConfig
	networkPolicy                           func(context.Context, Asset) downloadNetwork
	networkSource                           string
}

func New() Client {
	return Client{http: netcfg.Client(10 * time.Minute), metadataDirect: netcfg.DirectClient(30 * time.Second), api: "https://api.github.com/repos/hquip/Vaelen/releases", networkPolicy: automaticDownloadNetwork}
}

func (c Client) Check(ctx context.Context, current, version string) (info Info, err error) {
	c.report(Progress{Stage: "checking"})
	finish := logging.Start("updater", "check", logging.Fields{"current": current, "requested": version})
	defer func() { finish(err) }()
	endpoint := c.api + "/latest"
	if version != "" {
		if !stableVersion.MatchString(version) {
			return Info{}, fmt.Errorf("更新版本必须为稳定版本号，例如 v1.0.5")
		}
		endpoint = c.api + "/tags/v" + strings.TrimPrefix(version, "v")
	}
	data, err := c.get(ctx, endpoint, 4<<20)
	if err != nil {
		return Info{}, err
	}
	var release struct {
		Tag        string  `json:"tag_name"`
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Body       string  `json:"body"`
		Assets     []Asset `json:"assets"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return Info{}, fmt.Errorf("GitHub Release 元数据无效: %w", err)
	}
	if release.Draft || release.Prerelease || !stableVersion.MatchString(release.Tag) {
		return Info{}, fmt.Errorf("仅支持已发布的稳定版本")
	}
	if version != "" && strings.TrimPrefix(version, "v") != strings.TrimPrefix(release.Tag, "v") {
		return Info{}, fmt.Errorf("GitHub 返回了其它版本")
	}
	if len(release.Assets) == 0 {
		return Info{}, fmt.Errorf("该版本没有可更新的安装包")
	}
	return Info{Current: current, Latest: "v" + strings.TrimPrefix(release.Tag, "v"), Available: core.CompareVersions(release.Tag, current) > 0,
		URL: repository + "/releases/tag/" + release.Tag, Notes: release.Body, Assets: release.Assets}, nil
}

func (c Client) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	return c.getWithProgress(ctx, raw, limit, "")
}

func (c Client) getWithProgress(ctx context.Context, raw string, limit int64, asset string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		started := time.Now()
		if asset != "" {
			c.report(Progress{Stage: "downloading", Asset: asset, Total: limit, Attempt: attempt + 1})
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "vsm-updater")
		req.Header.Set("Accept-Encoding", "identity")
		resp, err := c.http.Do(req)
		if err == nil && c.metadataDirect != nil && req.URL.Hostname() == "api.github.com" && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) {
			// Shared proxy exits may exhaust GitHub's unauthenticated API quota.
			// Only metadata retries directly; package transfers retain the proxy.
			if route, routeErr := netcfg.RouteForURL(ctx, raw); routeErr == nil && route.Proxy != "" {
				resp.Body.Close()
				logging.Record("info", "updater", "metadata.direct.fallback", logging.Fields{"reason": "proxy API rate limit"})
				resp, err = c.metadataDirect.Do(req.Clone(ctx))
			}
		}
		if err == nil {
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return nil, fmt.Errorf("GitHub 请求失败: HTTP %d（检查代理、网络或 API 限流）", resp.StatusCode)
			}
			var reader io.Reader = resp.Body
			var progress *progressReader
			if asset != "" {
				progress = &progressReader{reader: resp.Body, started: started, lastEmit: started, update: func(received int64, speed float64) {
					c.report(Progress{Stage: "downloading", Asset: asset, Downloaded: received, Total: limit, BytesPerSecond: speed, Attempt: attempt + 1})
				}}
				reader = progress
			}
			data, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
			resp.Body.Close()
			if progress != nil {
				progress.emit()
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if int64(len(data)) > limit {
				return nil, fmt.Errorf("更新资源超过大小上限")
			}
			if readErr == nil && (resp.ContentLength < 0 || resp.ContentLength == int64(len(data))) {
				return data, nil
			}
			last = readErr
			if last == nil {
				last = io.ErrUnexpectedEOF
			}
		} else {
			last = err
		}
	}
	return nil, fmt.Errorf("读取 GitHub 更新资源失败（3 次）: %w", last)
}

func (i Info) asset(name string) (Asset, error) {
	var found *Asset
	for _, a := range i.Assets {
		if a.Name == name {
			if found != nil {
				return Asset{}, fmt.Errorf("重复更新资源 %s", name)
			}
			value := a
			found = &value
		}
	}
	if found == nil {
		return Asset{}, fmt.Errorf("此版本没有 %s；请到 %s 下载对应程序", name, i.URL)
	}
	want := repository + "/releases/download/" + i.Latest + "/" + url.PathEscape(name)
	if found.URL != want || found.Size <= 0 || found.Size > 256<<20 {
		return Asset{}, fmt.Errorf("更新资源来源或大小无效: %s", name)
	}
	return *found, nil
}

func digestValue(value string) (string, error) {
	if !strings.HasPrefix(value, "sha256:") {
		return "", fmt.Errorf("缺少 SHA256 摘要")
	}
	sum := strings.TrimPrefix(value, "sha256:")
	data, err := hex.DecodeString(sum)
	if err != nil || len(data) != sha256.Size {
		return "", fmt.Errorf("无效 SHA256 摘要")
	}
	return strings.ToLower(sum), nil
}

func (c Client) download(ctx context.Context, info Info, name, dest string) error {
	a, err := info.asset(name)
	if err != nil {
		return err
	}
	sum := ""
	if a.Digest != "" {
		sum, err = digestValue(a.Digest)
		if err != nil {
			return err
		}
	} else {
		manifest, err := info.asset("SHA256SUMS")
		if err != nil {
			return err
		}
		data, err := c.get(ctx, manifest.URL, 64<<10)
		if err != nil {
			return err
		}
		if manifest.Digest != "" {
			want, err := digestValue(manifest.Digest)
			if err != nil {
				return err
			}
			got := sha256.Sum256(data)
			if hex.EncodeToString(got[:]) != want {
				return fmt.Errorf("SHA256SUMS 校验失败")
			}
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
				if sum != "" {
					return fmt.Errorf("重复校验记录")
				}
				sum, err = digestValue("sha256:" + fields[0])
				if err != nil {
					return err
				}
			}
		}
		if sum == "" {
			return fmt.Errorf("发布方未提供 %s 的 SHA256；停止更新", name)
		}
	}
	if c.networkPolicy != nil {
		c.report(Progress{Stage: "routing", Asset: name, Total: a.Size})
		network := c.networkPolicy(ctx, a)
		a.mirrors, c.networkSource = network.mirrors, network.proxy
		logging.Record("info", "updater", "download.route", logging.Fields{"asset": name, "network": network.proxy, "domestic_sources": len(network.mirrors)})
	}
	data, err := c.downloadAsset(ctx, a)
	if err != nil {
		return err
	}
	if int64(len(data)) != a.Size {
		return fmt.Errorf("更新包长度不一致")
	}
	c.report(Progress{Stage: "verifying", Asset: name, Downloaded: a.Size, Total: a.Size})
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != sum && len(a.mirrors) > 0 {
		logging.Record("warn", "updater", "download.mirror.rejected", logging.Fields{"asset": name, "reason": "SHA256"})
		c.report(Progress{Stage: "official-fallback", Asset: name, Total: a.Size})
		a.mirrors, a.resolvedURL = nil, ""
		data, err = c.downloadAsset(ctx, a)
		if err != nil {
			return err
		}
		c.report(Progress{Stage: "verifying", Asset: name, Downloaded: a.Size, Total: a.Size})
		hash = sha256.Sum256(data)
	}
	if hex.EncodeToString(hash[:]) != sum {
		return fmt.Errorf("%s SHA256 校验失败，旧程序未更改", name)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return os.WriteFile(dest, data, 0755)
}

func cliAsset() string {
	name := "vsm-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}
func guiAsset() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return "vsm-gui-macos-universal.zip", nil
	case "windows", "linux":
		if runtime.GOARCH != "amd64" {
			return "", fmt.Errorf("此平台尚无原生 GUI 更新包")
		}
		name := "vsm-gui-" + runtime.GOOS + "-amd64"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		return name, nil
	}
	return "", fmt.Errorf("不支持的 GUI 平台")
}

// Apply stages all requested components before modifying any installed file.
// Windows may retain a running executable backup until the process exits.
func (c Client) Apply(ctx context.Context, info Info, executable string, withGUI bool) (result Result, err error) {
	finish := logging.Start("updater", "apply", logging.Fields{"version": info.Latest, "gui": withGUI})
	defer func() {
		finish(err)
		stage := "done"
		if err != nil {
			stage = "failed"
		}
		c.report(Progress{Stage: stage})
	}()
	result = Result{Version: info.Latest}
	if !stableVersion.MatchString(info.Latest) || core.CompareVersions(info.Latest, info.Current) <= 0 {
		return result, fmt.Errorf("目标版本不比当前版本新，拒绝降级或重复替换")
	}
	exe, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return result, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return result, err
	}
	if runtime.GOOS == "darwin" {
		if bundle := macBundle(exe); bundle != "" {
			c.fileIndex, c.fileCount = 1, 1
			c.currentAsset = "vsm-gui-macos-universal.zip"
			if a, e := info.asset(c.currentAsset); e == nil {
				c.currentSize, c.totalBytes = a.Size, a.Size
			}
			return c.applyBundle(ctx, info, bundle)
		}
		if withGUI {
			return result, fmt.Errorf("macOS GUI 使用完整 VSM.app 更新；请在应用中检查更新，独立 CLI 可运行 vsm update")
		}
	}
	parent := filepath.Dir(exe)
	stage, err := os.MkdirTemp(parent, ".vsm-update-")
	if err != nil {
		return result, fmt.Errorf("程序目录不可写（请更新可写位置的副本）: %w", err)
	}
	defer os.RemoveAll(stage)
	lock, err := os.OpenFile(filepath.Join(parent, ".vsm-update.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, fmt.Errorf("已有更新任务或目录不可写: %w", err)
	}
	lock.Close()
	defer os.Remove(lock.Name())
	name := filepath.Base(exe)
	items := []replacement{}
	cliName := "vsm"
	guiName := "vsm-gui"
	if runtime.GOOS == "windows" {
		cliName += ".exe"
		guiName += ".exe"
	}
	isGUI := strings.HasPrefix(name, "vsm-gui")
	if bi, err := buildinfo.ReadFile(exe); err == nil && bi.Path == "vsm/gui" {
		isGUI = true
	}
	if isGUI {
		asset, err := guiAsset()
		if err != nil {
			return result, err
		}
		items = append(items, replacement{Target: exe, Asset: asset, Main: "vsm/gui"})
		items = append(items, replacement{Target: filepath.Join(parent, cliName), Asset: cliAsset(), Main: "vsm"})
	} else {
		items = append(items, replacement{Target: exe, Asset: cliAsset(), Main: "vsm"})
		if withGUI {
			asset, err := guiAsset()
			if err != nil {
				return result, err
			}
			items = append(items, replacement{Target: adjacentGUI(parent, guiName, asset), Asset: asset, Main: "vsm/gui"})
		}
	}
	c.fileCount = len(items)
	for _, item := range items {
		a, err := info.asset(item.Asset)
		if err != nil {
			return result, err
		}
		c.totalBytes += a.Size
	}
	for n := range items {
		item := &items[n]
		c.fileIndex, c.currentAsset = n+1, item.Asset
		a, _ := info.asset(item.Asset)
		c.currentSize = a.Size
		item.Staged = filepath.Join(stage, fmt.Sprintf("new-%d", n))
		if runtime.GOOS == "windows" {
			item.Staged += ".exe"
		}
		if err := c.download(ctx, info, item.Asset, item.Staged); err != nil {
			return result, err
		}
		c.report(Progress{Stage: "validating", Downloaded: c.currentSize})
		if err := validateBinary(ctx, item.Staged, item.Main, info.Latest); err != nil {
			return result, err
		}
		c.completedBytes += c.currentSize
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	c.report(Progress{Stage: "replacing"})
	return replaceFiles(info.Latest, stage, items)
}

type replacement struct{ Target, Staged, Asset, Main string }

func adjacentGUI(parent, canonical, asset string) string {
	// Preserve an unrenamed GitHub GUI download rather than leaving an outdated
	// launcher next to a second, newly created executable.
	for _, name := range []string{canonical, asset} {
		candidate := filepath.Join(parent, name)
		if _, err := os.Lstat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(parent, canonical)
}

func replaceFiles(version, stage string, items []replacement) (Result, error) {
	result := Result{Version: version, RestartRequired: true}
	type done struct {
		target, backup string
		existed        bool
	}
	var completed []done
	rollback := func(cause error) (Result, error) {
		for n := len(completed) - 1; n >= 0; n-- {
			d := completed[n]
			if err := os.Remove(d.target); err != nil && !os.IsNotExist(err) {
				cause = fmt.Errorf("%w; 回滚删除失败 %s: %v", cause, d.target, err)
			}
			if d.existed {
				if err := os.Rename(d.backup, d.target); err != nil {
					cause = fmt.Errorf("%w; 旧文件保留在 %s: %v", cause, d.backup, err)
				}
			}
		}
		return Result{Version: version}, cause
	}
	// Preflight every destination to avoid replacing a directory or symbolic link.
	for _, item := range items {
		if fi, err := os.Lstat(item.Target); err == nil {
			if !fi.Mode().IsRegular() {
				return result, fmt.Errorf("更新目标必须是普通文件: %s", item.Target)
			}
		} else if !os.IsNotExist(err) {
			return result, err
		}
	}
	for n, item := range items {
		backup := filepath.Join(filepath.Dir(item.Target), fmt.Sprintf("%s.old-%s-%d", filepath.Base(item.Target), filepath.Base(stage), n))
		existed := false
		if _, err := os.Lstat(item.Target); err == nil {
			if err := os.Rename(item.Target, backup); err != nil {
				return rollback(fmt.Errorf("无法备份旧程序 %s: %w", item.Target, err))
			}
			existed = true
		}
		completed = append(completed, done{item.Target, backup, existed})
		if err := os.Rename(item.Staged, item.Target); err != nil {
			return rollback(fmt.Errorf("替换程序失败: %w", err))
		}
		result.Updated = append(result.Updated, item.Target)
	}
	for _, d := range completed {
		if d.existed {
			if err := os.Remove(d.backup); err != nil {
				result.Backups = append(result.Backups, d.backup)
			}
		}
	}
	return result, nil
}

func validateBinary(ctx context.Context, file, main, version string) error {
	bi, err := buildinfo.ReadFile(file)
	if err != nil {
		return fmt.Errorf("下载的程序不是有效 Go 可执行文件: %w", err)
	}
	if bi.Path != main {
		return fmt.Errorf("下载的程序组件不匹配: %s", bi.Path)
	}
	var goos, arch string
	for _, setting := range bi.Settings {
		if setting.Key == "GOOS" {
			goos = setting.Value
		}
		if setting.Key == "GOARCH" {
			arch = setting.Value
		}
	}
	if goos != runtime.GOOS || arch != runtime.GOARCH {
		return fmt.Errorf("下载的程序平台不匹配: %s/%s", goos, arch)
	}
	if main == "vsm" {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(probeCtx, file, "version")
		hideConsole(cmd)
		out, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(out)) != "vsm "+version {
			return fmt.Errorf("新版 CLI 版本验证失败: %v（输出 %.200s）", err, out)
		}
	}
	return nil
}

func macBundle(exe string) string {
	dir := filepath.Dir(exe)
	if filepath.Base(dir) != "MacOS" || filepath.Base(filepath.Dir(dir)) != "Contents" {
		return ""
	}
	bundle := filepath.Dir(filepath.Dir(dir))
	if strings.HasSuffix(bundle, ".app") {
		return bundle
	}
	return ""
}

func (c Client) applyBundle(ctx context.Context, info Info, bundle string) (Result, error) {
	result := Result{Version: info.Latest, RestartRequired: true}
	stage, err := os.MkdirTemp(filepath.Dir(bundle), ".vsm-update-")
	if err != nil {
		return result, fmt.Errorf("应用目录不可写，请将应用移至可写目录: %w", err)
	}
	defer os.RemoveAll(stage)
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(bundle), ".vsm-update.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, fmt.Errorf("已有应用更新任务或目录不可写: %w", err)
	}
	lock.Close()
	defer os.Remove(lock.Name())
	archive := filepath.Join(stage, "update.zip")
	if err := c.download(ctx, info, "vsm-gui-macos-universal.zip", archive); err != nil {
		return result, err
	}
	c.completedBytes = c.totalBytes
	c.report(Progress{Stage: "extracting"})
	extracted := filepath.Join(stage, "extracted")
	if err := core.ExtractWithLimits(archive, extracted, core.ArchiveLimits{MaxFiles: 2000, MaxFileBytes: 256 << 20, MaxTotalBytes: 512 << 20}); err != nil {
		return result, err
	}
	app := filepath.Join(extracted, "VSM.app")
	c.report(Progress{Stage: "validating"})
	cmd := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", app)
	if out, err := cmd.CombinedOutput(); err != nil {
		return result, fmt.Errorf("macOS 应用签名验证失败: %v %.2000s", err, out)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(probeCtx, filepath.Join(app, "Contents", "MacOS", "vsm"), "version")
	out, err := probe.Output()
	if err != nil || strings.TrimSpace(string(out)) != "vsm "+info.Latest {
		return result, fmt.Errorf("macOS 内置 CLI 版本校验失败")
	}
	backup := bundle + ".old-" + filepath.Base(stage)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	c.report(Progress{Stage: "replacing"})
	if err := os.Rename(bundle, backup); err != nil {
		return result, fmt.Errorf("备份应用失败: %w", err)
	}
	if err := os.Rename(app, bundle); err != nil {
		if restore := os.Rename(backup, bundle); restore != nil {
			return result, fmt.Errorf("替换失败 %v，恢复失败 %v；旧应用位于 %s", err, restore, backup)
		}
		return result, err
	}
	// The old bundle may still be running; keep it until the user has restarted.
	result.Updated = []string{bundle}
	result.Backups = []string{backup}
	return result, nil
}
