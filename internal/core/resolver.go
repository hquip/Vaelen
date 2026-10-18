package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vsm/internal/config"
)

// Resolver 按优先级解析某工具当前应使用的版本。
//
// 优先级（高→低）：
//  1. 环境变量 VSM_<TOOL>_VERSION
//  2. 当前目录向上逐级查找的 .tool-versions
//  3. 全局 .tool-versions（~/.vsm/.tool-versions）
type Resolver struct {
	Paths    Paths
	Registry Registry
	Pinned   map[string]string
	// Boundary limits project discovery without changing process-wide environment.
	Boundary string
}

func (r Resolver) boundary() string {
	if r.Boundary != "" {
		return r.Boundary
	}
	return os.Getenv("VSM_PROJECT_BOUNDARY")
}

// Resolution 描述一次解析结果。
type Resolution struct {
	Tool    string
	Spec    string // 配置中写的版本要求，如 "20" / "1.22.0" / "latest"
	Version string // 匹配到的已安装具体版本；为空表示未装或未配置
	Source  string // 版本要求的来源描述
	Error   string // Invalid configuration/registry; never silently fall back.
}

// Resolve 解析单个工具当前应使用的版本。startDir 通常是当前工作目录。
func (r Resolver) Resolve(tool, startDir string) Resolution {
	if version := r.Pinned[tool]; version != "" {
		return r.match(tool, version, "vsm.lock")
	}
	envKey := "VSM_" + strings.ToUpper(tool) + "_VERSION"
	if v := os.Getenv(envKey); v != "" && r.Paths.ProjectMode != "local" && r.Paths.ProjectMode != "docker" {
		return r.match(tool, v, "环境变量 "+envKey)
	}
	spec, src, err := findToolVersionBounded(tool, startDir, r.boundary())
	if err != nil {
		return Resolution{Tool: tool, Error: err.Error()}
	}
	if src != "" {
		return r.match(tool, spec, src)
	}
	if r.Paths.ProjectMode == "local" || r.Paths.ProjectMode == "docker" {
		return Resolution{Tool: tool}
	}
	if tv, err := config.Load(r.Paths.GlobalToolVersions()); err == nil {
		if spec, ok := tv.Get(tool); ok {
			return r.match(tool, spec, "全局")
		}
	} else if !os.IsNotExist(err) {
		return Resolution{Tool: tool, Error: err.Error()}
	}
	return Resolution{Tool: tool}
}

func (r Resolver) match(tool, spec, source string) Resolution {
	installed, err := r.Registry.Installed(tool)
	if err != nil {
		return Resolution{Tool: tool, Spec: spec, Source: source, Error: err.Error()}
	}
	return Resolution{
		Tool:    tool,
		Spec:    spec,
		Version: BestMatch(spec, installed),
		Source:  source,
	}
}

func findToolVersion(tool, startDir string) (spec, source string, ok bool) {
	spec, source, err := findToolVersionChecked(tool, startDir)
	return spec, source, source != "" && err == nil
}

func findToolVersionChecked(tool, startDir string) (spec, source string, err error) {
	return findToolVersionBounded(tool, startDir, os.Getenv("VSM_PROJECT_BOUNDARY"))
}

func findToolVersionBounded(tool, startDir, boundary string) (spec, source string, err error) {
	project, err := findProjectBounded(startDir, boundary)
	if err != nil {
		return "", "", err
	}
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", err
	}
	for {
		// Explicit workspace configuration wins over ecosystem version files.
		workspacePath := filepath.Join(dir, config.WorkspaceName)
		if workspace, err := config.LoadWorkspace(workspacePath); err == nil {
			if spec, ok := workspace.Tools[tool]; ok {
				return spec, workspacePath, nil
			}
		} else if !os.IsNotExist(err) {
			return "", "", err
		}
		// 同一目录内：.tool-versions 的显式声明优先，其次各生态惯用版本文件
		// （.nvmrc / go.mod / .python-version 等）。越靠近当前目录的越优先。
		path := filepath.Join(dir, ".tool-versions")
		if tv, err := config.Load(path); err == nil {
			if spec, has := tv.Get(tool); has {
				return spec, path, nil
			}
			if warnings := tv.Warnings(); len(warnings) > 0 {
				return "", "", fmt.Errorf("invalid version declaration in %s: %s", path, strings.Join(warnings, ", "))
			}
		} else if !os.IsNotExist(err) {
			return "", "", fmt.Errorf("read %s: %w", path, err)
		}
		spec, src, err := projectVersionChecked(tool, dir)
		if err != nil {
			return "", "", err
		}
		if src != "" {
			return spec, src, nil
		}
		parent := filepath.Dir(dir)
		if project != nil && sameDir(project.Root, dir) {
			return "", "", nil
		}
		if boundary != "" && sameDir(boundary, dir) {
			return "", "", nil
		}
		if parent == dir {
			return "", "", nil
		}
		dir = parent
	}
}

// BestMatch 把版本要求匹配到已安装/可选版本列表中的具体版本。
// 规则：精确匹配优先；否则按点分段前缀匹配（"20" 命中 "20.11.0"）取最高；
// spec 为 "latest" 或空时取最高版本。
func BestMatch(spec string, candidatesAll []string) string {
	if len(candidatesAll) == 0 {
		return ""
	}
	for _, v := range candidatesAll { // 精确匹配优先
		if v == spec {
			return v
		}
	}
	var candidates []string
	for _, v := range candidatesAll {
		if versionMatches(spec, v) {
			candidates = append(candidates, v)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	for _, v := range candidates[1:] {
		if CompareVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}

// versionHasPrefix 判断 version 是否以 prefix 为版本前缀（按点分段对齐）。
func versionHasPrefix(version, prefix string) bool {
	version, _, _ = strings.Cut(strings.TrimPrefix(version, "v"), "+")
	prefix, _, _ = strings.Cut(strings.TrimPrefix(prefix, "v"), "+")
	vp := strings.Split(version, ".")
	pp := strings.Split(prefix, ".")
	if len(pp) > len(vp) {
		return false
	}
	for i := range pp {
		if vp[i] != pp[i] {
			return false
		}
	}
	return true
}

// atoiSafe 取字符串前导数字，遇到非数字即停（容忍 "0-rc1" 之类后缀）。
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
