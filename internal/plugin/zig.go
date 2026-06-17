package plugin

import (
	"encoding/json"
	"fmt"
	"sort"
)

func init() { register(&zigPlugin{}) }

// zigPlugin 管理 Zig 工具链，使用官方 index.json：直接取其中的 tarball 字段，
// 避免因 Zig 文件名规则变动（os-arch ↔ arch-os）而拼错 URL。
type zigPlugin struct{}

func (z *zigPlugin) Name() string { return "zig" }

func (z *zigPlugin) index() (map[string]map[string]json.RawMessage, error) {
	var idx map[string]map[string]json.RawMessage
	if err := fetchJSON("https://ziglang.org/download/index.json", &idx); err != nil {
		return nil, err
	}
	return idx, nil
}

func (z *zigPlugin) archKey(t Target) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64", "386": "x86"}[t.Arch]
	if arch == "" {
		return "", fmt.Errorf("zig 不支持的架构: %s", t.Arch)
	}
	osn := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[t.OS]
	if osn == "" {
		return "", fmt.Errorf("zig 不支持的系统: %s", t.OS)
	}
	return arch + "-" + osn, nil
}

func (z *zigPlugin) ListAll() ([]string, error) {
	idx, err := z.index()
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(idx))
	for k := range idx {
		if k == "master" {
			continue
		}
		versions = append(versions, k)
	}
	sort.Slice(versions, func(i, j int) bool { return compareVer(versions[i], versions[j]) > 0 })
	return versions, nil
}

func (z *zigPlugin) LatestStable() (string, error) {
	versions, err := z.ListAll()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("未找到 zig 版本")
	}
	return versions[0], nil
}

func (z *zigPlugin) DownloadURL(version string, t Target) (string, error) {
	key, err := z.archKey(t)
	if err != nil {
		return "", err
	}
	idx, err := z.index()
	if err != nil {
		return "", err
	}
	ver, ok := idx[version]
	if !ok {
		return "", fmt.Errorf("zig 无此版本: %s", version)
	}
	raw, ok := ver[key]
	if !ok {
		return "", fmt.Errorf("zig %s 无 %s 构建", version, key)
	}
	var entry struct {
		Tarball string `json:"tarball"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", err
	}
	if entry.Tarball == "" {
		return "", fmt.Errorf("zig %s/%s 缺少下载地址", version, key)
	}
	return entry.Tarball, nil
}

func (z *zigPlugin) BinPaths(installDir string) []string         { return []string{installDir} }
func (z *zigPlugin) ExecEnv(installDir string) map[string]string { return nil }
