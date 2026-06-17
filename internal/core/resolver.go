package core

import (
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
}

// Resolution 描述一次解析结果。
type Resolution struct {
	Tool    string
	Spec    string // 配置中写的版本要求，如 "20" / "1.22.0" / "latest"
	Version string // 匹配到的已安装具体版本；为空表示未装或未配置
	Source  string // 版本要求的来源描述
}

// Resolve 解析单个工具当前应使用的版本。startDir 通常是当前工作目录。
func (r Resolver) Resolve(tool, startDir string) Resolution {
	envKey := "VSM_" + strings.ToUpper(tool) + "_VERSION"
	if v := os.Getenv(envKey); v != "" {
		return r.match(tool, v, "环境变量 "+envKey)
	}
	if spec, src, ok := findToolVersion(tool, startDir); ok {
		return r.match(tool, spec, src)
	}
	if tv, err := config.Load(r.Paths.GlobalToolVersions()); err == nil {
		if spec, ok := tv.Get(tool); ok {
			return r.match(tool, spec, "全局")
		}
	}
	return Resolution{Tool: tool}
}

func (r Resolver) match(tool, spec, source string) Resolution {
	installed, _ := r.Registry.Installed(tool)
	return Resolution{
		Tool:    tool,
		Spec:    spec,
		Version: BestMatch(spec, installed),
		Source:  source,
	}
}

func findToolVersion(tool, startDir string) (spec, source string, ok bool) {
	dir := startDir
	for {
		// 同一目录内：.tool-versions 的显式声明优先，其次各生态惯用版本文件
		// （.nvmrc / go.mod / .python-version 等）。越靠近当前目录的越优先。
		path := filepath.Join(dir, ".tool-versions")
		if tv, err := config.Load(path); err == nil {
			if spec, has := tv.Get(tool); has {
				return spec, path, true
			}
		}
		if spec, src, has := projectVersion(tool, dir); has {
			return spec, src, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
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
	candidates := candidatesAll
	if spec != "latest" && spec != "" {
		var filtered []string
		for _, v := range candidatesAll {
			if versionHasPrefix(v, spec) {
				filtered = append(filtered, v)
			}
		}
		candidates = filtered
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

// CompareVersions 按点分段数值比较两个版本：a>b 返回 1，a<b 返回 -1，相等返回 0。
func CompareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			ai = atoiSafe(as[i])
		}
		if i < len(bs) {
			bi = atoiSafe(bs[i])
		}
		if ai != bi {
			if ai > bi {
				return 1
			}
			return -1
		}
	}
	return 0
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
