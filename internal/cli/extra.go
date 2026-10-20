package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"vsm/internal/config"
	"vsm/internal/core"
	"vsm/internal/plugin"
)

// jsonFlag 判断参数里是否要求 JSON 输出，并返回去掉该开关后的参数。
func jsonFlag(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
	on := false
	for _, a := range args {
		if a == "--json" {
			on = true
			continue
		}
		out = append(out, a)
	}
	return out, on
}

// printJSON 输出缩进 JSON（供编辑器插件 / CI 集成消费）。
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// toolStatusJSON 是 --json 输出的单条工具状态。
type toolStatusJSON struct {
	Tool      string   `json:"tool"`
	Current   string   `json:"current"`
	Spec      string   `json:"spec"`
	Source    string   `json:"source"`
	Installed []string `json:"installed"`
}

// collectStatus 汇总所有工具状态（CLI 与 GUI 共享同一套核心，字段保持可解析）。
func (a *app) collectStatus() ([]toolStatusJSON, error) {
	names := plugin.Names()
	out := make([]toolStatusJSON, 0, len(names))
	for _, name := range names {
		installed, err := a.reg.Installed(name)
		if err != nil {
			return nil, err
		}
		if installed == nil {
			installed = []string{}
		}
		sort.Slice(installed, func(i, j int) bool {
			return core.CompareVersions(installed[i], installed[j]) > 0
		})
		r := a.res.Resolve(name, a.cwd)
		if r.Error != "" {
			return nil, fmt.Errorf("%s: %s", name, r.Error)
		}
		out = append(out, toolStatusJSON{
			Tool:      name,
			Current:   r.Version,
			Spec:      r.Spec,
			Source:    r.Source,
			Installed: installed,
		})
	}
	return out, nil
}

// ---------- vsm doctor ----------

type doctorIssue struct {
	Level  string `json:"level"` // ok | warn | error
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// cmdDoctor 体检运行环境：数据目录、PATH 顺序、shim 完整性、hook 是否装上、
// 镜像/代理状态、配置解析异常。目标是让「装不上 / 切不动」这类问题一眼有结论。
func (a *app) cmdDoctor(args []string) error {
	_, asJSON := jsonFlag(args)
	var issues []doctorIssue
	add := func(level, title, detail, fix string) {
		issues = append(issues, doctorIssue{level, title, detail, fix})
	}

	add("ok", "数据目录", a.paths.Root, "")
	if err := a.paths.EnsureDirs(); err != nil {
		add("error", "数据目录不可写", err.Error(), "检查目录权限，或用 VSM_HOME 指向其它位置")
	}

	installs := a.paths.Installs()
	add("ok", "安装根目录", installs, "")

	// 已安装（有 shim）的工具。只有这些工具会被「排在前面的同名系统命令」抢占；
	// 不能拿全部 73 种去比对，否则 macOS 自带的 /usr/bin/perl、/usr/bin/ruby
	// 在用户根本没用 vsm 管理它们时也会被误报。
	tools, toolsErr := a.reg.Tools()
	var managed []string
	for _, tool := range tools {
		if _, ok := plugin.Get(tool); !ok {
			continue
		}
		if v, err := a.reg.Installed(tool); err == nil && len(v) > 0 {
			managed = append(managed, tool)
		}
	}

	// PATH 相关检查
	shimsDir := a.paths.Shims()
	pathDirs := filepath.SplitList(os.Getenv("PATH"))
	shimIdx := indexFold(pathDirs, shimsDir)
	switch {
	case shimIdx < 0:
		add("warn", "shims 不在 PATH 中", shimsDir,
			"运行 `vsm path-init`（Windows 配置用户 PATH 和 cmd / PowerShell 钩子；macOS/Linux 写入 shell 配置），未激活的已有终端加载一次激活命令")
	default:
		var ahead []string
		for _, d := range pathDirs[:shimIdx] {
			for _, tool := range managed {
				if exeInDir(d, tool) || exeInDir(d, tool+".exe") || exeInDir(d, tool+".cmd") {
					ahead = append(ahead, filepath.Join(d, tool))
				}
			}
		}
		if len(ahead) > 0 {
			add("warn", "PATH 中有同名命令排在 vsm shims 之前",
				"vsm 选好的版本会被它们抢占："+strings.Join(ahead, ", "),
				"运行 `vsm path-init` 配置 cmd / PowerShell 自动激活；已加载 VSM 的终端后续全局切换无需重开")
		} else {
			add("ok", "shims 在 PATH 中", fmt.Sprintf("位置 #%d: %s", shimIdx+1, shimsDir), "")
		}
	}

	// PATH 重复条目（多次 source 激活脚本、或 Windows 用户/系统 PATH 重叠都会造成）
	seen := map[string]int{}
	var dupes []string
	for _, d := range pathDirs {
		if d == "" {
			continue
		}
		key := filepath.Clean(d)
		if runtime.GOOS != "linux" { // Windows 与 macOS 默认文件系统不区分大小写
			key = strings.ToLower(key)
		}
		seen[key]++
		if seen[key] == 2 {
			dupes = append(dupes, d)
		}
	}
	if len(dupes) > 0 {
		shown := dupes
		if len(shown) > 5 {
			shown = append(append([]string{}, shown[:5]...), fmt.Sprintf("…等 %d 个", len(dupes)))
		}
		add("warn", fmt.Sprintf("PATH 中有 %d 个重复目录", len(dupes)), strings.Join(shown, "; "),
			"不影响使用，但会拖慢命令查找；检查 shell 配置里 activate 是否执行了多次")
	} else {
		add("ok", "PATH 无重复条目", "", "")
	}

	// shim 完整性：已装工具是否都有 shim
	shimMissing := []string{}
	shimStale := []string{}
	if toolsErr != nil {
		add("error", "读取已安装列表失败", toolsErr.Error(), "检查 installs 目录权限")
	} else {
		for _, tool := range tools {
			if _, ok := plugin.Get(tool); !ok {
				continue
			}
			shim := filepath.Join(shimsDir, tool)
			if !fileExists(shim) && !fileExists(shim+".cmd") {
				shimMissing = append(shimMissing, tool)
			}
		}
		// 反向：shim 指向的工具已经没有任何版本了
		entries, _ := os.ReadDir(shimsDir)
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".cmd")
			if name == "vsm" {
				continue
			}
			if _, ok := plugin.Get(name); !ok {
				continue
			}
			if v, _ := a.reg.Installed(name); len(v) == 0 {
				shimStale = append(shimStale, name)
			}
		}
	}
	if len(shimMissing) > 0 {
		add("warn", "部分已装工具缺少 shim", strings.Join(shimMissing, ", "), "运行 `vsm reshim`")
	}
	if len(shimStale) > 0 {
		add("warn", "存在指向空版本的残留 shim", strings.Join(shimStale, ", "), "运行 `vsm reshim` 清理")
	}
	if len(shimMissing) == 0 && len(shimStale) == 0 && toolsErr == nil {
		add("ok", "shim 完整", fmt.Sprintf("%d 个工具", len(managed)), "")
	}

	// 当前目录解析结果
	if sv, err := config.Load(filepath.Join(a.cwd, ".tool-versions")); err == nil {
		if warns := sv.Warnings(); len(warns) > 0 {
			add("warn", ".tool-versions 有无法解析的行", strings.Join(warns, ", "),
				"这些行只有工具名、缺少版本号")
		} else if len(sv.Tools()) > 0 {
			add("ok", "当前目录 .tool-versions", strings.Join(sv.Tools(), ", "), "")
		}
	}

	// 全局配置
	if gv, err := config.Load(a.paths.GlobalToolVersions()); err == nil {
		if warns := gv.Warnings(); len(warns) > 0 {
			add("warn", "全局 .tool-versions 有无法解析的行", strings.Join(warns, ", "), "")
		}
	}

	// 镜像与代理
	mirror := plugin.MirrorMode()
	if mirror == "" || mirror == "auto" {
		add("ok", "下载镜像", "自动：优先代理，无代理时使用国内源", "")
	} else if mirror == "off" {
		add("ok", "下载镜像", "未启用", "国内网络可执行 `vsm mirror cn` 加速")
	} else {
		add("ok", "下载镜像", mirror, "")
	}

	// 下载缓存体积
	if size, n := dirSize(a.paths.Downloads()); n > 0 {
		add("ok", "下载缓存", fmt.Sprintf("%d 个文件，%s", n, humanSize(size)),
			"不再需要时可用 `vsm clean` 清理")
	}

	if asJSON {
		return printJSON(map[string]any{
			"dataDir":   a.paths.Root,
			"installs":  a.paths.Installs(),
			"shims":     a.paths.Shims(),
			"cwd":       a.cwd,
			"issues":    issues,
			"hasWarns":  hasLevel(issues, "warn"),
			"hasErrors": hasLevel(issues, "error"),
		})
	}

	fmt.Println("vsm 环境体检")
	fmt.Println(strings.Repeat("─", 58))
	for _, is := range issues {
		mark := "✓"
		switch is.Level {
		case "warn":
			mark = "!"
		case "error":
			mark = "✗"
		}
		fmt.Printf("%s %s\n", mark, is.Title)
		if is.Detail != "" {
			fmt.Printf("    %s\n", is.Detail)
		}
		if is.Fix != "" {
			fmt.Printf("    → %s\n", is.Fix)
		}
	}
	fmt.Println(strings.Repeat("─", 58))
	if hasLevel(issues, "error") {
		fmt.Println("发现需要处理的问题。")
		return fmt.Errorf("体检未通过")
	} else if hasLevel(issues, "warn") {
		fmt.Println("有可用性建议，见上方 ! 项。")
	} else {
		fmt.Println("一切正常。")
	}
	return nil
}

// ---------- vsm completion ----------

func (a *app) cmdCompletion(args []string) error {
	if len(args) == 0 {
		fmt.Println("用法: vsm completion <bash|zsh|fish|powershell>")
		fmt.Println("      eval \"$(vsm completion bash)\"   # 立即生效")
		return nil
	}
	sh := strings.ToLower(args[0])
	tools := strings.Join(plugin.Names(), " ")
	switch sh {
	case "bash":
		fmt.Printf(`# vsm bash 补全：eval "$(vsm completion bash)"
_vsm_completion() {
  COMPREPLY=()
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local cmds="install uninstall use list list-all current context run project mcp which exec reshim activate env path-init detect adopt mirror proxy install-root doctor completion shell clean size version update logs lock tasks tools db help"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "$cmds" -- "$cur") )
    return
  fi
  if [ "$COMP_CWORD" -eq 2 ]; then
    case "${COMP_WORDS[1]}" in
      help) COMPREPLY=( $(compgen -W "$cmds" -- "$cur") ) ;;
      use|install|uninstall|list|list-all|which|exec|adopt|shell|size)
        COMPREPLY=( $(compgen -W "%s" -- "$cur") ) ;;
      activate|env|completion|path-init)
        COMPREPLY=( $(compgen -W "bash zsh sh fish powershell pwsh cmd" -- "$cur") ) ;;
      mirror) COMPREPLY=( $(compgen -W "auto cn off custom" -- "$cur") ) ;;
      tasks) COMPREPLY=( $(compgen -W "list graph run" -- "$cur") ) ;;
      db) COMPREPLY=( $(compgen -W "use switch start stop list status versions engines logs connection exec" -- "$cur") ) ;;
      tools) COMPREPLY=( $(compgen -W "list search add" -- "$cur") ) ;;
      project) COMPREPLY=( $(compgen -W "init status configure inspect trust import" -- "$cur") ) ;;
      logs) COMPREPLY=( $(compgen -W "path export clear --tail --level --json" -- "$cur") ) ;;
      proxy)  COMPREPLY=( $(compgen -W "off system" -- "$cur") ) ;;
    esac
  fi
}
complete -F _vsm_completion vsm
`, tools)
	case "zsh":
		fmt.Printf(`#compdef vsm
# vsm zsh 补全：在 ~/.zshrc 里 compinit 之后加入  eval "$(vsm completion zsh)"
(( $+functions[compdef] )) || { autoload -Uz compinit && compinit }
_vsm() {
  local -a cmds tools shells
  cmds=(install uninstall use list list-all current context run project mcp which exec reshim activate env path-init detect adopt mirror proxy install-root doctor completion shell clean size version update logs lock tasks tools db help)
  tools=(%s)
  shells=(bash zsh sh fish powershell pwsh cmd)
  if (( CURRENT == 2 )); then
    _describe 'command' cmds
    return
  fi
  if (( CURRENT == 3 )); then
    case $words[2] in
      help) _describe 'command' cmds ;;
      use|install|uninstall|list|list-all|which|exec|adopt|shell|size) _describe 'tool' tools ;;
      activate|env|completion|path-init) _describe 'shell' shells ;;
      mirror) _values 'mode' auto cn off custom ;;
      tasks) _values 'tasks' list graph run ;;
      db) _values 'database' use switch start stop list status versions engines logs connection exec ;;
      tools) _values 'tools' list search add ;;
      project) _values 'project' init status configure inspect trust import ;;
      logs) _values 'logs' path export clear --tail --level --json ;;
      proxy) _values 'mode' off system ;;
    esac
  fi
}
compdef _vsm vsm
`, tools)
	case "fish":
		fmt.Printf(`# vsm fish 补全：vsm completion fish | source
set -l vsm_cmds install uninstall use list list-all current context run project mcp which exec reshim activate env path-init detect adopt mirror proxy install-root doctor completion shell clean size version update logs lock tasks tools db help
set -l vsm_tools %s
complete -c vsm -f
complete -c vsm -n "not __fish_seen_subcommand_from $vsm_cmds" -a "$vsm_cmds"
complete -c vsm -n "__fish_seen_subcommand_from help" -a "$vsm_cmds"
complete -c vsm -n "__fish_seen_subcommand_from use install uninstall list list-all which exec adopt shell" -a "$vsm_tools"
complete -c vsm -n "__fish_seen_subcommand_from activate env completion" -a "bash zsh sh fish powershell pwsh cmd"
complete -c vsm -n "__fish_seen_subcommand_from mirror" -a "auto cn off custom"
complete -c vsm -n "__fish_seen_subcommand_from logs" -a "path export clear"
complete -c vsm -n "__fish_seen_subcommand_from db" -a "use switch start stop list status versions engines logs connection exec"
complete -c vsm -n "__fish_seen_subcommand_from proxy" -a "off system"
`, tools)
	case "powershell", "pwsh":
		// 候选数组内联进补全块：补全块执行时不一定能看到 Invoke-Expression 所在作用域的变量
		fmt.Printf(`# vsm PowerShell 补全：vsm completion powershell | Out-String | Invoke-Expression
Register-ArgumentCompleter -Native -CommandName vsm -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $cmds = @('install','uninstall','use','list','list-all','current','context','run','project','mcp','which','exec','reshim','activate','env','path-init','detect','adopt','mirror','proxy','install-root','doctor','completion','shell','clean','size','version','update','logs','lock','tasks','tools','db','help')
    $tools = @(%s)
    $shells = @('bash','zsh','sh','fish','powershell','pwsh','cmd')
    $words = @($commandAst.CommandElements | ForEach-Object { $_.ToString() })
    # 正在补全的参数序号：光标在空白处时是「下一个」参数
    $idx = if ($wordToComplete) { $words.Count - 1 } else { $words.Count }
    $cands = @()
    if ($idx -le 1) { $cands = $cmds }
    elseif ($idx -eq 2) {
        switch ($words[1]) {
            'help' { $cands = $cmds }
            { $_ -in @('use','install','uninstall','list','list-all','which','exec','adopt','shell','size') } { $cands = $tools }
            { $_ -in @('activate','env','completion','path-init') } { $cands = $shells }
            'mirror' { $cands = @('auto','cn','off','custom') }
            'tasks' { $cands = @('list','graph','run') }
            'db' { $cands = @('use','switch','start','stop','list','status','versions','engines','logs','connection','exec') }
            'tools' { $cands = @('list','search','add') }
            'project' { $cands = @('init','status','configure','inspect','trust','import') }
            'logs' { $cands = @('path','export','clear','--tail','--level','--json') }
            'proxy'  { $cands = @('off','system') }
        }
    }
    $cands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`, strings.Join(quoteEach(plugin.Names()), ","))
	default:
		return fmt.Errorf("不支持的 shell: %s（支持 bash/zsh/fish/powershell）", sh)
	}
	return nil
}

func quoteEach(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "'" + s + "'"
	}
	return out
}

// ---------- vsm shell ----------

// cmdShell 打开一个「临时使用某版本」的子 shell：只改当前会话的环境，
// 不写 .tool-versions、不影响其它终端。适合「我就想试试 Java 21 的虚拟线程」。
func (a *app) cmdShell(args []string) error {
	tool, version := "", ""
	sh := ""
	rest := args
	if len(rest) >= 2 {
		if _, ok := plugin.Get(rest[0]); ok {
			tool, version = rest[0], rest[1]
			rest = rest[2:]
		}
	}
	if len(rest) >= 1 {
		sh = rest[0]
	}
	if tool == "" {
		return fmt.Errorf("用法: vsm shell <tool> <version> [shell]\n      不指定版本时可用 vsm shell <tool> latest")
	}
	p, ok := plugin.Get(tool)
	if !ok {
		return unsupported(tool)
	}
	installed, err := a.reg.Installed(tool)
	if err != nil {
		return err
	}
	resolved := core.BestMatch(version, installed)
	if resolved == "" {
		return fmt.Errorf("%s 没有匹配 %q 的已安装版本；先执行 `vsm install %s %s`",
			tool, version, tool, version)
	}
	installDir := a.paths.InstallDir(tool, resolved)
	binDirs := p.BinPaths(installDir)

	if sh == "" {
		sh = os.Getenv("SHELL")
		if sh == "" {
			if isWindows() {
				sh = "powershell"
			} else {
				sh = "bash"
			}
		}
		sh = filepath.Base(sh)
	}
	sh, err = normalizeShellName(sh)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "进入 %s %s 临时环境（%s）。输入 exit 退出，不影响其它终端。\n", tool, resolved, sh)

	env := os.Environ()
	prefix := strings.Join(binDirs, string(os.PathListSeparator))
	env = setEnv(env, "PATH", prefix+string(os.PathListSeparator)+os.Getenv("PATH"))
	for k, v := range p.ExecEnv(installDir) {
		env = setEnv(env, k, v)
	}
	return runShell(sh, env)
}

// ---------- vsm clean / size ----------

// cmdClean 清理下载缓存（安装完的压缩包不再需要）。
func (a *app) cmdClean(args []string) error {
	_, asJSON := jsonFlag(args)
	dir := a.paths.Downloads()
	size, n := dirSize(dir)
	if n == 0 {
		if asJSON {
			return printJSON(map[string]any{"removed": 0, "freedBytes": 0})
		}
		fmt.Println("下载缓存已是空的。")
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("清理下载缓存失败: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if asJSON {
		return printJSON(map[string]any{"removed": n, "freedBytes": size})
	}
	fmt.Printf("已清理 %d 个下载缓存文件，释放 %s\n", n, humanSize(size))
	return nil
}

// cmdSize 统计各工具版本占用的磁盘空间。
func (a *app) cmdSize(args []string) error {
	rest, asJSON := jsonFlag(args)
	filter := ""
	if len(rest) > 0 {
		filter = rest[0]
	}
	tools, err := a.reg.Tools()
	if err != nil {
		return err
	}
	type row struct {
		Tool    string `json:"tool"`
		Version string `json:"version"`
		Path    string `json:"path"`
		Bytes   int64  `json:"bytes"`
		Adopted bool   `json:"adopted"` // 纳管的系统版本：空间不归 vsm，不计入合计
	}
	var rows []row
	var total int64
	for _, tool := range tools {
		if filter != "" && tool != filter {
			continue
		}
		versions, err := a.reg.Installed(tool)
		if err != nil {
			return fmt.Errorf("读取 %s 的已安装版本失败: %w", tool, err)
		}
		for _, v := range versions {
			real, owned := core.InstallTarget(a.paths.InstallDir(tool, v))
			sz, _ := dirSize(real)
			rows = append(rows, row{tool, v, real, sz, !owned})
			if owned {
				total += sz
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Bytes > rows[j].Bytes })
	if asJSON {
		return printJSON(map[string]any{"items": rows, "totalBytes": total})
	}
	if len(rows) == 0 {
		fmt.Println("没有已安装的版本。")
		return nil
	}
	for _, r := range rows {
		fmt.Printf("%-10s %-14s %10s  %s\n", r.Tool, r.Version, humanSize(r.Bytes), r.Path)
	}
	fmt.Println(strings.Repeat("─", 58))
	fmt.Printf("共 %d 个版本，占用 %s\n", len(rows), humanSize(total))
	dlSize, dlN := dirSize(a.paths.Downloads())
	if dlN > 0 {
		fmt.Printf("另有下载缓存 %d 个文件 %s（`vsm clean` 可清理）\n", dlN, humanSize(dlSize))
	}
	return nil
}

// ---------- 小工具 ----------

func hasLevel(issues []doctorIssue, level string) bool {
	for _, i := range issues {
		if i.Level == level {
			return true
		}
	}
	return false
}

func indexFold(list []string, want string) int {
	cw := strings.ToLower(filepath.Clean(want))
	for i, d := range list {
		if d == "" {
			continue
		}
		if strings.ToLower(filepath.Clean(d)) == cw {
			return i
		}
	}
	return -1
}

func exeInDir(dir, name string) bool {
	fi, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !fi.IsDir()
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// dirSize 递归统计目录下普通文件的大小与数量（不跟随目录内部的符号链接）。
func dirSize(root string) (int64, int) {
	var size int64
	var count int
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		size += info.Size()
		count++
		return nil
	})
	return size, count
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
