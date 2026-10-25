package plugin

import (
	"sort"
	"strings"
)

type ToolSupport struct {
	DisplayName string   `json:"display_name"`
	Languages   []string `json:"languages"`
	Mode        string   `json:"mode"`
	Notes       string   `json:"notes"`
	Platforms   []string `json:"platforms"`
}

var nativeLanguages = map[string][]string{
	"go": {"Go", "Golang"}, "java": {"Java", "JVM"}, "python": {"Python"}, "node": {"JavaScript", "TypeScript", "Node.js"}, "bun": {"JavaScript", "TypeScript", "JSX", "TSX"}, "deno": {"JavaScript", "TypeScript", "JSX", "TSX"}, "zig": {"Zig", "C", "C++"},
	"dotnet": {"C#", "CSharp", "C Sharp", "F#", "FSharp", "Visual Basic .NET", "VB.NET", "VBNet"}, "dart": {"Dart"}, "flutter": {"Dart", "Flutter"}, "julia": {"Julia"},
}

func Support(name string, target Target) ToolSupport {
	out := ToolSupport{DisplayName: name, Languages: []string{}, Mode: "adopt", Platforms: []string{"windows", "linux", "darwin"}, Notes: "纳管并切换本机已有安装；尚无可直接安装的发行包"}
	p, ok := Get(name)
	if !ok {
		return out
	}
	if languages, ok := nativeLanguages[name]; ok {
		out.Languages = append([]string{}, languages...)
		out.Mode = "install"
		out.Notes = "使用发行方归档安装，或纳管已有安装"
	}
	if name == "dotnet" {
		out.DisplayName = ".NET SDK · C# / F# / VB.NET"
		out.Notes = "C#、F#、VB.NET 共用 SDK 版本；支持官方 SHA512 校验及 global.json 版本声明"
	}
	if name == "dart" {
		out.Notes = "远端列表显示当前稳定版；完整历史版本可直接输入版本号安装"
	}
	if name == "zig" {
		out.Notes = "Zig 内含 C/C++ 编译能力（zig cc / zig c++）；C/C++ 项目也可选择 GCC、Clang 或 MSVC"
	}
	if m, ok := p.(manifestPlugin); ok {
		if m.m.DisplayName != "" {
			out.DisplayName = m.m.DisplayName
		}
		out.Languages = append([]string{}, m.m.Languages...)
		if m.m.Notes != "" {
			out.Notes = m.m.Notes
		}
		if len(m.m.Platforms) > 0 {
			out.Platforms = append([]string{}, m.m.Platforms...)
		}
		if len(m.m.Assets) > 0 {
			out.Mode = "install"
			if m.m.Assets[target.OS+"/"+target.Arch] == "" {
				out.Mode = "adopt"
				out.Notes = "当前平台暂无自动安装归档；可纳管已有安装。" + out.Notes
			}
		} else if m.m.Source.Type == "github-auto" {
			out.Mode = "release"
			out.Notes = "按平台匹配 GitHub 发行包，须有发布方摘要；实际可用性以版本资产为准。" + out.Notes
		}
		if m.m.Service {
			out.Mode = "service"
			if len(out.Languages) == 0 {
				out.Languages = []string{"SQL"}
				if name == "redis" || name == "valkey" {
					out.Languages = []string{"Redis protocol"}
				}
			}
		}
	}
	found := false
	for _, osName := range out.Platforms {
		if osName == target.OS {
			found = true
		}
	}
	if !found {
		out.Mode = "unavailable"
		out.Notes = "当前平台不支持该工具链。" + out.Notes
	}
	return out
}

func LanguageNames() []string {
	seen := map[string]bool{}
	for _, name := range Names() {
		for _, language := range Support(name, CurrentTarget()).Languages {
			seen[language] = true
		}
	}
	var out []string
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func MatchesTool(name, query string) bool {
	if query == "" {
		return true
	}
	s := Support(name, CurrentTarget())
	values := append([]string{name, s.DisplayName}, s.Languages...)
	q := strings.ToLower(strings.TrimSpace(query))
	for _, value := range values {
		if len(q) == 1 && strings.EqualFold(value, q) || len(q) > 1 && strings.Contains(strings.ToLower(value), q) {
			return true
		}
	}
	return false
}

func VersionProbe(tool string) (flag, pattern string) {
	if p, ok := Get(tool); ok {
		if m, ok := p.(manifestPlugin); ok {
			return m.m.VersionFlag, m.m.VersionPattern
		}
	}
	return "", ""
}
