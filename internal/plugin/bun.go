package plugin

import (
	"fmt"
	"strings"
)

func init() { register(&bunPlugin{}) }

// bunPlugin 管理 Bun，下载 GitHub Releases 的压缩包（.zip，含一层顶层目录）。
// Bun 的 tag 形如 "bun-v1.1.0"。
type bunPlugin struct{}

func (b *bunPlugin) Name() string { return "bun" }

func (b *bunPlugin) assetName(t Target) (string, error) {
	m := map[string]string{
		"windows/amd64": "bun-windows-x64.zip",
		"linux/amd64":   "bun-linux-x64.zip",
		"linux/arm64":   "bun-linux-aarch64.zip",
		"darwin/amd64":  "bun-darwin-x64.zip",
		"darwin/arm64":  "bun-darwin-aarch64.zip",
	}
	name := m[t.OS+"/"+t.Arch]
	if name == "" {
		return "", fmt.Errorf("bun 不支持的平台: %s/%s", t.OS, t.Arch)
	}
	return name, nil
}

func (b *bunPlugin) ListAll() ([]string, error) {
	rels, err := githubReleases("oven-sh", "bun")
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, r := range rels {
		v := strings.TrimPrefix(r.TagName, "bun-v")
		if v == r.TagName { // 不符合 bun-vX 形式（如 canary）则跳过
			continue
		}
		versions = append(versions, v)
	}
	return versions, nil
}

func (b *bunPlugin) LatestStable() (string, error) {
	rel, err := githubLatest("oven-sh", "bun")
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(rel.TagName, "bun-v")
	if v == rel.TagName {
		return "", fmt.Errorf("无法解析 bun 版本: %s", rel.TagName)
	}
	return v, nil
}

func (b *bunPlugin) DownloadURL(version string, t Target) (string, error) {
	name, err := b.assetName(t)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/oven-sh/bun/releases/download/bun-v%s/%s", version, name), nil
}

func (b *bunPlugin) BinPaths(installDir string) []string         { return []string{installDir} }
func (b *bunPlugin) ExecEnv(installDir string) map[string]string { return nil }
