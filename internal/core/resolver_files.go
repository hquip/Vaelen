package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// versionFile 描述一个“生态惯用的版本声明文件”及其解析方式。
type versionFile struct {
	file  string
	parse func(content string) string // 返回版本要求字符串；无法解析返回 ""
}

// versionFileSources 列出每个工具会读取的惯用版本文件（按优先级，靠前的先用）。
// 这样进入只有 .nvmrc / go.mod / .python-version 的现成项目，无需写 .tool-versions
// 也能自动识别出它需要的版本。
var versionFileSources = map[string][]versionFile{
	"node": {
		{".nvmrc", plainVer},
		{".node-version", plainVer},
		{"package.json", pkgJSONNode},
	},
	"python": {
		{".python-version", plainVer},
		{"pyproject.toml", pyRequires},
		{"runtime.txt", runtimeTxt},
	},
	"ruby": {
		{".ruby-version", plainVer},
		{"Gemfile", gemfileRuby},
	},
	"java": {
		{".java-version", plainVer},
		{".sdkmanrc", sdkmanJava},
	},
	"go": {
		{".go-version", plainVer},
		{"go.mod", goModVer},
	},
}

// projectVersion 在 dir 下按该工具的惯用文件查找版本要求，返回首个命中的 spec 与来源路径。
func projectVersion(tool, dir string) (spec, source string, ok bool) {
	for _, src := range versionFileSources[tool] {
		path := filepath.Join(dir, src.file)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if v := src.parse(string(data)); v != "" {
			return v, path, true
		}
	}
	return "", "", false
}

var verNumRe = regexp.MustCompile(`\d+(\.\d+){0,2}`)

// plainVer 解析“一行一个版本号”的简单文件（.nvmrc / .python-version 等）：
// 取首个非空非注释行、去掉前缀 v；遇到 lts/* / system 之类无法映射的返回 ""。
func plainVer(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "v")
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			line = line[:i]
		}
		if line != "" && line[0] >= '0' && line[0] <= '9' {
			return line
		}
		return ""
	}
	return ""
}

func goModVer(s string) string {
	re := regexp.MustCompile(`(?m)^\s*go\s+(\d+\.\d+(?:\.\d+)?)`)
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func pkgJSONNode(s string) string {
	re := regexp.MustCompile(`"engines"\s*:\s*\{[^}]*?"node"\s*:\s*"([^"]+)"`)
	if m := re.FindStringSubmatch(s); m != nil {
		return verNumRe.FindString(m[1])
	}
	return ""
}

func pyRequires(s string) string {
	re := regexp.MustCompile(`(?m)^\s*requires-python\s*=\s*["']([^"']+)["']`)
	if m := re.FindStringSubmatch(s); m != nil {
		return verNumRe.FindString(m[1])
	}
	return ""
}

func runtimeTxt(s string) string {
	re := regexp.MustCompile(`(?i)python-?(\d+\.\d+(?:\.\d+)?)`)
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func gemfileRuby(s string) string {
	re := regexp.MustCompile(`(?m)^\s*ruby\s+["']([^"']+)["']`)
	if m := re.FindStringSubmatch(s); m != nil {
		return verNumRe.FindString(m[1])
	}
	return ""
}

func sdkmanJava(s string) string {
	re := regexp.MustCompile(`(?m)^\s*java\s*=\s*(\d+(?:\.\d+){0,2})`)
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
