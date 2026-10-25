package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"vsm/internal/plugin"
)

// InspectToolPath validates a user-selected executable or installation directory.
// It never guesses a language from an unrelated executable's numeric output.
func InspectToolPath(tool, path string) (SystemVersion, error) {
	return InspectToolPathLabeled(tool, path, "")
}

func InspectToolPathLabeled(tool, path, label string) (SystemVersion, error) {
	p, ok := plugin.Get(tool)
	if !ok {
		return SystemVersion{}, fmt.Errorf("不支持的工具: %s", tool)
	}
	manual := plugin.VersionMode(tool) == "label"
	if manual {
		if label == "" {
			return SystemVersion{}, fmt.Errorf("%s 需要手动提供版本标识；仅检查命令路径，不自动读取软件版本", tool)
		}
		if err := validateInstall(tool, label); err != nil {
			return SystemVersion{}, err
		}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return SystemVersion{}, fmt.Errorf("请选择安装目录或可执行文件")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return SystemVersion{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return SystemVersion{}, err
	}
	names := plugin.ProbeCommands()[tool]
	if tool == "j" || manual {
		names = plugin.DetectCommands()[tool]
	}
	if len(names) == 0 {
		names = []string{tool}
	}
	bin := ""
	if info.IsDir() {
		dirs := append([]string{abs, filepath.Join(abs, "bin")}, p.BinPaths(abs)...)
		for _, name := range names {
			if bin = findExecutable(dirs, name); bin != "" {
				break
			}
		}
	} else if info.Mode().IsRegular() && info.Size() > 0 {
		for _, name := range names {
			for _, ext := range executableExts() {
				if filepath.Base(abs) == name+ext || strings.EqualFold(filepath.Base(abs), name+ext) && os.PathSeparator == '\\' {
					bin = abs
				}
			}
		}
	}
	if info.Mode().IsRegular() && info.Size() > 0 && tool == "python" && regexp.MustCompile(`^python[0-9]+(?:\.[0-9]+)*(?:\.exe)?$`).MatchString(filepath.Base(abs)) {
		bin = abs
	}
	if bin == "" {
		return SystemVersion{}, fmt.Errorf("路径中找不到 %s 的主命令（%s）", tool, strings.Join(names, " / "))
	}
	if manual {
		result := describeRuntime(tool, filepath.Base(bin), bin, label)
		result.VersionOrigin = "label"
		return result, nil
	}
	version, err := probeVersion(tool, bin)
	if err != nil || version == "" {
		return SystemVersion{}, fmt.Errorf("无法读取 %s 版本: %v", tool, err)
	}
	return describeRuntime(tool, filepath.Base(bin), bin, version), nil
}
