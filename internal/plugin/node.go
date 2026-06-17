package plugin

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

func init() { register(&nodePlugin{}) }

// nodePlugin 管理 Node.js，下载官方预编译包（nodejs.org/dist）。
type nodePlugin struct{}

type nodeRelease struct {
	Version string `json:"version"`
}

func (n *nodePlugin) Name() string { return "node" }

func (n *nodePlugin) ListAll() ([]string, error) {
	var releases []nodeRelease
	if err := fetchJSON("https://nodejs.org/dist/index.json", &releases); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, strings.TrimPrefix(r.Version, "v"))
	}
	return versions, nil
}

func (n *nodePlugin) LatestStable() (string, error) {
	versions, err := n.ListAll()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("未找到 node 版本")
	}
	return versions[0], nil
}

func (n *nodePlugin) DownloadURL(version string, t Target) (string, error) {
	nodeOS := map[string]string{"windows": "win", "linux": "linux", "darwin": "darwin"}[t.OS]
	if nodeOS == "" {
		return "", fmt.Errorf("node 不支持的系统: %s", t.OS)
	}
	nodeArch := map[string]string{"amd64": "x64", "arm64": "arm64", "386": "x86"}[t.Arch]
	if nodeArch == "" {
		return "", fmt.Errorf("node 不支持的架构: %s", t.Arch)
	}
	return fmt.Sprintf("https://nodejs.org/dist/v%s/node-v%s-%s-%s%s", version, version, nodeOS, nodeArch, extFor(t.OS)), nil
}

func (n *nodePlugin) BinPaths(installDir string) []string {
	// Windows zip 解压后 node.exe 位于根目录；其它平台位于 bin/ 下。
	if runtime.GOOS == "windows" {
		return []string{installDir}
	}
	return []string{filepath.Join(installDir, "bin")}
}

func (n *nodePlugin) ExecEnv(installDir string) map[string]string { return nil }
