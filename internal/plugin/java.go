package plugin

import (
	"fmt"
	"path/filepath"
	"sort"
)

func init() { register(&javaPlugin{}) }

// javaPlugin 管理 Eclipse Temurin (Adoptium) JDK。
// Adoptium 的 binary/latest 端点会 302 重定向到真实二进制（.zip/.tar.gz），
// 下载器会跟随重定向并按最终后缀识别格式。Java 版本以大版本号表示（如 17 / 21）。
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
	osName := map[string]string{"windows": "windows", "linux": "linux", "darwin": "mac"}[t.OS]
	if osName == "" {
		return "", fmt.Errorf("java 不支持的系统: %s", t.OS)
	}
	archName := map[string]string{"amd64": "x64", "arm64": "aarch64", "386": "x86"}[t.Arch]
	if archName == "" {
		return "", fmt.Errorf("java 不支持的架构: %s", t.Arch)
	}
	return fmt.Sprintf("https://api.adoptium.net/v3/binary/latest/%s/ga/%s/%s/jdk/hotspot/normal/eclipse",
		version, osName, archName), nil
}

func (j *javaPlugin) BinPaths(installDir string) []string {
	// Windows/Linux 为 <dir>/bin；macOS 实际为 <dir>/Contents/Home/bin（MVP 以 Windows 为主）。
	return []string{filepath.Join(installDir, "bin")}
}

func (j *javaPlugin) ExecEnv(installDir string) map[string]string {
	return map[string]string{"JAVA_HOME": installDir}
}
