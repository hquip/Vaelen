package core

import (
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"vsm/internal/plugin"
)

// VerifyCmdActivation discovers all configured commands automatically. /D keeps
// this probe from running unrelated user AutoRun commands; loading our exact
// managed helper exercises the PATH setup without modifying user processes.
func (a Activator) VerifyCmdActivation(dir string) (CmdActivationCheck, error) {
	check := CmdActivationCheck{Passed: true, Tools: []CmdToolActivation{}}
	command, err := CmdActivationCommand(a.Paths.Root)
	if err != nil {
		return check, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, windowsCmdPath())
	cmd.Dir = dir
	hideConsole(cmd)
	cmd.SysProcAttr.CmdLine = `"` + windowsCmdPath() + `" /D /U /V:OFF /S /C "` + command + ` >nul & set PATH"`
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return check, fmt.Errorf("cmd 激活验证失败: %w", err)
	}
	if len(out)%2 != 0 {
		return check, fmt.Errorf("cmd 返回无效的 Unicode PATH")
	}
	units := make([]uint16, len(out)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(out[i*2:])
	}
	var dirs []string
	for _, line := range strings.Split(string(utf16.Decode(units)), "\n") {
		if key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "="); ok && strings.EqualFold(key, "PATH") {
			dirs = filepath.SplitList(value)
			break
		}
	}
	if len(dirs) == 0 || !sameDir(dirs[0], a.Paths.Shims()) {
		return check, fmt.Errorf("cmd 激活后 VSM shims 仍未排在 PATH 最前面")
	}
	for _, tool := range plugin.Names() {
		if plugin.IsServiceTool(tool) {
			continue
		}
		r := a.Resolver.Resolve(tool, dir)
		if r.Spec == "" && r.Error == "" {
			continue
		}
		row := CmdToolActivation{Tool: tool, Version: r.Version, Source: r.Source, Error: r.Error}
		names := plugin.DetectCommands()[tool]
		if len(names) == 0 {
			names = []string{tool}
		}
		row.Before, _ = exec.LookPath(names[0])
		// cmd searches the working directory before PATH. A local java.exe must
		// be reported as an override rather than a successful shim activation.
		row.Command = findExecutable(append([]string{dir}, dirs...), names[0])
		row.Executable, err = a.Which(tool, dir)
		if err != nil {
			row.Error = err.Error()
		}
		row.Passed = row.Error == "" && row.Command != "" && (sameDir(filepath.Dir(row.Command), a.Paths.Shims()) || sameCommandFile(row.Command, row.Executable))
		if !row.Passed && row.Error == "" {
			row.Error = "命令未经过 VSM，请检查 shim 或当前目录中的同名程序"
		}
		check.Passed = check.Passed && row.Passed
		check.Tools = append(check.Tools, row)
	}
	return check, nil
}

func (a Activator) OpenActivatedCmd(dir string) error {
	env, err := a.ActivatedTerminalEnvironment(dir)
	if err != nil {
		return fmt.Errorf("准备终端环境失败: %w", err)
	}
	cmd := exec.Command(windowsCmdPath(), "/D", "/V:OFF", "/K")
	cmd.Dir, cmd.Env = dir, env
	// This is an explicit user request for an interactive window. No pipes or
	// console hiding: the shell owns its own console after the GUI exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
