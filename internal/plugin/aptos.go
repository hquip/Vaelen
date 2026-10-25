package plugin

import (
	"fmt"
	"path/filepath"
	"strings"
)

func init() { register(aptosSDK{}) }

type aptosSDK struct{}

func (aptosSDK) Name() string                     { return "aptos" }
func (aptosSDK) BinPaths(root string) []string    { return []string{root, filepath.Join(root, "bin")} }
func (aptosSDK) ExecEnv(string) map[string]string { return nil }
func (aptosSDK) ListAll() ([]string, error) {
	var refs []struct {
		Ref string `json:"ref"`
	}
	if err := fetchJSONOfficial("https://api.github.com/repos/aptos-labs/aptos-core/git/matching-refs/tags/aptos-cli-v", &refs); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		v := strings.TrimPrefix(ref.Ref, "refs/tags/aptos-cli-v")
		if sdkVersion.MatchString(v) {
			seen[v] = true
		}
	}
	return sortedLanguageVersions(seen), nil
}
func (p aptosSDK) LatestStable() (string, error) {
	versions, err := p.ListAll()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("未找到 Aptos CLI 稳定版本")
	}
	return versions[0], nil
}
func (p aptosSDK) DownloadURL(v string, t Target) (string, error) {
	a, err := p.Artifact(v, t)
	return a.URL, err
}
func (aptosSDK) Artifact(v string, t Target) (Artifact, error) {
	if !sdkVersion.MatchString(v) {
		return Artifact{}, fmt.Errorf("Aptos CLI 需要完整稳定版本号")
	}
	platform := map[string]string{"windows/amd64": "Windows-x86_64", "linux/amd64": "Linux-x86_64", "linux/arm64": "Linux-aarch64", "darwin/amd64": "macOS-x86_64", "darwin/arm64": "macOS-arm64"}[t.OS+"/"+t.Arch]
	if platform == "" {
		return Artifact{}, fmt.Errorf("Aptos CLI 暂无 %s/%s 安装包", t.OS, t.Arch)
	}
	want := "aptos-cli-" + v + "-" + platform + ".zip"
	return publisherReleaseArtifact("aptos-labs/aptos-core", "aptos-cli-v"+v, "zip", func(name string) bool { return name == want })
}
