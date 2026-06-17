// Package cli 实现 vsm 的命令行界面：参数解析与子命令分发。
// 所有命令都委托给 internal/core 完成实际工作，便于未来 TUI/GUI 复用同一套核心。
package cli

import (
	"fmt"
	"os"
	"strings"

	"vsm/internal/core"
	"vsm/internal/plugin"
)

const version = "0.1.0"

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
	cwd, _ := os.Getwd()
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

// Run 是 CLI 入口，返回进程退出码。
func Run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage()
		return 0
	case "version", "-v", "--version":
		fmt.Println("vsm " + version)
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
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		printUsage()
		return 1
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "错误:", runErr)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Printf(usageText, strings.Join(plugin.Names(), ", "))
}

const usageText = `vsm - 多语言版本管理器 (` + version + `)

用法:
  vsm <命令> [参数]

命令:
  install   <tool> [version]   安装某工具版本（省略或 latest 装最新稳定版）
            [--path <目录>]      可选：装到自定义目录（建链接进 vsm，可正常切换/卸载）
  install-root [目录|default]  查看/设置全局安装根目录（默认 ~/.vsm/installs）
  uninstall <tool> <version>   卸载某版本
  use [-g]  <tool> <version>   设为当前目录版本（写 .tool-versions）；-g 设为全局
  list      [tool]             列出已安装版本（* 为当前生效）
  list-all  <tool>             列出可安装的远端版本
  current                      显示各工具当前生效的版本及来源
  which     <tool>             打印当前版本主命令的完整路径
  exec      <tool> [--] [args] 在当前解析版本环境下执行命令
  reshim                       为已安装工具重新生成 shim
  activate  <shell>            输出 shell 激活脚本（含进目录自动切换 hook）
  env       [shell]            输出当前目录应生效的环境（供 hook 调用，一般不手动用）
  path-init                    [Windows] 把 shims 写入用户级 PATH 并广播（新终端免改配置）
  detect                       探测系统 PATH/环境变量中已安装的语言版本
  adopt [--all] <tool> [ver]   纳管系统已装版本（链接进 vsm，可 use/卸载，不动原目录）
  mirror    [cn|off]           设置下载镜像（cn 国内加速 / off 关闭）
  proxy     [url|off|system]   设置网络代理（http/https/socks5；off 直连 / system 跟随系统）
  version                      显示版本
  help                         显示帮助

支持的工具: %s

示例:
  vsm install go 1.22.0
  vsm use go 1.22.0
  vsm exec go version
  vsm activate powershell
`
