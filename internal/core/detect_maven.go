package core

import (
	"archive/zip"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

var mavenMetadataVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)

// The core jar describes Maven itself, independently of JAVA_HOME, Maven rc
// scripts or the current project's .mvn configuration. Never parse a jar name.
func probeMavenMetadata(bin string) (string, error) {
	bin = realPath(bin)
	root := filepath.Dir(bin)
	if strings.EqualFold(filepath.Base(root), "bin") {
		root = filepath.Dir(root)
	}
	jars, err := filepath.Glob(filepath.Join(root, "lib", "maven-core-*.jar"))
	if err != nil {
		return "", err
	}
	version := ""
	for i, jar := range jars {
		if i >= 8 {
			return "", fmt.Errorf("Maven core jars 超过限制")
		}
		z, err := zip.OpenReader(jar)
		if err != nil {
			continue
		}
		for _, file := range z.File {
			if file.Name != "META-INF/maven/org.apache.maven/maven-core/pom.properties" || file.UncompressedSize64 > 16<<10 {
				continue
			}
			r, err := file.Open()
			if err != nil {
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(r, 16<<10+1))
			r.Close()
			if readErr != nil || len(data) > 16<<10 {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
				value = strings.TrimSpace(value)
				if !ok || strings.TrimSpace(key) != "version" || !mavenMetadataVersion.MatchString(value) {
					continue
				}
				if version != "" && version != value {
					z.Close()
					return "", fmt.Errorf("安装中存在多个 Maven core 版本")
				}
				version = value
			}
		}
		z.Close()
	}
	if version == "" {
		return "", fmt.Errorf("未找到有效 Maven core 版本元数据")
	}
	return version, nil
}
