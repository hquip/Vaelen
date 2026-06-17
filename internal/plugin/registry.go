package plugin

import "sort"

// builtins 保存所有内置插件，由各插件文件的 init() 注册。
var builtins = map[string]Plugin{}

func register(p Plugin) { builtins[p.Name()] = p }

// Get 按名字取插件。
func Get(name string) (Plugin, bool) {
	p, ok := builtins[name]
	return p, ok
}

// Names 返回所有已注册插件名（已排序）。
func Names() []string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
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
	}
	for tool, names := range map[string][]string{
		"python":  {"python", "python3"},
		"vlang":   {"v"},
		"kotlin":  {"kotlin", "kotlinc"},
		"ruby":    {"ruby"},
		"rust":    {"rustc"},
		"haskell": {"ghc", "ghci"},
		"r":       {"R", "Rscript"},
		"vala":    {"valac"},
	} {
		if _, ok := commands[tool]; ok {
			commands[tool] = names
		}
	}
	return commands
}
