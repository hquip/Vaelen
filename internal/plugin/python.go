package plugin

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

func init() { register(&pythonPlugin{}) }

// pythonPlugin 使用 astral-sh/python-build-standalone 的预编译包（install_only，免编译），
// 从其最新 GitHub Release 中解析可用版本并定位对应平台的下载包。
type pythonPlugin struct{}

var pyAssetRe = regexp.MustCompile(`^cpython-(\d+\.\d+\.\d+)\+\d+-(.+)-install_only\.tar\.gz$`)

func (p *pythonPlugin) Name() string { return "python" }

func (p *pythonPlugin) triple(t Target) (string, error) {
	switch t.OS {
	case "windows":
		switch t.Arch {
		case "amd64":
			return "x86_64-pc-windows-msvc", nil
		case "386":
			return "i686-pc-windows-msvc", nil
		}
	case "linux":
		switch t.Arch {
		case "amd64":
			return "x86_64-unknown-linux-gnu", nil
		case "arm64":
			return "aarch64-unknown-linux-gnu", nil
		}
	case "darwin":
		switch t.Arch {
		case "amd64":
			return "x86_64-apple-darwin", nil
		case "arm64":
			return "aarch64-apple-darwin", nil
		}
	}
	return "", fmt.Errorf("python 不支持的平台: %s/%s", t.OS, t.Arch)
}

// releases 拉取 python-build-standalone 的近 20 个 release（长超时 + 带重试）。
// per_page 不取满 100，是因为该仓库每个 release 含数百个 asset，拉满会让响应过大、
// 触发 GitHub 504；20 个配合长超时既能稳定返回、又能覆盖各系列的多个历史 patch。
func (p *pythonPlugin) releases() ([]ghRelease, error) {
	url := "https://api.github.com/repos/astral-sh/python-build-standalone/releases?per_page=20"
	var lastErr error
	for i := 0; i < 3; i++ {
		var rels []ghRelease
		err := fetchJSONSlow(url, &rels)
		if err == nil {
			return rels, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (p *pythonPlugin) ListAll() ([]string, error) {
	// 聚合多个 release：python-build-standalone 每个 release 里每个系列只放一个最新 patch，
	// 只看最新 release 会漏掉历史 patch（如 3.12.0~3.12.13）。遍历多个 release 收集。
	rels, err := p.releases()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var versions []string
	for _, rel := range rels {
		for _, a := range rel.Assets {
			m := pyAssetRe.FindStringSubmatch(a.Name)
			if m == nil {
				continue
			}
			if v := m[1]; !seen[v] {
				seen[v] = true
				versions = append(versions, v)
			}
		}
	}
	sort.Slice(versions, func(i, j int) bool { return compareVer(versions[i], versions[j]) > 0 })
	return versions, nil
}

func (p *pythonPlugin) LatestStable() (string, error) {
	versions, err := p.ListAll()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("未找到 python 版本")
	}
	return versions[0], nil
}

func (p *pythonPlugin) DownloadURL(version string, t Target) (string, error) {
	triple, err := p.triple(t)
	if err != nil {
		return "", err
	}
	rels, err := p.releases()
	if err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("cpython-%s+", version)
	suffix := fmt.Sprintf("-%s-install_only.tar.gz", triple)
	// 较老的 patch 只存在于历史 release，故需遍历多个 release 查找对应 asset。
	for _, rel := range rels {
		for _, a := range rel.Assets {
			if strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, suffix) {
				return a.URL, nil
			}
		}
	}
	return "", fmt.Errorf("未找到 python %s (%s) 的预编译包", version, triple)
}

func (p *pythonPlugin) BinPaths(installDir string) []string {
	// install_only 解压后顶层目录 python/，剥层后：Windows 下 python.exe 在根，Unix 在 bin/。
	if runtime.GOOS == "windows" {
		return []string{installDir}
	}
	return []string{filepath.Join(installDir, "bin")}
}

func (p *pythonPlugin) ExecEnv(installDir string) map[string]string { return nil }
