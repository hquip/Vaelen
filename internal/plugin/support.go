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
	"nim": {"Nim"}, "godot": {"GDScript", "Godot"}, "vyper": {"Vyper"},
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
	if name == "godot" {
		out.DisplayName = "Godot · GDScript"
		out.Notes = "官方标准版编辑器及无界面命令；不包含 .NET 版或导出模板，项目与插件兼容性由项目自行管理"
	}
	if name == "vyper" {
		out.DisplayName = "Vyper"
		out.Notes = "官方独立编译器及 SHA256 校验；当前自动安装适配 amd64，并验证实际二进制架构；合约部署与链上运行不在此范围"
		if target.Arch != "amd64" {
			out.Mode = "adopt"
		}
	}
	if name == "nim" {
		out.DisplayName = "Nim"
		out.Notes = "按 Nim 官网稳定发行表选择归档并验证 SHA256；编译 C/C++ 后端仍需已有系统编译器"
		if target.OS == "windows" && target.Arch != "amd64" {
			out.Mode = "adopt"
		}
	}
	if name == "zig" {
		out.Notes = "Zig 内含 C/C++ 编译能力（zig cc / zig c++）；C/C++ 项目也可选择 GCC、Clang 或 MSVC"
	}
	if m, ok := p.(manifestPlugin); ok {
		// An external definition can override a native tool. Its capabilities
		// come from that definition, not from the native tool's former source.
		out.Mode = "adopt"
		out.Notes = "纳管并切换本机已有安装；尚无可直接安装的发行包"
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
			if m.m.Notes == "" {
				out.Notes = "使用发行方归档安装，或纳管已有安装"
			}
			if m.m.Assets[target.OS+"/"+target.Arch] == "" {
				out.Mode = "adopt"
				out.Notes = "当前平台暂无自动安装归档；可纳管已有安装。" + m.m.Notes
			}
		} else if m.m.Source.Type == "github-auto" {
			out.Mode = "release"
			out.Notes = "按平台匹配 GitHub 发行包，须有发布方摘要；实际可用性以版本资产为准。" + m.m.Notes
		}
		if m.m.Service {
			out.Mode = "service"
			if m.m.Notes == "" {
				out.Notes = "通过数据库实例入口选择版本和运行方式"
			}
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
	switch tool {
	case "godot":
		return "--version", `(?m)^(\d+\.\d+(?:\.\d+)?)\.stable(?:\.|\s|$)`
	case "vyper":
		return "--version", `(?m)^(\d+\.\d+\.\d+)(?:\+commit\.[0-9a-f]+)?(?:\s|$)`
	case "nim":
		return "--version", `(?m)^Nim Compiler Version\s+(\d+\.\d+\.\d+)(?:\s|$)`
	}
	return "", ""
}
