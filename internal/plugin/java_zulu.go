package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func zuluArtifact(version string, target Target) (Artifact, error) {
	if !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*(?:\+[0-9]+)?$`).MatchString(version) {
		return Artifact{}, fmt.Errorf("Zulu 版本格式无效")
	}
	platform := map[string]string{"windows": "windows", "darwin": "macos", "linux": "linux"}[target.OS]
	arch := map[string]string{"amd64": "x86", "arm64": "arm"}[target.Arch]
	if platform == "" || arch == "" {
		return Artifact{}, fmt.Errorf("Zulu 不支持此平台")
	}
	javaVersion, build, _ := strings.Cut(version, "+")
	query := url.Values{"java_version": {javaVersion}, "os": {platform}, "arch": {arch}, "hw_bitness": {"64"}, "archive_type": {strings.TrimPrefix(extFor(target.OS), ".")}, "java_package_type": {"jdk"}, "javafx_bundled": {"false"}, "release_status": {"ga"}, "availability_types": {"CA"}}
	if !strings.Contains(javaVersion, ".") {
		query.Set("latest", "true")
	}
	var packages []struct {
		ID      string `json:"package_uuid"`
		Name    string `json:"name"`
		Version []int  `json:"java_version"`
		Distro  []int  `json:"distro_version"`
		Build   int    `json:"openjdk_build_number"`
	}
	if err := fetchJSONWithClient(httpClient, "https://api.azul.com/metadata/v1/zulu/packages/?"+query.Encode(), &packages); err != nil {
		return Artifact{}, err
	}
	var selectedID, selectedVersion string
	var selectedDistro []int
	for _, p := range packages {
		if strings.Contains(p.Name, "-crac-") || strings.Contains(p.Name, "-fx-") || strings.Contains(p.Name, "-musl-") {
			continue
		}
		if !regexp.MustCompile(`^[a-f0-9-]{36}$`).MatchString(p.ID) || len(p.Version) == 0 {
			continue
		}
		parts := []string{}
		for _, n := range p.Version {
			parts = append(parts, strconv.Itoa(n))
		}
		resolved := strings.Join(parts, ".")
		if strings.Contains(javaVersion, ".") && resolved != javaVersion {
			continue
		}
		if build != "" && strconv.Itoa(p.Build) != build {
			continue
		}
		newer := selectedID == ""
		for i := 0; i < len(p.Distro) && i < len(selectedDistro); i++ {
			if p.Distro[i] != selectedDistro[i] {
				newer = p.Distro[i] > selectedDistro[i]
				break
			}
		}
		if newer {
			selectedID = p.ID
			selectedVersion = "zulu-" + resolved + "+" + strconv.Itoa(p.Build)
			selectedDistro = p.Distro
		}
	}
	if selectedID == "" {
		return Artifact{}, fmt.Errorf("没有匹配的 Zulu GA JDK 包")
	}
	var detail struct {
		URL    string `json:"download_url"`
		SHA    string `json:"sha256_hash"`
		Size   int64  `json:"size"`
		OS     string `json:"os"`
		Arch   string `json:"arch"`
		Bits   int    `json:"hw_bitness"`
		Status string `json:"release_status"`
		Type   string `json:"java_package_type"`
		ID     string `json:"package_uuid"`
	}
	if err := fetchJSONWithClient(httpClient, "https://api.azul.com/metadata/v1/zulu/packages/"+selectedID, &detail); err != nil {
		return Artifact{}, err
	}
	u, err := url.Parse(detail.URL)
	if err != nil || u.Scheme != "https" || u.Host != "cdn.azul.com" || !strings.HasPrefix(u.Path, "/zulu/bin/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return Artifact{}, fmt.Errorf("Zulu 返回了非官方包地址")
	}
	digest, err := hex.DecodeString(detail.SHA)
	if err != nil || len(digest) != sha256.Size || detail.Size <= 0 || detail.Size > 8<<30 || detail.OS != platform || detail.Arch != arch || detail.Bits != 64 || detail.Status != "ga" || detail.Type != "jdk" || detail.ID != selectedID {
		return Artifact{}, fmt.Errorf("Zulu 包身份、平台或摘要不匹配")
	}
	return Artifact{Version: selectedVersion, URL: detail.URL, SHA256: strings.ToLower(detail.SHA), Size: detail.Size}, nil
}
