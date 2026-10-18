package plugin

import (
	"net/url"
	"regexp"
	"strings"
)

const javaChinaMirror = "https://mirrors.tuna.tsinghua.edu.cn/Adoptium/"

var temurinFilename = regexp.MustCompile(`^OpenJDK([0-9]+)U-jdk_(x64|aarch64|x86)_(windows|linux|mac)_hotspot_[A-Za-z0-9._+-]+\.(zip|tar\.gz)$`)

func temurinMirrorPath(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 6 || parts[0] != "adoptium" || parts[2] != "releases" || parts[3] != "download" {
		return "", false
	}
	m := temurinFilename.FindStringSubmatch(parts[5])
	if len(m) == 0 || parts[1] != "temurin"+m[1]+"-binaries" {
		return "", false
	}
	return m[1] + "/jdk/" + m[2] + "/" + m[3] + "/" + parts[5], true
}

// A mirror and its official fallback always use the same pinned digest.
func DownloadURLs(original string) []string {
	preferred := MirrorURL(original)
	if _, java := temurinMirrorPath(original); java && preferred != original {
		return []string{preferred, original}
	}
	return []string{preferred}
}
