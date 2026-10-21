package core

import (
	"fmt"
	"strings"
)

// VersionUnavailableError distinguishes a saved requirement with no matching
// installation from a runtime that was found but failed to execute.
type VersionUnavailableError struct {
	Tool, Spec, Source, InstallRoot string
	Installed                       []string
}

func (e *VersionUnavailableError) Error() string {
	if e.Spec == "" {
		return fmt.Sprintf("%s 未找到可用版本：尚未配置版本；当前安装目录：%s", e.Tool, e.InstallRoot)
	}
	available := "无"
	if len(e.Installed) > 0 {
		available = strings.Join(e.Installed[:min(len(e.Installed), 8)], "、")
	}
	return fmt.Sprintf("%s 未找到可用版本：要求 %s（来源：%s）；当前安装目录：%s；该目录可见版本：%s", e.Tool, e.Spec, e.Source, e.InstallRoot, available)
}

func (a Activator) unavailableVersion(tool string, res Resolution) *VersionUnavailableError {
	installed, _ := a.Registry.Installed(tool)
	return &VersionUnavailableError{Tool: tool, Spec: res.Spec, Source: res.Source, InstallRoot: a.Paths.Installs(), Installed: installed}
}
