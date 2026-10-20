package core

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"vsm/internal/plugin"
)

func findCmdCommand(dirs []string, name string, extensions []string) string {
	for _, dir := range dirs {
		for _, ext := range extensions {
			file := filepath.Join(dir, name+ext)
			if fileExists(file) {
				return file
			}
		}
	}
	return ""
}

func probeCmdLookupVersion(tool, name string, env []string, dir string) (string, error) {
	if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._+-") != "" {
		return "", fmt.Errorf("无法安全验证 cmd 命令名")
	}
	file, err := os.CreateTemp("", "vsm-command-check-*.cmd")
	if err != nil {
		return "", err
	}
	namePath := file.Name()
	defer os.Remove(namePath)
	if _, err = file.WriteString("@echo off\r\n@call " + name + " %*\r\n"); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	// Deliberately invoke the unqualified command as the user does. Executing a
	// full java.cmd path would bypass cmd's current-directory/PATHEXT lookup.
	return probeVersionCommand(tool, namePath, env, dir)
}

// cmdActivationEnvironment exercises the managed helper in either cmd mode.
// /D avoids executing unrelated AutoRun commands; it does not test whether an
// arbitrary terminal launcher will execute the user's AutoRun registration.
func (a Activator) cmdActivationEnvironment(dir, mode string) ([]string, string, error) {
	command, err := CmdActivationCommand(a.Paths.Root)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, windowsCmdPath())
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VSM_HOME="+a.Paths.Root)
	hideConsole(cmd)
	cmd.SysProcAttr.CmdLine = `"` + windowsCmdPath() + `" /D /U /V:` + mode + ` /S /C "` + command + ` >nul & set PATH"`
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("cmd /V:%s 激活验证失败: %w", mode, err)
	}
	if len(out)%2 != 0 {
		return nil, "", fmt.Errorf("cmd /V:%s 返回无效的 Unicode PATH", mode)
	}
	units := make([]uint16, len(out)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(out[i*2:])
	}
	var dirs []string
	var pathExt string
	for _, line := range strings.Split(string(utf16.Decode(units)), "\n") {
		if key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "="); ok && strings.EqualFold(key, "PATH") {
			dirs = filepath.SplitList(value)
		}
		if key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "="); ok && strings.EqualFold(key, "PATHEXT") {
			pathExt = value
		}
	}
	if len(dirs) == 0 || !sameDir(dirs[0], a.Paths.Shims()) {
		return nil, "", fmt.Errorf("cmd /V:%s 激活后 VSM shims 仍未排在 PATH 最前面", mode)
	}
	return dirs, pathExt, nil
}

// VerifyCmdActivation checks helper behavior in both delayed-expansion modes,
// then executes configured commands using the resulting environment.
func (a Activator) VerifyCmdActivation(dir string) (CmdActivationCheck, error) {
	check := CmdActivationCheck{Passed: true, Tools: []CmdToolActivation{}}
	var dirs []string
	for _, mode := range []string{"OFF", "ON"} {
		paths, extensions, err := a.cmdActivationEnvironment(dir, mode)
		if err != nil {
			return check, err
		}
		if dirs == nil {
			dirs, check.PathExt = paths, extensions
		} else if strings.Join(dirs, ";") != strings.Join(paths, ";") || check.PathExt != extensions {
			return check, fmt.Errorf("cmd /V:OFF 与 /V:ON 的激活环境不一致")
		}
		check.CmdModes = append(check.CmdModes, mode)
	}
	extensions := windowsExecutableExts(check.PathExt)
	check.PathExtRepaired = !hasCMDExtension(os.Getenv("PATHEXT")) && hasCMDExtension(check.PathExt)
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
		row.Command = findCmdCommand(append([]string{dir}, dirs...), names[0], extensions)
		var err error
		row.Executable, err = a.Which(tool, dir)
		if err != nil {
			row.Error = err.Error()
		}
		shim := filepath.Join(a.Paths.Shims(), names[0]+".cmd")
		row.Passed = row.Error == "" && row.Command != "" && (sameCommandFile(row.Command, shim) || sameCommandFile(row.Command, row.Executable))
		if row.Passed {
			row.ExpectedVersion, err = probeVersion(tool, row.Executable)
			if err != nil {
				row.Error = "无法执行所选程序读取版本: " + err.Error()
				row.Passed = false
			} else {
				env := append(os.Environ(), "VSM_HOME="+a.Paths.Root, "PATH="+strings.Join(dirs, string(os.PathListSeparator)), "PATHEXT="+check.PathExt)
				row.ActualVersion, err = probeCmdLookupVersion(tool, names[0], env, dir)
				if err != nil {
					row.Error = "经 shim 执行失败: " + err.Error()
					row.Passed = false
				} else if row.ActualVersion != row.ExpectedVersion {
					row.Error = fmt.Sprintf("经 shim 返回 %s，所选程序返回 %s", row.ActualVersion, row.ExpectedVersion)
					row.Passed = false
				}
			}
		}
		if !row.Passed && row.Error == "" {
			switch {
			case row.Command != "" && sameDir(filepath.Dir(row.Command), dir):
				row.Code = "current-directory-override"
				row.Error = "当前工作目录中的同名程序优先于版本管理器，版本尚未切换"
				row.Action = "在不含该程序的项目目录执行命令，或确认用途后自行移动该同名文件。VSM 不会删除原程序。"
			case row.Command != "" && sameDir(filepath.Dir(row.Command), a.Paths.Shims()):
				row.Code = "shim-shadowed"
				row.Error = "VSM 入口目录中存在优先级更高的同名程序，版本尚未切换"
				row.Action = "确认实际命中的文件用途后移出该入口目录，再点击「激活 PATH」。VSM 不会删除该文件。"
			case !fileExists(shim):
				row.Code = "shim-missing"
				row.Error = "版本管理器的命令入口没有生成，版本尚未切换"
				row.Action = "检查安全软件是否拦截写入，允许 VSM 写入数据目录后重试「激活 PATH」。"
			default:
				row.Code = "command-bypassed"
				row.Error = "系统仍优先运行原来的程序，版本尚未切换"
				row.Action = "点击「重新验证」；若仍失败，可复制下方诊断详情反馈，包含实际命中的程序和所选程序。"
			}
		}
		if !row.Passed && row.Code == "" {
			row.Code = "runtime-failed"
			row.Action = "检查下方所选程序是否可以运行；可重新检测并纳管正确版本后重试。"
		}
		check.Passed = check.Passed && row.Passed
		check.Tools = append(check.Tools, row)
	}
	return check, nil
}

func hasCMDExtension(value string) bool {
	for _, extension := range strings.Split(value, ";") {
		if strings.EqualFold(strings.TrimSpace(extension), ".CMD") {
			return true
		}
	}
	return false
}
