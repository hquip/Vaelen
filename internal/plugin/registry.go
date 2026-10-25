package plugin

import (
	"runtime"
	"sort"
	"sync"
)

var registryMu sync.RWMutex

// builtins 保存所有内置插件，由各插件文件的 init() 注册。
var builtins = map[string]Plugin{}
var externalTools = map[string]Plugin{}

func register(p Plugin) { registryMu.Lock(); defer registryMu.Unlock(); builtins[p.Name()] = p }

// Get 按名字取插件。
func Get(name string) (Plugin, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := externalTools[name]
	if !ok {
		p, ok = builtins[name]
	}
	return p, ok
}

func IsServiceTool(name string) bool {
	p, ok := Get(name)
	if !ok {
		return false
	}
	manifest, ok := p.(manifestPlugin)
	return ok && manifest.m.Service
}

// ProbeCommands excludes service administration/bootstrap utilities from scans,
// while DetectCommands retains all executable aliases for project execution.
func ProbeCommands() map[string][]string {
	commands := DetectCommands()
	for tool := range commands {
		if VersionMode(tool) == "label" {
			delete(commands, tool)
			continue
		}
		raw, _ := Get(tool)
		if p, ok := raw.(manifestPlugin); ok && len(p.m.DetectCommands) > 0 {
			commands[tool] = append([]string{}, p.m.DetectCommands...)
		}
	}
	return commands
}

// Names 返回所有已注册插件名（已排序）。
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(builtins)+len(externalTools))
	for n := range builtins {
		names = append(names, n)
	}
	for n := range externalTools {
		if _, builtin := builtins[n]; !builtin {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// DetectCommands 返回“工具名 → 候选系统命令名”的映射，供系统检测扫描 PATH 用。
// 多数语言的主命令名即工具名；少数存在别名或可执行名不同（如 vlang 的命令是 v、
// python 还可能叫 python3）。CLI 与 GUI 共用这一份，避免两边各写一套。
func DetectCommands() map[string][]string {
	commands := map[string][]string{}
	for _, name := range Names() {
		commands[name] = []string{name}
		raw, _ := Get(name)
		if p, ok := raw.(manifestPlugin); ok && len(p.m.Commands) > 0 {
			commands[name] = append([]string{}, p.m.Commands...)
		}
	}
	for tool, names := range map[string][]string{
		"python":  {"python", "python3"},
		"dotnet":  {"dotnet"},
		"flutter": {"flutter"},
		"maven":   {"mvn"},
		"vlang":   {"v"},
		"kotlin":  {"kotlin", "kotlinc"},
		"ruby":    {"ruby"},
		"rust":    {"rustc"},
		"haskell": {"ghc", "ghci"},
		"r":       {"R", "Rscript"},
		"vala":    {"valac"},
		"godot":   {"godot"},
	} {
		if _, ok := commands[tool]; ok {
			commands[tool] = names
		}
	}
	if runtime.GOOS == "darwin" {
		commands["godot"] = []string{"godot", "Godot"}
	}
	if runtime.GOOS == "windows" {
		commands["godot"] = []string{"godot", "godot_console"}
	}
	return commands
}
