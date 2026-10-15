// Package cli 实现 vsm 的命令行界面：参数解析与子命令分发。
// 所有命令都委托给 internal/core 完成实际工作，便于未来 TUI/GUI 复用同一套核心。
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"vsm/internal/buildinfo"
	"vsm/internal/core"
	"vsm/internal/plugin"
)

// app 聚合一次命令执行所需的核心组件。
type app struct {
	paths core.Paths
	reg   core.Registry
	res   core.Resolver
	inst  core.Installer
	act   core.Activator
	cwd   string
}

func newApp() (*app, error) {
	paths, err := core.DefaultPaths()
	if err != nil {
		return nil, err
	}
	reg := core.Registry{Paths: paths}
	res := core.Resolver{Paths: paths, Registry: reg}
	// cwd 失败（目录被删、网络盘断开）会让 `use` 把 .tool-versions 写到错误位置，
	// 因此这里直接报错，而不是带着空 cwd 继续跑。
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("无法获取当前目录: %w", err)
	}
	paths, err = core.PathsForDirectory(paths, cwd)
	if err != nil {
		return nil, err
	}
	if expected := os.Getenv("VSM_MCP_EXPECT_MODE"); expected != "" && paths.ProjectMode != expected {
		return nil, fmt.Errorf("project isolation mode changed during MCP operation; retry after checking project status")
	}
	reg = core.Registry{Paths: paths}
	res = core.Resolver{Paths: paths, Registry: reg}
	// 可执行文件路径用于生成 shim；拿不到时退回 PATH 查找（见 shimTarget）。
	exe, _ := os.Executable()
	return &app{
		paths: paths,
		reg:   reg,
		res:   res,
		inst:  core.Installer{Paths: paths},
		act:   core.Activator{Paths: paths, Resolver: res, Registry: reg, VsmExe: exe},
		cwd:   cwd,
	}, nil
}

// exitCoder 由希望自定义退出码的错误实现（如 `vsm exec` 透传子进程退出码）。
type exitCoder interface {
	ExitCode() int
}

// Run 是 CLI 入口，返回进程退出码。
func Run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}
	cmd, rest := args[0], args[1:]
	cmd = canonicalCommand(cmd)
	if cmd == "help" {
		return printCommandHelp(rest)
	}
	if topic, requested := helpTopicForArgs(cmd, rest); requested {
		return printCommandHelp(topic)
	}
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println("vsm " + buildinfo.DisplayVersion())
		return 0
	}

	a, err := newApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		return 1
	}

	var runErr error
	switch cmd {
	case "install", "i":
		runErr = a.cmdInstall(rest)
	case "uninstall", "rm":
		runErr = a.cmdUninstall(rest)
	case "use", "u":
		runErr = a.cmdUse(rest)
	case "list", "ls":
		runErr = a.cmdList(rest)
	case "list-all", "list-remote":
		runErr = a.cmdListAll(rest)
	case "current":
		runErr = a.cmdCurrent(rest)
	case "context":
		runErr = a.cmdContext(rest)
	case "run":
		runErr = a.cmdRun(rest)
	case "project":
		runErr = a.cmdProject(rest)
	case "mcp":
		runErr = a.cmdMCP(rest)
	case "which":
		runErr = a.cmdWhich(rest)
	case "exec", "x":
		runErr = a.cmdExec(rest)
	case "reshim":
		runErr = a.cmdReshim(rest)
	case "activate":
		runErr = a.cmdActivate(rest)
	case "env":
		runErr = a.cmdEnv(rest)
	case "path-init":
		runErr = a.cmdPathInit(rest)
	case "detect":
		runErr = a.cmdDetect(rest)
	case "adopt":
		runErr = a.cmdAdopt(rest)
	case "mirror":
		runErr = a.cmdMirror(rest)
	case "proxy":
		runErr = a.cmdProxy(rest)
	case "install-root":
		runErr = a.cmdInstallRoot(rest)
	case "doctor":
		runErr = a.cmdDoctor(rest)
	case "completion":
		runErr = a.cmdCompletion(rest)
	case "shell":
		runErr = a.cmdShell(rest)
	case "clean":
		runErr = a.cmdClean(rest)
	case "size":
		runErr = a.cmdSize(rest)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		printUsage()
		return 1
	}
	if runErr != nil {
		// 透传子进程退出码（`vsm exec` 场景）：仅在没有可透传的码时才压成 1。
		var ec exitCoder
		if errors.As(runErr, &ec) {
			if code := ec.ExitCode(); code > 0 {
				return code
			}
			return 1
		}
		fmt.Fprintln(os.Stderr, "错误:", runErr)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Printf(usageText, buildinfo.DisplayVersion(), strings.Join(plugin.Names(), ", "))
}

const usageText = `vsm - 多语言版本管理器 (%s)

用法:
  vsm <命令> [参数]

命令:
  install   <tool> [version]   安装某工具版本（省略或 latest 装最新稳定版）
            [--path <目录>]      可选：装到自定义目录（建链接进 vsm，可正常切换/卸载）
            [--require-checksum] 没有发布方 SHA256 时拒绝安装
  install-root [目录|default]  查看/设置全局安装根目录（默认 ~/.vsm/installs）
  uninstall <tool> <version>   卸载某版本
  use [-g]  <tool> <version>   设为当前目录版本（写 .tool-versions）；-g 设为全局
  list      [tool]             列出已安装版本（* 为当前生效）
  list-all  <tool>             列出可安装的远端版本
  current                      显示各工具当前生效的版本及来源
  context [--json] [--check]    向 Agent/AI 报告项目要求、当前版本与建议命令（只读）
  run [--install-missing] -- <command> [args] 按项目版本自动执行（补装必须校验 SHA256）
  project init [--mode local|versions|docker] [--image 镜像] 为项目初始化隔离环境
  project status [--json]      显示项目环境
  mcp [serve|config] --project <目录> [--read-only] MCP 服务或客户端配置
      config --client <客户端>                 Cursor / Claude / Codex / VS Code / Windsurf 配置
  which     <tool>             打印当前版本主命令的完整路径
  exec      <tool> [--] [args] 在当前解析版本环境下执行命令
  reshim                       为已安装工具重新生成 shim
  activate  <shell>            输出 shell 激活脚本（含进目录自动切换 hook）
  env       [shell]            输出当前目录应生效的环境（供 hook 调用，一般不手动用）
  path-init [shell]            新终端自动启用 vsm（Windows 写用户 PATH；macOS/Linux 写 ~/.zshrc 等）
  detect                       探测系统 PATH/环境变量中已安装的语言版本
  adopt [--all] <tool> [ver]   纳管系统已装版本（链接进 vsm，可 use/卸载，不动原目录）
  mirror    [cn|off]           设置下载镜像（cn 国内加速 / off 关闭）
  proxy     [url|off|system]   设置网络代理（http/https/socks5；off 直连 / system 跟随系统）
  doctor    [--json]           环境体检（PATH 顺序、shim 完整性、hook、配置异常）
  shell     <tool> <version>   打开临时子 shell 使用某版本（不写任何配置）
  completion <shell>           输出 bash/zsh/fish/powershell 补全脚本
  size      [tool]             统计各版本占用的磁盘空间
  clean                        清理下载缓存
  version                      显示版本
  help [command [subcommand]]  查看总览或指定命令的用法、选项与示例

  list / list-all / current / detect / doctor / size / clean / context 支持 --json 结构化输出

支持的工具: %s

示例:
  vsm install go 1.22.0
  vsm use go 1.22.0
  vsm exec go version
  vsm activate powershell
  vsm help install
  vsm help project init
`
