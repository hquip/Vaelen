package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func init() { register(&javaPlugin{}) }

// javaPlugin 管理 Eclipse Temurin (Adoptium) JDK。
// Resolve immutable URL, size and SHA256 from official metadata before mirroring.
// Java 版本以大版本号表示（如 17 / 21）。
type javaPlugin struct{}

type adoptiumReleases struct {
	AvailableReleases []int `json:"available_releases"`
	MostRecentLTS     int   `json:"most_recent_lts"`
}

func (j *javaPlugin) Name() string { return "java" }

func (j *javaPlugin) fetchReleases() (adoptiumReleases, error) {
	var r adoptiumReleases
	err := fetchJSON("https://api.adoptium.net/v3/info/available_releases", &r)
	return r, err
}

func (j *javaPlugin) ListAll() ([]string, error) {
	r, err := j.fetchReleases()
	if err != nil {
		return nil, err
	}
	majors := append([]int(nil), r.AvailableReleases...)
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))
	out := make([]string, 0, len(majors))
	for _, v := range majors {
		out = append(out, fmt.Sprintf("%d", v))
	}
	return out, nil
}

func (j *javaPlugin) LatestStable() (string, error) {
	r, err := j.fetchReleases()
	if err != nil {
		return "", err
	}
	if r.MostRecentLTS == 0 {
		return "", fmt.Errorf("未获取到 Java LTS 版本")
	}
	return fmt.Sprintf("%d", r.MostRecentLTS), nil
}

func (j *javaPlugin) DownloadURL(version string, t Target) (string, error) {
	a, err := j.Artifact(version, t)
	return a.URL, err
}
func (j *javaPlugin) Checksum(version string, t Target) (string, error) {
	a, err := j.Artifact(version, t)
	return a.SHA256, err
}
func (j *javaPlugin) Artifact(version string, t Target) (Artifact, error) {
	major, err := strconv.Atoi(version)
	if err != nil || major <= 0 || major > 9999 || strconv.Itoa(major) != version {
		return Artifact{}, fmt.Errorf("Java 安装版本需要大版本号，如 17 或 21")
	}
	osName := map[string]string{"windows": "windows", "linux": "linux", "darwin": "mac"}[t.OS]
	if osName == "" {
		return Artifact{}, fmt.Errorf("java 不支持的系统: %s", t.OS)
	}
	archName := map[string]string{"amd64": "x64", "arm64": "aarch64", "386": "x86"}[t.Arch]
	if archName == "" {
		return Artifact{}, fmt.Errorf("java 不支持的架构: %s", t.Arch)
	}
	query := url.Values{"architecture": {archName}, "heap_size": {"normal"}, "jvm_impl": {"hotspot"}, "image_type": {"jdk"}, "os": {osName}, "project": {"jdk"}, "vendor": {"eclipse"}, "page": {"0"}, "page_size": {"1"}, "sort_order": {"DESC"}}
	raw := fmt.Sprintf("https://api.adoptium.net/v3/assets/feature_releases/%d/ga?%s", major, query.Encode())
	var releases []struct {
		ReleaseType string `json:"release_type"`
		Binaries    []struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
			ImageType    string `json:"image_type"`
			JVMImpl      string `json:"jvm_impl"`
			HeapSize     string `json:"heap_size"`
			Package      struct {
				Name     string `json:"name"`
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
				Size     int64  `json:"size"`
			} `json:"package"`
		} `json:"binaries"`
	}
	// Integrity metadata always comes from the original Adoptium API.
	if err := fetchJSONWithClient(httpClient, raw, &releases); err != nil {
		return Artifact{}, err
	}
	var selected Artifact
	for _, release := range releases {
		if release.ReleaseType != "ga" {
			continue
		}
		for _, b := range release.Binaries {
			if b.Architecture != archName || b.OS != osName || b.ImageType != "jdk" || b.JVMImpl != "hotspot" || b.HeapSize != "normal" {
				continue
			}
			p := b.Package
			rel, ok := temurinMirrorPath(p.Link)
			wantPrefix := fmt.Sprintf("%d/jdk/%s/%s/", major, archName, osName)
			if !ok || rel != wantPrefix+p.Name {
				return Artifact{}, fmt.Errorf("官方 Java 元数据返回了不匹配的包地址")
			}
			digest, err := hex.DecodeString(p.Checksum)
			if err != nil || len(digest) != sha256.Size || p.Size <= 0 || p.Size > 8<<30 {
				return Artifact{}, fmt.Errorf("官方 Java 元数据缺少有效 SHA256 或包大小")
			}
			a := Artifact{URL: p.Link, SHA256: strings.ToLower(p.Checksum), Size: p.Size}
			if selected.URL != "" && selected != a {
				return Artifact{}, fmt.Errorf("官方 Java 元数据包含多个不一致的安装包")
			}
			selected = a
		}
	}
	if selected.URL == "" {
		return Artifact{}, fmt.Errorf("没有适用于 %s/%s 的 Java %s JDK 包", t.OS, t.Arch, version)
	}
	return selected, nil
}

func (j *javaPlugin) BinPaths(installDir string) []string {
	return []string{filepath.Join(installDir, "bin"), filepath.Join(installDir, "Contents", "Home", "bin")}
}

func (j *javaPlugin) ExecEnv(installDir string) map[string]string {
	home := filepath.Join(installDir, "Contents", "Home")
	if fi, err := os.Stat(home); err == nil && fi.IsDir() {
		return map[string]string{"JAVA_HOME": home}
	}
	return map[string]string{"JAVA_HOME": installDir}
}
