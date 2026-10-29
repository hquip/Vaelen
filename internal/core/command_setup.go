package core

import (
	"fmt"
	"path/filepath"
	"runtime"

	"vsm/internal/logging"
)

// CommandSetup describes registration of VSM itself, independently of any
// language selection or project environment.
type CommandSetup struct {
	Status     string `json:"status"`
	CLI        string `json:"cli"`
	Shims      string `json:"shims"`
	CurrentCmd string `json:"current_cmd"`
}

// RegisterWindowsCommand makes a paired CLI available as `vsm`, using a
// user-level PATH entry and cmd startup helper. No language runtime is changed.
func (a Activator) RegisterWindowsCommand() (CommandSetup, error) {
	return a.registerWindowsCommandAt("Environment", cmdRegistryKey)
}

func (a Activator) registerWindowsCommandAt(environmentKey, cmdKey string) (result CommandSetup, err error) {
	finish := logging.Start("activation", "command.setup", logging.Fields{"operation_id": a.TraceID, "cli": a.VsmExe, "data_dir": a.Paths.Root})
	defer func() { finish(err) }()
	if runtime.GOOS != "windows" {
		return result, fmt.Errorf("自动注册 vsm 命令仅用于 Windows；其它平台使用 path-init")
	}
	if a.Paths.ProjectMode == "local" || a.Paths.ProjectMode == "docker" {
		return result, fmt.Errorf("vsm 命令必须注册到全局数据目录，不能使用隔离项目目录")
	}
	if a.Paths.Root == "" || !filepath.IsAbs(a.VsmExe) || !executableFile(a.VsmExe) {
		return result, fmt.Errorf("注册 vsm 命令需要明确的数据目录和可执行的 CLI 绝对路径")
	}
	if _, err := CmdActivationCommand(a.Paths.Root); err != nil {
		return result, err
	}
	if err := a.Paths.EnsureDirs(); err != nil {
		return result, err
	}
	if err := a.GenerateCLIShim(); err != nil {
		return result, err
	}
	status, err := persistShimsPathAt(a.Paths.Shims(), environmentKey, a.TraceID)
	if err != nil {
		return result, err
	}
	if _, err := persistCmdHookAt(a.Paths.Root, cmdKey, a.TraceID); err != nil {
		return result, fmt.Errorf("vsm 已加入用户 PATH，但 cmd 自动加载未完成: %w", err)
	}
	command, _ := CmdActivationCommand(a.Paths.Root)
	return CommandSetup{Status: status, CLI: a.VsmExe, Shims: a.Paths.Shims(), CurrentCmd: command}, nil
}
