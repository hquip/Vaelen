package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"vsm/internal/config"
	"vsm/internal/logging"
	"vsm/internal/plugin"
)

// ActivationStatus distinguishes the saved default, project resolution and the
// command actually found on this process's PATH. A GUI cannot change an already
// running terminal's environment; callers must not describe a saved default as
// proof that an external terminal has switched.
type ActivationStatus struct {
	Global     string   `json:"global"`
	Current    string   `json:"current"`
	Source     string   `json:"source"`
	Executable string   `json:"executable"`
	Command    string   `json:"command"`
	CurrentCmd string   `json:"current_cmd,omitempty"`
	Warnings   []string `json:"warnings"`
}

func (a Activator) ActivationStatus(tool, dir string) ActivationStatus {
	status := ActivationStatus{Warnings: []string{}}
	if runtime.GOOS == "windows" && fileExists(filepath.Join(a.Paths.Root, "vsm-cmd-init.cmd")) {
		status.CurrentCmd, _ = CmdActivationCommand(a.Paths.Root)
	}
	if tv, err := config.Load(a.Paths.GlobalToolVersions()); err == nil {
		status.Global, _ = tv.Get(tool)
	}
	r := a.Resolver.Resolve(tool, dir)
	status.Current, status.Source = r.Version, r.Source
	if r.Error != "" {
		status.Warnings = append(status.Warnings, r.Error)
	}
	if status.Global != "" && r.Source != "全局" && r.Source != "" {
		status.Warnings = append(status.Warnings, fmt.Sprintf("全局默认 %s；当前目录由 %s 指定 %s，优先于全局设置", status.Global, r.Source, r.Spec))
	}
	if r.Version == "" {
		if r.Spec != "" && r.Error == "" {
			status.Warnings = append(status.Warnings, a.unavailableVersion(tool, r).Error())
		}
		return status
	}
	status.Executable, _ = a.Which(tool, dir)
	names := plugin.DetectCommands()[tool]
	if len(names) == 0 {
		names = []string{tool}
	}
	status.Command, _ = exec.LookPath(names[0])
	if status.Command == "" {
		status.Warnings = append(status.Warnings, "当前进程 PATH 中找不到 "+names[0]+"；请点击「激活 PATH」，并在终端执行激活命令")
	} else if !sameCommandFile(status.Command, status.Executable) && !sameDir(filepath.Dir(status.Command), a.Paths.Shims()) {
		hint := "请点击「激活 PATH」配置终端启动钩子"
		if runtime.GOOS == "windows" {
			hint = "请点击「激活 PATH」配置 cmd / PowerShell"
		}
		status.Warnings = append(status.Warnings, "GUI 启动时的 PATH 优先命中 "+status.Command+"；这不代表已激活终端的实际结果。若终端仍使用该程序，"+hint+"；已通过 VSM shim 的终端后续切换无需重开")
	}
	return status
}

func sameCommandFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	return ea == nil && eb == nil && os.SameFile(fa, fb)
}

// SetGlobalVersion updates the config only after shim generation succeeds. The
// shim must point to this CLI, rather than a previously moved or outdated copy.
// CLI callers may select an uninstalled version and receive a separate warning.
func (a Activator) SetGlobalVersion(tool, version string) (err error) {
	if a.TraceID == "" {
		a.TraceID = logging.NewOperationID()
	}
	fields := a.activationFields()
	fields["tool"], fields["requested_version"] = tool, version
	finish := logging.Start("activation", "global.save", fields)
	defer func() { finish(err) }()
	if a.Paths.ProjectMode == "local" || a.Paths.ProjectMode == "docker" {
		return fmt.Errorf("隔离项目请设置项目版本，不使用全局默认")
	}
	if _, ok := plugin.Get(tool); !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	if strings.TrimSpace(version) != version || version == "" || strings.ContainsAny(version, " \t\r\n\x00") {
		return fmt.Errorf("版本号不能为空或包含空白字符")
	}
	if err := a.Paths.EnsureDirs(); err != nil {
		return err
	}
	if a.VsmExe == "" {
		return fmt.Errorf("未找到配套 VSM CLI，请将新版 CLI 与 GUI 放在同一目录后重试")
	}
	shimFinish := logging.Start("activation", "shims.generate", fields)
	shimErr := a.GenerateShims()
	shimFinish(shimErr)
	if shimErr != nil {
		return fmt.Errorf("生成激活 shim 失败，全局配置未修改: %w", shimErr)
	}
	configFinish := logging.Start("activation", "global.config.write", fields)
	err = config.SetIn(a.Paths.GlobalToolVersions(), tool, version)
	configFinish(err)
	return err
}
