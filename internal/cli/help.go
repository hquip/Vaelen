package cli

import (
	"fmt"
	"os"
	"strings"
)

type commandHelp struct {
	Summary     string
	Aliases     []string
	Usage       []string
	Options     []string
	Subcommands []string
	Examples    []string
	Notes       []string
}

func canonicalCommand(name string) string {
	for command, help := range commandHelpTopics {
		for _, alias := range help.Aliases {
			if name == alias {
				return command
			}
		}
	}
	return name
}

func isHelpFlag(arg string) bool { return arg == "-h" || arg == "--help" }

// exec's arguments after its tool name belong to the child command. run forwards
// everything after --. Only flags belonging to VSM may request VSM help.
func helpTopicForArgs(command string, args []string) ([]string, bool) {
	if command == "exec" {
		return []string{command}, len(args) == 1 && isHelpFlag(args[0])
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if isHelpFlag(arg) {
			topic := []string{command}
			if (command == "project" || command == "mcp" || command == "logs" || command == "tasks" || command == "tools" || command == "db") && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
				topic = append(topic, args[0])
			}
			return topic, true
		}
	}
	return nil, false
}

func printCommandHelp(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 0
	}
	topic := append([]string(nil), args...)
	topic[0] = canonicalCommand(topic[0])
	name := strings.Join(topic, " ")
	help, ok := commandHelpTopics[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "未知帮助主题: %s\n运行 vsm help 查看命令列表，或 vsm help <command> 查看指定命令。\n", strings.Join(args, " "))
		return 1
	}
	fmt.Printf("vsm %s - %s\n", name, help.Summary)
	if len(help.Aliases) > 0 {
		fmt.Println("别名:", strings.Join(help.Aliases, ", "))
	}
	printHelpSection("用法", help.Usage)
	printHelpSection("子命令", help.Subcommands)
	options := append(append([]string(nil), help.Options...), "-h, --help  查看本命令帮助")
	printHelpSection("选项", options)
	printHelpSection("说明", help.Notes)
	printHelpSection("示例", help.Examples)
	return 0
}

func printHelpSection(title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Printf("\n%s:\n", title)
	for _, line := range lines {
		fmt.Println("  " + line)
	}
}

var commandHelpTopics = map[string]commandHelp{
	"db use":         {Summary: "创建实例或选择数据库版本", Usage: []string{"vsm db use <数据库> <版本> [--name 实例] [--backend docker|native] [--port 端口] [--json]"}, Examples: []string{"vsm db use postgres 17 --name dev --port 55432", "vsm db use mysql 8.4 --backend native --name mysql-dev --port 3307"}, Notes: []string{"配置新实例后使用 db start 启动；运行中的实例会切换并重启，数据按版本隔离。原生版本需先 adopt 完整安装目录。"}},
	"db switch":      {Summary: "切换已有实例的数据库版本", Usage: []string{"vsm db switch <实例名> <版本> [--json]"}, Examples: []string{"vsm db switch dev 18"}, Notes: []string{"保留旧版本数据，启动失败尝试恢复旧服务；不自动迁移旧数据。"}},
	"db start":       {Summary: "启动数据库实例", Usage: []string{"vsm db start <实例名> [--json]"}, Examples: []string{"vsm db start dev"}},
	"db stop":        {Summary: "停止数据库实例并保留数据", Usage: []string{"vsm db stop <实例名> [--json]"}, Examples: []string{"vsm db stop dev"}},
	"db list":        {Summary: "列出数据库实例与状态", Usage: []string{"vsm db list [--json]"}, Examples: []string{"vsm db list --json"}},
	"db status":      {Summary: "列出数据库实例与状态", Usage: []string{"vsm db status [--json]"}, Examples: []string{"vsm db status"}},
	"db engines":     {Summary: "列出支持的数据库", Usage: []string{"vsm db engines"}, Examples: []string{"vsm db engines"}},
	"db versions":    {Summary: "列出已纳管的原生数据库版本", Usage: []string{"vsm db versions <数据库> [--json]"}, Examples: []string{"vsm db versions postgres"}},
	"db logs":        {Summary: "查看数据库实例最近日志", Usage: []string{"vsm db logs <实例名> [--json]"}, Examples: []string{"vsm db logs dev"}},
	"db connection":  {Summary: "显式显示含密码的本机连接地址", Usage: []string{"vsm db connection <实例名> [--json]"}, Examples: []string{"vsm db connection dev"}},
	"db exec":        {Summary: "通过该版本的数据库客户端执行参数", Usage: []string{"vsm db exec <实例名> -- <客户端参数>"}, Examples: []string{"vsm db exec dev -- -c \"SELECT version()\"", "vsm db exec cache -- SET hello world"}, Notes: []string{"-- 后的选项属于数据库客户端，包括 --help / --json。"}},
	"db":             {Summary: "数据库版本切换与独立实例管理", Aliases: []string{"database"}, Usage: []string{"vsm db use <数据库> <版本> [--name 实例] [--backend docker|native] [--port 端口]", "vsm db switch <实例> <版本>", "vsm db start|stop|logs|connection <实例>", "vsm db list [--json]", "vsm db exec <实例> -- <客户端参数>"}, Notes: []string{"支持 postgres / mysql / mariadb / redis / valkey。Docker 自动拉取官方镜像并锁定摘要；原生使用已纳管的完整数据库程序。", "同实例按版本隔离数据；切回旧版本恢复旧数据。运行中切换会重启服务，失败尝试恢复旧版本。新版本不会自动迁移旧数据。", "仅监听 127.0.0.1；连接密码保存在本地私有凭据文件，connection 显式显示连接信息。停止服务保留数据。"}, Examples: []string{"vsm db use postgres 17 --name dev --port 55432", "vsm db start dev", "vsm db switch dev 18", "vsm adopt mysql --path D:\\mysql-8.4", "vsm db use mysql 8.4 --backend native --name local-mysql --port 3307"}},
	"project import": {Summary: "预览或导入 mise 工具版本", Usage: []string{"vsm project import --from mise [--file 导出的JSON] [--apply]"}, Notes: []string{"默认只预览。--apply 创建新的 vsm.json，不覆盖已有文件。只导入工具版本，不复制任务或秘密变量。未指定文件时以 MISE_SAFE=1 调用 mise ls --current --json。"}, Examples: []string{"vsm project import --from mise", "vsm project import --from mise --file mise-tools.json --apply"}},

	"lock":              {Summary: "锁定项目工具链的具体安装包", Usage: []string{"vsm lock [--platform os/arch,...] [--update|--check] [--json]"}, Options: []string{"--platform  windows/amd64,linux/arm64 等目标", "--update  重新解析版本与包摘要", "--check  只检查锁文件与项目要求是否一致", "--json  结构化结果"}, Notes: []string{"安装包必须有发布方 SHA256。普通 lock 保留已有锁定构建。提交 vsm.lock 后使用 install --locked。"}, Examples: []string{"vsm lock", "vsm lock --platform windows/amd64,darwin/arm64,linux/amd64", "vsm install --locked", "vsm run --locked -- go test ./..."}},
	"tasks":             {Summary: "列出、规划或执行项目任务", Usage: []string{"vsm tasks list|graph [任务] [--json]", "vsm tasks run <任务...> [--jobs N] [--locked] [--no-cache] [--json] [--dry-run] [-- 参数]"}, Notes: []string{"任务在 vsm.json 中以命令参数数组声明。执行前需 project trust；依赖先完成，独立任务可并行。"}, Examples: []string{"vsm tasks list", "vsm tasks run test --jobs 4", "vsm tasks run build --locked --json"}},
	"tasks list":        {Summary: "列出项目任务", Usage: []string{"vsm tasks list [--json]"}, Examples: []string{"vsm tasks list --json"}},
	"tasks graph":       {Summary: "查看任务依赖执行顺序", Usage: []string{"vsm tasks graph [任务...] [--json]"}, Examples: []string{"vsm tasks graph build --json"}},
	"tasks run":         {Summary: "执行任务及其依赖", Usage: []string{"vsm tasks run <任务...> [--jobs 1–16] [--locked] [--no-cache] [--dry-run] [--json] [-- 参数]"}, Notes: []string{"默认最多并行 4 个任务。超时终止进程树，依赖失败阻止下游；只有声明 inputs 和 outputs 的任务会使用内容缓存。"}, Examples: []string{"vsm tasks run test", "vsm tasks run build --locked --jobs 4"}},
	"tools":             {Summary: "查找工具及添加 GitHub 安装来源", Usage: []string{"vsm tools list|search [关键词] [--json]", "vsm tools add <名称> github:owner/repo [--command 命令] [--bin 子目录] [--match 资产关键词]"}, Notes: []string{"GitHub 自动匹配当前平台的官方发布包；资产含 SHA256 且匹配唯一时才安装。不会执行第三方安装脚本。"}, Examples: []string{"vsm tools search python", "vsm tools add ripgrep github:BurntSushi/ripgrep --command rg --match msvc"}},
	"tools list":        {Summary: "查看工具目录", Usage: []string{"vsm tools list [--json]"}, Examples: []string{"vsm tools list --json"}},
	"tools search":      {Summary: "按名称或安装来源搜索工具", Usage: []string{"vsm tools search [关键词] [--json]"}, Examples: []string{"vsm tools search github --json"}},
	"tools add":         {Summary: "添加 GitHub 发布工具", Usage: []string{"vsm tools add <名称> github:owner/repo [--command 命令] [--bin 子目录] [--match 关键词]"}, Notes: []string{"定义保存在全局 plugins，不覆盖已有工具。新进程自动加载。多个资产无法唯一匹配时拒绝猜测。"}, Examples: []string{"vsm tools add ripgrep github:BurntSushi/ripgrep --command rg --match msvc"}},
	"project configure": {Summary: "创建统一项目配置", Usage: []string{"vsm project configure"}, Notes: []string{"生成 vsm.json，导入已声明工具并为 Go/Node 项目添加任务示例；保留已有配置。"}, Examples: []string{"vsm project configure"}},
	"project trust":     {Summary: "信任已检查的项目配置", Usage: []string{"vsm project trust"}, Notes: []string{"信任绑定本机项目路径及 vsm.json/env_files 内容；修改后失效。MCP 不能授予信任。"}, Examples: []string{"vsm project trust"}},
	"project inspect":   {Summary: "查看项目配置概况", Usage: []string{"vsm project inspect [--json]"}, Notes: []string{"报告工具、任务和环境变量名称，不返回环境变量值。"}, Examples: []string{"vsm project inspect --json"}},
	"logs path": {
		Summary: "显示全局日志目录", Usage: []string{"vsm logs path"},
		Notes: []string{"只输出路径，不创建目录；不受项目配置或自定义安装目录影响。"}, Examples: []string{"vsm logs path"},
	},
	"logs export": {
		Summary: "导出全部保留日志", Usage: []string{"vsm logs export <新文件.jsonl>"},
		Notes: []string{"导出 JSON Lines 文件，不覆盖已有文件，不支持导出到活动日志目录。"}, Examples: []string{"vsm logs export vsm-report.jsonl"},
	},
	"logs clear": {
		Summary: "清理 VSM 运行日志", Usage: []string{"vsm logs clear"},
		Notes: []string{"仅删除主日志及轮换备份；保留工具、项目配置、下载缓存及其它文件。"}, Examples: []string{"vsm logs clear"},
	},
	"logs diagnose": {
		Summary: "记录当前终端的版本选择和命令顺序", Usage: []string{"vsm logs diagnose [tool]"},
		Notes:    []string{"在出现问题的原终端执行，记录全局默认、有效版本、配置来源、PATH 前部和候选命令路径。", "只读取路由与配置，不运行语言程序，不修改 PATH；随后可用 logs export 导出。"},
		Examples: []string{"vsm logs diagnose java", "vsm logs diagnose"},
	},
	"logs": {
		Summary:  "查看、导出和清理本地诊断日志",
		Usage:    []string{"vsm logs [--tail N] [--level debug|info|warn|error] [--json]", "vsm logs path", "vsm logs diagnose [tool]", "vsm logs export <新文件.jsonl>", "vsm logs clear"},
		Options:  []string{"--tail N  最近 N 条，默认 100，范围 1–1000", "--level  最低显示级别，默认显示所有已记录级别", "--json  输出目录、级别、文件和记录"},
		Notes:    []string{"日志位于全局 VSM_HOME/logs（默认 ~/.vsm/logs），每个文件最多 2 MiB，保留 3 个备份。", "界面及文本日志显示本地时间和时区偏移；--json 和 export 保留 UTC（Z 表示 UTC）。", "VSM_LOG_LEVEL=debug|info|warn|error|off 控制记录级别，默认 info。off 关闭新日志，不删除旧日志。", "不记录完整命令参数、子进程输出、MCP 请求正文或完整环境；激活诊断只记录 PATH 前部、候选程序、PATHEXT、运行时 HOME 和版本覆盖变量。URL 凭据、查询参数及常见密钥会脱敏。", "cmd 初始化自动记录 PATH 调整前后，受控验证与用户会话分别标记；同次操作和初始化会话可按编号关联。", "export 导出全部保留日志到新文件，不覆盖已有文件；分享前请检查其中的本地路径。clear 只清理 VSM 日志。"},
		Examples: []string{"vsm logs --tail 50 --level error", "vsm logs --json", "vsm logs export vsm-diagnostics.jsonl", "vsm logs clear"},
	},
	"update": {
		Summary: "从官方 GitHub 更新 VSM 自身", Usage: []string{"vsm update [--check] [--version <版本>] [--gui] [--json]"},
		Options:  []string{"--check  仅检查可用版本，不修改程序", "--version <版本>  指定稳定版；不会降级", "--gui  同时更新相邻 GUI（macOS 应用内自动整体更新）", "--json  输出结构化结果"},
		Notes:    []string{"校验官方 SHA256 后替换，失败恢复旧程序；不修改语言安装、项目配置或 AI 设置。", "更新时在 stderr 显示下载进度、速度和校验阶段；--json 保持纯结构化输出。", "Windows 使用中的旧文件可能保留备份，退出旧程序后可删除；目录需要可写。"},
		Examples: []string{"vsm update --check", "vsm update", "vsm update --gui"},
	},
	"install": {
		Summary: "下载并安装指定工具的版本", Aliases: []string{"i"},
		Usage:    []string{"vsm install <tool> [version] [--path <目录>] [--require-checksum]", "vsm install --locked"},
		Options:  []string{"--path, -p <目录>  自定义安装位置", "--require-checksum  发布方缺少 SHA256 时拒绝安装"},
		Notes:    []string{"省略版本或使用 latest 安装最新稳定版；支持数字前缀，如 node 22。", "local 项目在项目内安装；Docker 项目的工具由镜像提供。"},
		Examples: []string{"vsm install go 1.25.11", "vsm install node 22 --require-checksum", `vsm install go 1.25.11 --path "D:\sdk"`},
	},
	"uninstall": {
		Summary: "卸载指定工具版本", Aliases: []string{"rm"},
		Usage:    []string{"vsm uninstall <tool> <version>"},
		Notes:    []string{"删除 VSM 管理的安装；纳管的系统版本只删除链接，保留原始文件。"},
		Examples: []string{"vsm uninstall go 1.25.11"},
	},
	"use": {
		Summary: "设置项目或全局默认版本", Aliases: []string{"u"},
		Usage:    []string{"vsm use [-g] <tool> <version>"},
		Options:  []string{"-g, --global  设置全局默认版本"},
		Notes:    []string{"默认写入当前目录的 .tool-versions；此命令不会下载工具。", "支持具体版本、数字前缀和 latest；本地隔离项目应设置项目版本。", "全局设置会重建 shim；终端需要 activate 或 path-init 配置的启动钩子，项目配置和 VSM_<TOOL>_VERSION 仍优先于全局。"},
		Examples: []string{"vsm use go 1.25.11", "vsm use -g node 22"},
	},
	"list": {
		Summary: "列出已安装版本", Aliases: []string{"ls"},
		Usage: []string{"vsm list [tool] [--json]"}, Options: []string{"--json  输出结构化 JSON"},
		Notes:    []string{"省略工具时列出所有支持的工具；* 标记当前生效版本。"},
		Examples: []string{"vsm list", "vsm list go --json"},
	},
	"list-all": {
		Summary: "查询远端可安装版本", Aliases: []string{"list-remote"},
		Usage: []string{"vsm list-all <tool> [--json]"}, Options: []string{"--json  输出结构化 JSON"},
		Notes:    []string{"此命令需要访问工具发布方的版本元数据。"},
		Examples: []string{"vsm list-all node", "vsm list-all go --json"},
	},
	"current": {
		Summary: "查看当前生效版本及来源", Usage: []string{"vsm current [--all] [--json]"},
		Options: []string{"--all  同时显示未启用的工具", "--json  输出结构化 JSON"}, Examples: []string{"vsm current", "vsm current --all", "vsm current --json"},
	},
	"context": {
		Summary: "读取供 AI / Agent 使用的项目工具链上下文", Usage: []string{"vsm context [--json] [--check]"},
		Options:  []string{"--json  输出版本要求、来源、状态与建议命令", "--check  环境未就绪或存在冲突时返回非零退出码"},
		Notes:    []string{"只读操作，不安装工具，也不修改项目或父终端环境。"},
		Examples: []string{"vsm context --json", "vsm context --json --check"},
	},
	"run": {
		Summary: "使用项目要求的完整工具链执行命令", Usage: []string{"vsm run [--install-missing] [--locked] [--env KEY=VALUE] -- <command> [args...]"},
		Options:  []string{"--install-missing  先补装缺失工具，强制验证发布方 SHA256", "--require-checksum  兼容选项；自动补装始终要求 SHA256", "--  分隔 VSM 选项与子命令"},
		Notes:    []string{"按项目隔离模式执行并透传子进程退出码，不修改父终端。", "-- 之后的 -h / --help 会原样传给子命令。"},
		Examples: []string{"vsm run -- go test ./...", "vsm run --install-missing -- python app.py", "vsm run -- node --help"},
	},
	"project": {
		Summary: "管理每项目独立环境", Usage: []string{"vsm project <init|status> [选项]"},
		Subcommands: []string{"init    初始化环境，默认 local", "status  查看环境状态"},
		Notes:       []string{"使用 vsm help project init 或 vsm help project status 查看子命令详情。"},
		Examples:    []string{"vsm project init", "vsm project status --json"},
	},
	"project init": {
		Summary: "为当前项目初始化环境", Usage: []string{"vsm project init [--mode local|versions|docker] [--image <镜像>]"},
		Options:  []string{"--mode <模式>  local（默认）：独立工具/缓存/venv；versions：共享工具；docker：容器执行", "--image <镜像>  Docker 模式必填，镜像需包含项目工具链"},
		Notes:    []string{"写入当前项目 .vsm/project.json；已有环境不能被此命令静默切换模式。"},
		Examples: []string{"vsm project init", "vsm project init --mode versions", "vsm project init --mode docker --image golang:1.25.11"},
	},
	"project status": {
		Summary: "查看当前项目的环境状态", Usage: []string{"vsm project status [--json]"},
		Options: []string{"--json  输出初始化状态、隔离模式和项目配置"}, Examples: []string{"vsm project status --json"},
	},
	"mcp": {
		Summary: "启动 MCP 服务或生成 AI 客户端配置", Usage: []string{"vsm mcp [serve|config] --project <目录> [选项]"},
		Subcommands: []string{"serve   启动绑定项目的 stdio 服务（默认）", "config  生成客户端 JSON / TOML 配置"},
		Options:     []string{"--project <目录>  必填，绑定的项目目录", "--read-only  仅开放查看工具"},
		Notes:       []string{"使用 vsm help mcp serve 或 vsm help mcp config 查看子命令详情。"},
		Examples:    []string{"vsm mcp config --project /path/to/app --client cursor", "vsm mcp serve --project /path/to/app"},
	},
	"mcp serve": {
		Summary: "启动绑定单个项目的 MCP stdio 服务", Usage: []string{"vsm mcp serve --project <目录> [--read-only]"},
		Options:  []string{"--project <目录>  必填，AI 管理操作绑定此项目", "--read-only  仅提供 3 个查看工具；默认提供 7 个工具"},
		Notes:    []string{"通常由 AI 客户端按生成的配置自动启动；stdout 仅用于 MCP 协议。"},
		Examples: []string{"vsm mcp serve --project /path/to/app --read-only"},
	},
	"mcp config": {
		Summary: "生成指定 AI 客户端的 MCP 配置", Usage: []string{"vsm mcp config --project <目录> [--client <客户端>] [--read-only]"},
		Options:  []string{"--project <目录>  必填，绑定的项目目录", "--client <客户端>  cursor / claude-desktop / claude-code / codex / vscode / windsurf / generic（默认）", "--read-only  生成只读连接配置"},
		Notes:    []string{"配置写到 stdout，不会覆盖客户端设置。Codex 输出 TOML，其余预设输出对应 JSON。"},
		Examples: []string{"vsm mcp config --project /path/to/app --client codex", `vsm mcp config --project "D:\work\app" --client vscode --read-only`},
	},
	"which": {
		Summary: "打印当前版本主命令的路径", Usage: []string{"vsm which <tool>"},
		Examples: []string{"vsm which go", "vsm which node"},
	},
	"exec": {
		Summary: "在所选工具版本环境中执行命令", Aliases: []string{"x"},
		Usage:    []string{"vsm exec <tool> [args...]", "vsm exec <tool> -- <command> [args...]"},
		Options:  []string{"--  指定在该工具环境中执行的其它命令"},
		Notes:    []string{"透传子进程退出码。工具名之后的 -h / --help 属于子进程；查看 VSM 帮助请用 vsm help exec 或 vsm exec --help。"},
		Examples: []string{"vsm exec go version", "vsm exec node -- npm --version", "vsm exec node --help"},
	},
	"reshim": {
		Summary: "为已安装工具重新生成 shim", Usage: []string{"vsm reshim"},
		Notes: []string{"在当前 VSM 数据目录重新生成命令垫片，并移除不再对应已安装工具的垫片。"}, Examples: []string{"vsm reshim"},
	},
	"activate": {
		Summary: "输出 shell 自动切换脚本", Usage: []string{"vsm activate [shell]"},
		Notes:    []string{"默认 bash；支持 bash / sh / zsh / fish / powershell / pwsh / cmd。", "此命令只输出脚本，需要由目标 shell 加载。"},
		Examples: []string{`eval "$(vsm activate bash)"`, "vsm activate powershell | Out-String | Invoke-Expression"},
	},
	"env": {
		Summary: "输出当前目录对应的 shell 环境", Usage: []string{"vsm env [shell]"},
		Notes: []string{"默认 bash；支持 bash / sh / zsh / fish / powershell / pwsh / cmd。", "通常由 activate 安装的 hook 调用。"}, Examples: []string{"vsm env bash", "vsm env powershell"},
	},
	"path-init": {
		Summary: "让新终端自动启用 VSM", Usage: []string{"vsm path-init [shell]"},
		Notes:    []string{"Windows 默认写入用户 PATH，并配置 cmd AutoRun 与 PowerShell 启动钩子；保留已有启动命令和自定义 prompt。path-init cmd 只配置 cmd 与用户 PATH。", "cmd 启动时将 shims 前置；已通过 VSM 的 cmd 后续全局切换无需重开。尚未激活的已有 cmd 需加载一次返回的命令；/D 跳过 AutoRun，支持 /V:ON 和 /V:OFF。", "不改系统 PATH 或执行策略。cmd 启动配置存于 HKCU\\Software\\Microsoft\\Command Processor，原命令保存在 VSM.AutoRun.Previous；不要手动删除原有命令。", "macOS / Linux 配置对应 shell 的启动文件。local / docker 项目使用 vsm run，不持久添加项目工具目录。"},
		Examples: []string{"vsm path-init", "vsm path-init zsh"},
	},
	"detect": {
		Summary: "探测系统中已有的语言运行时", Usage: []string{"vsm detect [--json]"},
		Options: []string{"--json  输出结构化 JSON"}, Notes: []string{"扫描 PATH 和相关环境变量，不自动纳管或安装。"},
		Examples: []string{"vsm detect", "vsm detect --json"},
	},
	"adopt": {
		Summary: "将系统已有运行时链接到 VSM 管理", Usage: []string{"vsm adopt <tool> [version] [可执行路径]", "vsm adopt <tool> --path <目录或可执行文件> [--name <标识>]", "vsm adopt --all"},
		Options:  []string{"--all, -a  纳管全部检测到且可用的版本", "--path <路径>  检查并纳管 PATH 之外的安装目录或主命令", "--name <标识>  为安装或 Conda 环境指定独立管理标识"},
		Notes:    []string{"创建链接，保留原始安装；local / docker 隔离模式拒绝纳管宿主机运行时。", "Conda Python 环境分别保留依赖，使用其完整管理标识选择；普通版本范围不会自动选中 Conda 环境。"},
		Examples: []string{"vsm adopt go", "vsm adopt go 1.25.11 /opt/go/bin/go", "vsm adopt python --path /opt/miniconda3/envs/science --name conda-science", "vsm adopt maven --path /opt/maven", "vsm adopt --all"},
	},
	"mirror": {
		Summary: "查看或设置下载镜像", Usage: []string{"vsm mirror [auto|cn|off|custom]", "vsm mirror set <go|node|java|npm|github|github-api> <URL>", "vsm mirror unset <源>", "vsm mirror list [--json]"},
		Options: []string{"--json  输出镜像配置"},
		Notes:   []string{"省略参数查看设置；默认 auto 优先代理、无代理时使用国内源；cn 固定启用国内镜像，off 关闭镜像。", "github 基地址作为代理前缀；java 使用 Adoptium 镜像根目录（例如 https://mirrors.tuna.tsinghua.edu.cn/Adoptium/）；其余源替换对应上游基地址。", "cn 对 JDK 启用清华镜像；失败时回退官方文件，并使用同一官方 SHA256。", "发布方 SHA256 仍从原站获取。npm 源通过 vsm run 的子进程环境生效，不改 .npmrc。"}, Examples: []string{"vsm mirror auto", "vsm mirror list --json", "vsm mirror set github https://mirror.example/", "vsm mirror set node https://mirror.example/node/"},
	},
	"proxy": {
		Summary: "查看或设置网络代理", Usage: []string{"vsm proxy [url|off|system]", "vsm proxy --json"},
		Notes:    []string{"支持 http / https / socks5 / socks5h；off 强制直连，system 自动检测代理环境变量、系统本机代理和常见端口。", "VSM_PROXY 环境变量优先于保存的设置；不带参数查看当前配置。"},
		Examples: []string{"vsm proxy", "vsm proxy http://127.0.0.1:7890", "vsm proxy system"},
	},
	"install-root": {
		Summary: "查看或设置全局安装根目录", Usage: []string{"vsm install-root [目录|default]"},
		Notes:    []string{"省略参数查看设置；default 恢复默认目录。此命令不搬迁已安装工具。", "目标目录缺少当前可用的全局版本时拒绝更改；先准备对应版本，或保留原查找目录。已失效的查找目录可恢复到包含原版本的目录。"},
		Examples: []string{"vsm install-root", `vsm install-root "D:\sdk"`, "vsm install-root default"},
	},
	"doctor": {
		Summary: "诊断 PATH、shim、hook 和配置问题", Usage: []string{"vsm doctor [--json]"},
		Options: []string{"--json  输出结构化诊断结果"}, Examples: []string{"vsm doctor", "vsm doctor --json"},
	},
	"completion": {
		Summary: "输出 shell 命令补全脚本", Usage: []string{"vsm completion <bash|zsh|fish|powershell>"},
		Notes:    []string{"powershell 也可写为 pwsh；将输出脚本加载到对应 shell。"},
		Examples: []string{`eval "$(vsm completion bash)"`, "vsm completion fish | source", "vsm completion powershell | Out-String | Invoke-Expression"},
	},
	"shell": {
		Summary: "打开使用指定工具版本的临时 shell", Usage: []string{"vsm shell <tool> <version> [shell]"},
		Notes:    []string{"使用已安装版本，不写版本配置；退出子 shell 后返回原环境。"},
		Examples: []string{"vsm shell go 1.25.11", "vsm shell node 22 bash"},
	},
	"size": {
		Summary: "统计安装目录和下载缓存占用", Usage: []string{"vsm size [tool] [--json]"},
		Options: []string{"--json  输出结构化空间统计"}, Examples: []string{"vsm size", "vsm size go --json"},
	},
	"clean": {
		Summary: "清理下载缓存", Usage: []string{"vsm clean [--json]"},
		Options: []string{"--json  输出清理数量与释放空间"}, Notes: []string{"仅清理下载缓存，保留已安装工具与项目依赖。"},
		Examples: []string{"vsm clean", "vsm clean --json"},
	},
	"version": {
		Summary: "显示 VSM 版本", Aliases: []string{"-v", "--version"}, Usage: []string{"vsm version"}, Examples: []string{"vsm version"},
	},
	"help": {
		Summary: "查看命令的详细帮助", Aliases: []string{"-h", "--help"}, Usage: []string{"vsm help [command [subcommand]]", "vsm <command> --help"},
		Notes:    []string{"不指定命令显示总览；支持命令别名。帮助不会初始化环境或执行命令。"},
		Examples: []string{"vsm help install", "vsm install --help", "vsm help project init", "vsm help mcp config"},
	},
}
