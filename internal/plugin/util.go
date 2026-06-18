package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"vsm/internal/netcfg"
)

var httpClient = netcfg.Client(30 * time.Second)

// fetchJSON 发起 GET 请求并把响应体解析为 JSON。
func fetchJSON(url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, MirrorURL(url), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vsm")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("请求 %s 失败: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// slowHTTPClient 用更长超时，应对体积很大的 API 响应（如 python-build-standalone 的 releases，
// 单页可达数 MB，默认 30s 容易下载超时）。
var slowHTTPClient = netcfg.Client(2 * time.Minute)

// fetchJSONSlow 与 fetchJSON 相同，但使用更长超时的 client。
func fetchJSONSlow(url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, MirrorURL(url), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vsm")
	resp, err := slowHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("请求 %s 失败: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// extFor 返回某平台下官方发行包常用的压缩扩展名。
func extFor(goos string) string {
	if goos == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

// --- GitHub releases 通用工具（供 deno/bun/python 等基于 GitHub 发布的插件复用）---

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

// maxGitHubPages 限制 GitHub 列表接口的翻页上限：per_page=100，最多 maxGitHubPages 页
// （约 1000 条）。既能覆盖几乎所有历史版本，又给匿名接口 60 次/小时的限流留足余量。
const maxGitHubPages = 10

func githubReleases(owner, repo string) ([]ghRelease, error) {
	var all []ghRelease
	for page := 1; page <= maxGitHubPages; page++ {
		var rels []ghRelease
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100&page=%d", owner, repo, page)
		if err := fetchJSON(url, &rels); err != nil {
			if page == 1 {
				return nil, err
			}
			break // 后续页失败：用已取到的，不让单页出错毁掉整个列表
		}
		all = append(all, rels...)
		if len(rels) < 100 {
			break // 不足一页 = 最后一页
		}
	}
	return all, nil
}

func githubLatest(owner, repo string) (ghRelease, error) {
	var rel ghRelease
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	err := fetchJSON(url, &rel)
	return rel, err
}

type ghTag struct {
	Name string `json:"name"`
}

func githubTags(owner, repo string) ([]string, error) {
	var out []string
	for page := 1; page <= maxGitHubPages; page++ {
		var tags []ghTag
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=100&page=%d", owner, repo, page)
		if err := fetchJSON(url, &tags); err != nil {
			if page == 1 {
				return nil, err
			}
			break // 后续页失败：用已取到的
		}
		for _, t := range tags {
			out = append(out, t.Name)
		}
		if len(tags) < 100 {
			break // 不足一页 = 最后一页
		}
	}
	return out, nil
}

// --- 版本号比较（plugin 包内部用，避免依赖 core 造成循环导入）---

func compareVer(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			ai = leadingInt(as[i])
		}
		if i < len(bs) {
			bi = leadingInt(bs[i])
		}
		if ai != bi {
			if ai > bi {
				return 1
			}
			return -1
		}
	}
	return 0
}

func leadingInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
