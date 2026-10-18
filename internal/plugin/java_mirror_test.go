package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemurinMirrorPathAndDownloadURLs(t *testing.T) {
	raw := "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_windows_hotspot_21.0.12.1_1.zip"
	path, ok := temurinMirrorPath(raw)
	if !ok || path != "21/jdk/x64/windows/OpenJDK21U-jdk_x64_windows_hotspot_21.0.12.1_1.zip" {
		t.Fatalf("%q %v", path, ok)
	}
	t.Setenv("VSM_MIRROR", "cn")
	t.Setenv("VSM_HOME", t.TempDir())
	urls := DownloadURLs(raw)
	if len(urls) != 2 || urls[0] != javaChinaMirror+path || urls[1] != raw {
		t.Fatalf("fallback URLs: %#v", urls)
	}
	for _, bad := range []string{"https://github.com/adoptium/temurin21-binaries/releases/download/jdk/file.zip?token=x", "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21/OpenJDK22U-jdk_x64_windows_hotspot_21.zip"} {
		if _, ok := temurinMirrorPath(bad); ok {
			t.Fatalf("unsafe URL accepted: %s", bad)
		}
	}
	for _, raw := range []string{
		"https://github.com/adoptium/temurin8-binaries/releases/download/jdk8u442-b06/OpenJDK8U-jdk_x64_linux_hotspot_8u442b06.tar.gz",
		"https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17/OpenJDK17U-jdk_aarch64_mac_hotspot_17.0.16_8.tar.gz",
	} {
		if _, ok := temurinMirrorPath(raw); !ok {
			t.Fatalf("valid JDK layout rejected: %s", raw)
		}
	}
	t.Setenv("VSM_MIRROR", "off")
	if urls := DownloadURLs(raw); len(urls) != 1 || urls[0] != raw {
		t.Fatalf("off still mirrored Java: %v", urls)
	}
	t.Setenv("VSM_MIRROR", "custom")
	if err := os.WriteFile(filepath.Join(os.Getenv("VSM_HOME"), "mirror-sources.json"), []byte(`{"java":"https://java.example/Adoptium/","github":"https://proxy.example/"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := MirrorURL(raw); got != "https://java.example/Adoptium/"+path {
		t.Fatalf("dedicated source did not take precedence: %s", got)
	}
}
