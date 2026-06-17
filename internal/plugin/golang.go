package plugin

import (
	"fmt"
	"path/filepath"
	"strings"
)

func init() { register(&golangPlugin{}) }

// golangPlugin 管理 Go 工具链，下载官方预编译包（go.dev/dl）。
type golangPlugin struct{}

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func (g *golangPlugin) Name() string { return "go" }

func (g *golangPlugin) ListAll() ([]string, error) {
	var releases []goRelease
	if err := fetchJSON("https://go.dev/dl/?mode=json&include=all", &releases); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, strings.TrimPrefix(r.Version, "go"))
	}
	return versions, nil
}

func (g *golangPlugin) LatestStable() (string, error) {
	var releases []goRelease
	if err := fetchJSON("https://go.dev/dl/?mode=json", &releases); err != nil {
		return "", err
	}
	for _, r := range releases {
		if r.Stable {
			return strings.TrimPrefix(r.Version, "go"), nil
		}
	}
	return "", fmt.Errorf("未找到 go 稳定版本")
}

func (g *golangPlugin) DownloadURL(version string, t Target) (string, error) {
	return fmt.Sprintf("https://go.dev/dl/go%s.%s-%s%s", version, t.OS, t.Arch, extFor(t.OS)), nil
}

func (g *golangPlugin) BinPaths(installDir string) []string {
	return []string{filepath.Join(installDir, "bin")}
}

func (g *golangPlugin) ExecEnv(installDir string) map[string]string {
	return map[string]string{"GOROOT": installDir}
}

type goFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Sha256   string `json:"sha256"`
	Kind     string `json:"kind"`
}

type goReleaseFull struct {
	Version string   `json:"version"`
	Files   []goFile `json:"files"`
}

// Checksum 从 go.dev/dl 的 JSON 元数据中取出对应平台归档包的官方 sha256。
func (g *golangPlugin) Checksum(version string, t Target) (string, error) {
	var releases []goReleaseFull
	if err := fetchJSON("https://go.dev/dl/?mode=json&include=all", &releases); err != nil {
		return "", err
	}
	want := fmt.Sprintf("go%s.%s-%s%s", version, t.OS, t.Arch, extFor(t.OS))
	for _, r := range releases {
		for _, f := range r.Files {
			if f.Filename == want {
				return f.Sha256, nil
			}
		}
	}
	return "", nil
}
