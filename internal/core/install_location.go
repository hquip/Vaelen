package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizeInstallBase accepts either the configured install root or its tool
// directory as the default destination. A custom base always receives /tool/version;
// nesting that base inside the registry would turn storage folders into versions.
func (p Paths) NormalizeInstallBase(tool, customDir string) (string, error) {
	if err := validateComponent(tool); err != nil {
		return "", err
	}
	customDir = strings.TrimSpace(customDir)
	if customDir == "" {
		return "", nil
	}
	base, err := canonicalPath(customDir)
	if err != nil {
		return "", err
	}
	installs, err := canonicalPath(p.Installs())
	if err != nil {
		return "", err
	}
	if sameDir(base, installs) || sameDir(base, filepath.Join(installs, tool)) {
		return "", nil
	}
	if pathWithin(installs, base) {
		return "", fmt.Errorf("安装位置不能嵌套在版本目录中；请选择安装根目录 %s、工具目录 %s，或其它安装父目录（自动创建 %s/<版本>）", p.Installs(), p.ToolInstalls(tool), tool)
	}
	return base, nil
}

// Only remove now-empty legacy storage parents, never files or other versions.
func removeEmptyInstallParents(target, toolDir string) {
	for parent := filepath.Dir(target); pathWithin(toolDir, parent) && !sameDir(parent, toolDir); parent = filepath.Dir(parent) {
		if err := os.Remove(parent); err != nil {
			return
		}
	}
}
