package plugin

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

func init() { register(&dotnetPlugin{}) }

type dotnetPlugin struct{}
type dotnetChannel struct {
	Channel string `json:"channel-version"`
	Latest  string `json:"latest-sdk"`
	Phase   string `json:"support-phase"`
}
type dotnetFile struct {
	RID  string `json:"rid"`
	URL  string `json:"url"`
	Hash string `json:"hash"`
}
type dotnetSDK struct {
	Version string       `json:"version"`
	Files   []dotnetFile `json:"files"`
}
type dotnetRelease struct {
	SDK  dotnetSDK   `json:"sdk"`
	SDKs []dotnetSDK `json:"sdks"`
}

var sdkVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

const dotnetMetadata = "https://builds.dotnet.microsoft.com/dotnet/release-metadata/"

func (d *dotnetPlugin) Name() string { return "dotnet" }
func dotnetChannels() ([]dotnetChannel, error) {
	var index struct {
		Channels []dotnetChannel `json:"releases-index"`
	}
	if err := fetchJSONOfficial(dotnetMetadata+"releases-index.json", &index); err != nil {
		return nil, err
	}
	return index.Channels, nil
}
func dotnetReleases(channel string) ([]dotnetRelease, error) {
	var data struct {
		Releases []dotnetRelease `json:"releases"`
	}
	if err := fetchJSONOfficial(dotnetMetadata+channel+"/releases.json", &data); err != nil {
		return nil, err
	}
	return data.Releases, nil
}
func (d *dotnetPlugin) ListAll() ([]string, error) {
	channels, err := dotnetChannels()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range channels {
		if c.Phase != "active" && c.Phase != "maintenance" {
			continue
		}
		if !regexp.MustCompile(`^[0-9]+\.[0-9]+$`).MatchString(c.Channel) {
			continue
		}
		releases, err := dotnetReleases(c.Channel)
		if err != nil {
			return nil, err
		}
		for _, r := range releases {
			for _, sdk := range append(r.SDKs, r.SDK) {
				if sdkVersion.MatchString(sdk.Version) && !seen[sdk.Version] {
					seen[sdk.Version] = true
					out = append(out, sdk.Version)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareVer(out[i], out[j]) > 0 })
	return out, nil
}
func (d *dotnetPlugin) LatestStable() (string, error) {
	channels, err := dotnetChannels()
	if err != nil {
		return "", err
	}
	best := ""
	for _, c := range channels {
		if (c.Phase == "active" || c.Phase == "maintenance") && sdkVersion.MatchString(c.Latest) && (best == "" || compareVer(c.Latest, best) > 0) {
			best = c.Latest
		}
	}
	if best == "" {
		return "", fmt.Errorf("未找到受支持的 .NET 稳定 SDK")
	}
	return best, nil
}
func (d *dotnetPlugin) DownloadURL(v string, t Target) (string, error) {
	a, err := d.Artifact(v, t)
	return a.URL, err
}
func (d *dotnetPlugin) Artifact(v string, t Target) (Artifact, error) {
	if !sdkVersion.MatchString(v) {
		return Artifact{}, fmt.Errorf(".NET SDK 版本需要完整版本号，如 8.0.408")
	}
	osName := map[string]string{"windows": "win", "linux": "linux", "darwin": "osx"}[t.OS]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[t.Arch]
	if osName == "" || arch == "" {
		return Artifact{}, fmt.Errorf(".NET 不支持 %s/%s", t.OS, t.Arch)
	}
	parts := strings.Split(v, ".")
	releases, err := dotnetReleases(strings.Join(parts[:2], "."))
	if err != nil {
		return Artifact{}, err
	}
	ext := ".tar.gz"
	if t.OS == "windows" {
		ext = ".zip"
	}
	for _, r := range releases {
		for _, sdk := range append(r.SDKs, r.SDK) {
			if sdk.Version != v {
				continue
			}
			for _, f := range sdk.Files {
				if f.RID != osName+"-"+arch || !strings.HasSuffix(f.URL, ext) {
					continue
				}
				u, err := url.Parse(f.URL)
				if err != nil || u.Scheme != "https" || u.User != nil || (u.Host != "builds.dotnet.microsoft.com" && u.Host != "download.visualstudio.microsoft.com" && u.Host != "dotnetcli.azureedge.net") {
					return Artifact{}, fmt.Errorf("无效 .NET 官方下载地址")
				}
				if !regexp.MustCompile(`^[a-fA-F0-9]{128}$`).MatchString(f.Hash) {
					return Artifact{}, fmt.Errorf("无效 .NET 官方 SHA512")
				}
				return Artifact{Version: v, URL: f.URL, SHA512: strings.ToLower(f.Hash), Format: strings.TrimPrefix(ext, ".")}, nil
			}
		}
	}
	return Artifact{}, fmt.Errorf("未找到 .NET SDK %s 的 %s 安装包", v, osName+"-"+arch)
}
func (d *dotnetPlugin) BinPaths(root string) []string { return []string{root} }
func (d *dotnetPlugin) ExecEnv(root string) map[string]string {
	return map[string]string{"DOTNET_ROOT": root, "DOTNET_ROOT_X64": root, "DOTNET_ROOT_ARM64": root, "DOTNET_MULTILEVEL_LOOKUP": "0"}
}
