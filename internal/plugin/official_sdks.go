package plugin

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func init() {
	for _, name := range []string{"dart", "julia", "flutter"} {
		register(officialSDK{name})
	}
}

type officialSDK struct{ name string }

func (p officialSDK) Name() string { return p.name }
func (p officialSDK) BinPaths(root string) []string {
	if p.name == "julia" {
		return []string{filepath.Join(root, "bin"), filepath.Join(root, "Contents", "Resources", "julia", "bin")}
	}
	return []string{filepath.Join(root, "bin")}
}
func (p officialSDK) ExecEnv(root string) map[string]string {
	if p.name == "flutter" {
		return map[string]string{"FLUTTER_ROOT": root}
	}
	return nil
}
func (p officialSDK) DownloadURL(v string, t Target) (string, error) {
	a, err := p.Artifact(v, t)
	return a.URL, err
}
func (p officialSDK) LatestStable() (string, error) {
	vs, err := p.ListAll()
	if err != nil {
		return "", err
	}
	if len(vs) == 0 {
		return "", fmt.Errorf("%s 无稳定版本", p.name)
	}
	return vs[0], nil
}
func (p officialSDK) ListAll() ([]string, error) {
	versions := map[string]bool{}
	switch p.name {
	case "dart":
		// The official current stable feed avoids confusing source tags with SDK builds.
		var latest struct {
			Version string `json:"version"`
		}
		if err := fetchJSONOfficial("https://storage.googleapis.com/dart-archive/channels/stable/release/latest/VERSION", &latest); err != nil {
			return nil, err
		}
		versions[latest.Version] = true
	case "julia":
		data, err := juliaVersions()
		if err != nil {
			return nil, err
		}
		for v, r := range data {
			if r.Stable {
				versions[v] = true
			}
		}
	case "flutter":
		data, err := flutterVersions(CurrentTarget().OS)
		if err != nil {
			return nil, err
		}
		for _, r := range data.Releases {
			if r.Channel == "stable" {
				versions[r.Version] = true
			}
		}
	}
	var out []string
	for v := range versions {
		if sdkVersion.MatchString(v) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareVer(out[i], out[j]) > 0 })
	return out, nil
}

type juliaRelease struct {
	Stable bool `json:"stable"`
	Files  []struct {
		URL       string `json:"url"`
		Triplet   string `json:"triplet"`
		SHA256    string `json:"sha256"`
		Size      int64  `json:"size"`
		OS        string `json:"os"`
		Arch      string `json:"arch"`
		Extension string `json:"extension"`
	} `json:"files"`
}

func juliaVersions() (map[string]juliaRelease, error) {
	data := map[string]juliaRelease{}
	err := fetchJSONOfficial("https://julialang-s3.julialang.org/bin/versions.json", &data)
	return data, err
}

type flutterRelease struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
	Arch    string `json:"dart_sdk_arch"`
	Archive string `json:"archive"`
	SHA256  string `json:"sha256"`
}
type flutterIndex struct {
	Releases []flutterRelease `json:"releases"`
}

func flutterVersions(osName string) (flutterIndex, error) {
	var data flutterIndex
	platform := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[osName]
	if platform == "" {
		return data, fmt.Errorf("Flutter 不支持 %s", osName)
	}
	err := fetchJSONOfficial("https://storage.googleapis.com/flutter_infra_release/releases/releases_"+platform+".json", &data)
	return data, err
}
func (p officialSDK) Artifact(v string, t Target) (Artifact, error) {
	if !sdkVersion.MatchString(v) {
		return Artifact{}, fmt.Errorf("%s 需要完整稳定版本号", p.name)
	}
	if t.Arch != "amd64" && t.Arch != "arm64" {
		return Artifact{}, fmt.Errorf("不支持架构 %s", t.Arch)
	}
	switch p.name {
	case "dart":
		osName := map[string]string{"windows": "windows", "linux": "linux", "darwin": "macos"}[t.OS]
		arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[t.Arch]
		if osName == "" {
			return Artifact{}, fmt.Errorf("Dart 不支持 %s", t.OS)
		}
		raw := fmt.Sprintf("https://storage.googleapis.com/dart-archive/channels/stable/release/%s/sdk/dartsdk-%s-%s-release.zip", v, osName, arch)
		data, err := fetchOfficial(raw + ".sha256sum")
		if err != nil {
			return Artifact{}, err
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 || !regexp.MustCompile(`^[0-9a-fA-F]{64}$`).MatchString(fields[0]) {
			return Artifact{}, fmt.Errorf("Dart 官方 SHA256 无效")
		}
		return Artifact{Version: v, URL: raw, SHA256: strings.ToLower(fields[0]), Format: "zip"}, nil
	case "julia":
		data, err := juliaVersions()
		if err != nil {
			return Artifact{}, err
		}
		r := data[v]
		osName := map[string]string{"windows": "winnt", "linux": "linux", "darwin": "mac"}[t.OS]
		arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[t.Arch]
		ext := "tar.gz"
		if t.OS == "windows" {
			ext = "zip"
		}
		for _, f := range r.Files {
			if f.OS != osName || f.Arch != arch || f.Extension != ext || strings.Contains(f.Triplet, "musl") {
				continue
			}
			u, err := url.Parse(f.URL)
			if err != nil || u.Scheme != "https" || u.Host != "julialang-s3.julialang.org" {
				return Artifact{}, fmt.Errorf("Julia 官方地址无效")
			}
			return Artifact{Version: v, URL: f.URL, SHA256: f.SHA256, Size: f.Size, Format: ext}, nil
		}
	case "flutter":
		data, err := flutterVersions(t.OS)
		if err != nil {
			return Artifact{}, err
		}
		arch := "x64"
		if t.Arch == "arm64" {
			arch = "arm64"
		}
		for _, r := range data.Releases {
			if r.Version != v || r.Channel != "stable" || r.Arch != "" && r.Arch != arch || r.Arch == "" && arch != "x64" {
				continue
			}
			if !filepath.IsLocal(r.Archive) || strings.ContainsAny(r.Archive, "\\:?#") {
				return Artifact{}, fmt.Errorf("Flutter 官方归档路径无效")
			}
			format := assetFormat(r.Archive)
			if format == "binary" {
				continue
			}
			return Artifact{Version: v, URL: "https://storage.googleapis.com/flutter_infra_release/flutter/" + r.Archive, SHA256: r.SHA256, Format: format}, nil
		}
	}
	return Artifact{}, fmt.Errorf("%s %s 没有 %s/%s 的可用归档；可纳管官方本机安装", p.name, v, t.OS, t.Arch)
}
