package plugin

import (
	"fmt"
	"strings"
)

func init() { register(&denoPlugin{}) }

// denoPlugin 管理 Deno，下载 GitHub Releases 的单二进制压缩包（.zip）。
type denoPlugin struct{}

func (d *denoPlugin) Name() string { return "deno" }

func (d *denoPlugin) assetName(t Target) (string, error) {
	m := map[string]string{
		"windows/amd64": "deno-x86_64-pc-windows-msvc.zip",
		"linux/amd64":   "deno-x86_64-unknown-linux-gnu.zip",
		"linux/arm64":   "deno-aarch64-unknown-linux-gnu.zip",
		"darwin/amd64":  "deno-x86_64-apple-darwin.zip",
		"darwin/arm64":  "deno-aarch64-apple-darwin.zip",
	}
	name := m[t.OS+"/"+t.Arch]
	if name == "" {
		return "", fmt.Errorf("deno 不支持的平台: %s/%s", t.OS, t.Arch)
	}
	return name, nil
}

func (d *denoPlugin) ListAll() ([]string, error) {
	rels, err := githubReleases("denoland", "deno")
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, r := range rels {
		if v := strings.TrimPrefix(r.TagName, "v"); v != "" && v != r.TagName {
			versions = append(versions, v)
		}
	}
	return versions, nil
}

func (d *denoPlugin) LatestStable() (string, error) {
	rel, err := githubLatest("denoland", "deno")
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(rel.TagName, "v")
	if v == "" || v == rel.TagName {
		return "", fmt.Errorf("无法解析 deno 版本: %s", rel.TagName)
	}
	return v, nil
}

func (d *denoPlugin) DownloadURL(version string, t Target) (string, error) {
	name, err := d.assetName(t)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/denoland/deno/releases/download/v%s/%s", version, name), nil
}

func (d *denoPlugin) BinPaths(installDir string) []string         { return []string{installDir} }
func (d *denoPlugin) ExecEnv(installDir string) map[string]string { return nil }
