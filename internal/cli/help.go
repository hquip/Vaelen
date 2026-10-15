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
			if (command == "project" || command == "mcp") && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
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
	"install": {
		Summary: "下载并安装指定工具的版本", Aliases: []string{"i"},
		Usage:    []string{"vsm install <tool> [version] [--path <目录>] [--require-checksum]"},
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
		Notes:    []string{"默认写入当前目录的 .tool-versions；此命令不会下载工具。", "支持具体版本、数字前缀和 latest；本地隔离项目应设置项目版本。"},
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
		Summary: "查看当前生效版本及来源", Usage: []string{"vsm current [--json]"},
		Options: []string{"--json  输出结构化 JSON"}, Examples: []string{"vsm current", "vsm current --json"},
	},
	"context": {
		Summary: "读取供 AI / Agent 使用的项目工具链上下文", Usage: []string{"vsm context [--json] [--check]"},
		Options:  []string{"--json  输出版本要求、来源、状态与建议命令", "--check  环境未就绪或存在冲突时返回非零退出码"},
		Notes:    []string{"只读操作，不安装工具，也不修改项目或父终端环境。"},
		Examples: []string{"vsm context --json", "vsm context --json --check"},
	},
	"run": {
		Summary: "使用项目要求的完整工具链执行命令", Usage: []string{"vsm run [--install-missing] -- <command> [args...]"},
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
		Notes:    []string{"Windows 写入用户级 PATH；macOS / Linux 配置对应 shell 的启动文件。", "local / docker 项目使用 vsm run，不持久添加项目工具目录。"},
		Examples: []string{"vsm path-init", "vsm path-init zsh"},
	},
	"detect": {
		Summary: "探测系统中已有的语言运行时", Usage: []string{"vsm detect [--json]"},
		Options: []string{"--json  输出结构化 JSON"}, Notes: []string{"扫描 PATH 和相关环境变量，不自动纳管或安装。"},
		Examples: []string{"vsm detect", "vsm detect --json"},
	},
	"adopt": {
		Summary: "将系统已有运行时链接到 VSM 管理", Usage: []string{"vsm adopt <tool> [version] [可执行路径]", "vsm adopt --all"},
		Options:  []string{"--all, -a  纳管全部检测到且可用的版本"},
		Notes:    []string{"创建链接，保留原始安装；local / docker 隔离模式拒绝纳管宿主机运行时。"},
		Examples: []string{"vsm adopt go", "vsm adopt go 1.25.11 /opt/go/bin/go", "vsm adopt --all"},
	},
	"mirror": {
		Summary: "查看或设置下载镜像", Usage: []string{"vsm mirror [cn|off]"},
		Notes: []string{"省略参数查看设置；cn 启用国内镜像，off 关闭。"}, Examples: []string{"vsm mirror", "vsm mirror cn", "vsm mirror off"},
	},
	"proxy": {
		Summary: "查看或设置网络代理", Usage: []string{"vsm proxy [url|off|system]"},
		Notes:    []string{"支持 http / https / socks5；off 强制直连，system 跟随系统环境。", "VSM_PROXY 环境变量优先于保存的设置。"},
		Examples: []string{"vsm proxy", "vsm proxy http://127.0.0.1:7890", "vsm proxy system"},
	},
	"install-root": {
		Summary: "查看或设置全局安装根目录", Usage: []string{"vsm install-root [目录|default]"},
		Notes:    []string{"省略参数查看设置；default 恢复默认目录。此命令不搬迁已安装工具。"},
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
