package plugin

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func MetadataKind(tool string) string {
	if p, ok := Get(tool); ok {
		if m, ok := p.(manifestPlugin); ok {
			return m.m.VersionMetadata
		}
	}
	return ""
}

func boundedSDKFile(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("SDK metadata exceeds 1 MiB")
	}
	return data, nil
}

var metadataSDKVersion = regexp.MustCompile(`^\d+(?:\.\d+){1,3}$`)

// ReadSDKMetadata reads only known SDK metadata. It never starts an application,
// loads a project, checks out a license, or evaluates a configuration script.
func ReadSDKMetadata(tool, executable string) (root, version string, err error) {
	kind := MetadataKind(tool)
	if kind == "" {
		return "", "", fmt.Errorf("%s does not define SDK version metadata", tool)
	}
	full, err := filepath.Abs(executable)
	if err != nil {
		return "", "", err
	}
	if real, e := filepath.EvalSymlinks(full); e == nil {
		full = real
	}
	dir := filepath.Dir(full)
	for i := 0; i < 7; i++ {
		switch kind {
		case "matlab":
			if data, e := boundedSDKFile(filepath.Join(dir, "VersionInfo.xml")); e == nil {
				var info struct {
					XMLName xml.Name
					Version string `xml:"version"`
				}
				if xml.Unmarshal(data, &info) == nil && info.XMLName.Local == "MathWorks_version_info" && metadataSDKVersion.MatchString(strings.TrimSpace(info.Version)) {
					return dir, strings.TrimSpace(info.Version), nil
				}
			}
		case "netlogo":
			for _, sub := range []string{"app", "lib/app", "Contents/app", "."} {
				base := filepath.Join(dir, filepath.FromSlash(sub))
				for _, name := range []string{"NetLogo.cfg", "NetLogo_Console.cfg"} {
					if data, e := boundedSDKFile(filepath.Join(base, name)); e == nil {
						section := ""
						for _, line := range strings.Split(string(data), "\n") {
							line = strings.TrimSpace(line)
							if strings.HasPrefix(line, "[") {
								section = line
							}
							key, value, ok := strings.Cut(line, "=")
							if ok && section == "[Application]" && strings.TrimSpace(key) == "app.version" && metadataSDKVersion.MatchString(strings.TrimSpace(value)) {
								return dir, strings.TrimSpace(value), nil
							}
						}
					}
				}
				entries, _ := os.ReadDir(base)
				for j, entry := range entries {
					if j >= 128 {
						break
					}
					name := strings.ToLower(entry.Name())
					if entry.IsDir() || !strings.HasPrefix(name, "netlogo") || !strings.HasSuffix(name, ".jar") {
						continue
					}
					if v := netlogoJarVersion(filepath.Join(base, entry.Name())); v != "" {
						return dir, v, nil
					}
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", "", fmt.Errorf("未找到有效的 %s SDK 版本元数据；请选择完整安装目录，不会自动启动宿主读取版本", tool)
}

func netlogoJarVersion(file string) string {
	z, err := zip.OpenReader(file)
	if err != nil {
		return ""
	}
	defer z.Close()
	for _, entry := range z.File {
		if entry.Name != "META-INF/MANIFEST.MF" || entry.UncompressedSize64 > 64<<10 {
			continue
		}
		r, err := entry.Open()
		if err != nil {
			return ""
		}
		data, err := io.ReadAll(io.LimitReader(r, (64<<10)+1))
		r.Close()
		if err != nil || len(data) > 64<<10 {
			return ""
		}
		title, version := "", ""
		for _, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok {
				continue
			}
			switch key {
			case "Implementation-Title":
				title = strings.TrimSpace(value)
			case "Implementation-Version":
				version = strings.TrimSpace(value)
			}
		}
		if strings.EqualFold(title, "NetLogo") && metadataSDKVersion.MatchString(version) {
			return version
		}
	}
	return ""
}

func eiffelPlatform(t Target) string {
	return map[string]string{"windows/amd64": "win64", "windows/arm64": "win64", "linux/amd64": "linux-x86-64", "linux/arm64": "linux-armv8", "darwin/amd64": "macosx-x86-64", "darwin/arm64": "macosx-armv8"}[t.OS+"/"+t.Arch]
}
func eiffelBinPaths(root string, t Target) []string {
	return []string{filepath.Join(root, "studio", "spec", eiffelPlatform(t), "bin")}
}

func SDKInstallRoot(tool, executable string) string {
	if MetadataKind(tool) != "" {
		root, _, err := ReadSDKMetadata(tool, executable)
		if err == nil {
			return root
		}
		return ""
	}
	if tool != "eiffel" && tool != "pharo" && tool != "squeak" {
		return ""
	}
	full, err := filepath.Abs(executable)
	if err != nil {
		return ""
	}
	if real, e := filepath.EvalSymlinks(full); e == nil {
		full = real
	}
	bin := filepath.Dir(full)
	root := bin
	for i := 0; i < 7; i++ {
		if tool == "pharo" || tool == "squeak" {
			if strings.HasSuffix(strings.ToLower(root), ".app") {
				if info, e := os.Stat(filepath.Join(root, "Contents", "MacOS")); e == nil {
					if wanted, e := os.Stat(bin); e == nil && os.SameFile(info, wanted) {
						return root
					}
				}
			}
		} else {
			for _, candidate := range eiffelBinPaths(root, CurrentTarget()) {
				if info, e := os.Stat(candidate); e == nil {
					if wanted, e := os.Stat(bin); e == nil && os.SameFile(info, wanted) {
						return root
					}
				}
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return ""
}
