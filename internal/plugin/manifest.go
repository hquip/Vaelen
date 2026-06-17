package plugin

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed manifests/*.json
var manifestFS embed.FS

// Manifest 是声明式语言插件定义：用数据（而非 Go 代码）描述一门语言怎么列版本、
// 怎么下载、装到哪、需要哪些环境变量。内置一批，也可放到 ~/.vsm/plugins/*.json
// （或 $VSM_HOME/plugins）由用户/社区扩展——这正是支持数十种语言而无需改代码的关键。
type Manifest struct {
	Name   string            `json:"name"`
	Source ManifestSource    `json:"source"`
	URL    string            `json:"url"`    // 下载 URL 模板，占位 {version} {os} {arch} {asset}
	Assets map[string]string `json:"assets"` // "os/arch" -> asset 名模板（占位 {version} {os} {arch}）
	Bin    string            `json:"bin"`    // bin 子目录（相对安装目录，空=根），用 / 分隔
	Env    map[string]string `json:"env"`    // 环境变量，值可用 {dir} 代表安装目录
}

// ManifestSource 描述如何获取可用版本列表。
type ManifestSource struct {
	Type        string `json:"type"`        // github-releases | github-tags
	Repo        string `json:"repo"`        // owner/repo
	StripPrefix string `json:"stripPrefix"` // 去除的 tag 前缀（如 v / bun-v）
}

// manifestPlugin 用 Manifest 驱动，实现 Plugin 接口。
type manifestPlugin struct{ m Manifest }

func (p manifestPlugin) Name() string { return p.m.Name }

func (p manifestPlugin) ListAll() ([]string, error) {
	owner, repo := splitRepo(p.m.Source.Repo)
	var tags []string
	var err error
	switch p.m.Source.Type {
	case "github-releases":
		var rels []ghRelease
		rels, err = githubReleases(owner, repo)
		for _, r := range rels {
			if !r.Prerelease {
				tags = append(tags, r.TagName)
			}
		}
	case "github-tags":
		tags, err = githubTags(owner, repo)
	default:
		return nil, fmt.Errorf("%s: 不支持的版本源类型 %q", p.m.Name, p.m.Source.Type)
	}
	if err != nil {
		return nil, err
	}
	return tagsToVersions(tags, p.m.Source.StripPrefix), nil
}

func (p manifestPlugin) LatestStable() (string, error) {
	vs, err := p.ListAll()
	if err != nil {
		return "", err
	}
	if len(vs) == 0 {
		return "", fmt.Errorf("%s: 无可用版本", p.m.Name)
	}
	return vs[0], nil
}

func (p manifestPlugin) DownloadURL(version string, t Target) (string, error) {
	if len(p.m.Assets) == 0 {
		return "", fmt.Errorf("%s: 暂未配置下载源（可被识别/切换；如需安装，请在 manifest 的 assets 中补充下载模板）", p.m.Name)
	}
	key := t.OS + "/" + t.Arch
	asset, ok := p.m.Assets[key]
	if !ok {
		return "", fmt.Errorf("%s: 不支持的平台 %s", p.m.Name, key)
	}
	asset = expandVars(asset, version, t)
	url := expandVars(p.m.URL, version, t)
	return strings.ReplaceAll(url, "{asset}", asset), nil
}

func (p manifestPlugin) BinPaths(installDir string) []string {
	if p.m.Bin == "" {
		return []string{installDir}
	}
	return []string{filepath.Join(installDir, filepath.FromSlash(p.m.Bin))}
}

func (p manifestPlugin) ExecEnv(installDir string) map[string]string {
	if len(p.m.Env) == 0 {
		return nil
	}
	out := make(map[string]string, len(p.m.Env))
	for k, v := range p.m.Env {
		out[k] = strings.ReplaceAll(v, "{dir}", installDir)
	}
	return out
}

func expandVars(s, version string, t Target) string {
	return strings.NewReplacer(
		"{version}", version,
		"{os}", t.OS,
		"{arch}", t.Arch,
	).Replace(s)
}

func splitRepo(s string) (owner, repo string) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return s, ""
	}
	return parts[0], parts[1]
}

// tagsToVersions 把 tag 列表转为版本号列表（去前缀、过滤非版本、按版本降序）。
func tagsToVersions(tags []string, stripPrefix string) []string {
	var out []string
	for _, t := range tags {
		v := t
		if stripPrefix != "" {
			if !strings.HasPrefix(t, stripPrefix) {
				continue
			}
			v = strings.TrimPrefix(t, stripPrefix)
		}
		if v == "" || v[0] < '0' || v[0] > '9' {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return compareVer(out[i], out[j]) > 0 })
	return out
}

func init() { loadManifests() }

func loadManifests() {
	if entries, err := manifestFS.ReadDir("manifests"); err == nil {
		for _, e := range entries {
			if data, rerr := manifestFS.ReadFile("manifests/" + e.Name()); rerr == nil {
				registerManifestData(data)
			}
		}
	}
	for _, f := range externalManifestFiles() {
		if data, err := os.ReadFile(f); err == nil {
			registerManifestData(data)
		}
	}
}

// externalManifestFiles 返回用户自定义 manifest 目录下的所有 *.json。
func externalManifestFiles() []string {
	var dir string
	if env := os.Getenv("VSM_HOME"); env != "" {
		dir = filepath.Join(env, "plugins")
	} else if home, err := os.UserHomeDir(); err == nil {
		dir = filepath.Join(home, ".vsm", "plugins")
	}
	if dir == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	return files
}

// registerManifestData 解析 JSON（支持单个对象或数组），注册其中的 manifest 插件。
// 容忍 UTF-8 BOM（某些编辑器/PowerShell 会写入），否则 JSON 解析会失败。
func registerManifestData(data []byte) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var arr []Manifest
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		for _, m := range arr {
			if m.Name != "" {
				register(manifestPlugin{m})
			}
		}
		return
	}
	var single Manifest
	if err := json.Unmarshal(data, &single); err == nil && single.Name != "" {
		register(manifestPlugin{single})
	}
}
