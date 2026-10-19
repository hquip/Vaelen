package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

// Activator 把“当前应使用的版本”落实为 PATH/env，并管理 shim 与 shell hook。
type Activator struct {
	Paths    Paths
	Resolver Resolver
	Registry Registry
	// VsmExe 是 vsm CLI 可执行文件的绝对路径；shim 用它调用 `vsm exec`，
	// 这样即使 vsm 不在 PATH 里垫片也能工作。为空时回退为裸命令 "vsm"。
	VsmExe string
}

// Which 返回 tool 当前解析版本下主命令的完整路径。
func (a Activator) Which(tool, startDir string) (string, error) {
	p, ok := plugin.Get(tool)
	if !ok {
		return "", fmt.Errorf("不支持的工具: %s", tool)
	}
	res := a.Resolver.Resolve(tool, startDir)
	if res.Error != "" {
		return "", fmt.Errorf("%s", res.Error)
	}
	if res.Version == "" {
		return "", fmt.Errorf("%s 未找到可用版本", tool)
	}
	installDir := a.Paths.InstallDir(tool, res.Version)
	exe := ""
	for _, name := range plugin.DetectCommands()[tool] {
		if exe = findExecutable(p.BinPaths(installDir), name); exe != "" {
			break
		}
	}
	if exe == "" {
		return "", fmt.Errorf("在 %s %s 中找不到 %s 可执行文件", tool, res.Version, tool)
	}
	return exe, nil
}

// Run 在 tool 当前解析版本的环境下执行 cmdName + cmdArgs。
// cmdName 通常是工具主命令（如 go），也可以是该工具环境附带的其它命令（如 npm）。
func (a Activator) Run(tool, startDir, cmdName string, cmdArgs []string) error {
	if a.Paths.ProjectMode == "local" || a.Paths.ProjectMode == "docker" {
		if cmdName == tool {
			if names := plugin.DetectCommands()[tool]; len(names) > 0 {
				cmdName = names[0]
			}
		}
		return a.RunProject(startDir, cmdName, cmdArgs)
	}
	p, ok := plugin.Get(tool)
	if !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	res := a.Resolver.Resolve(tool, startDir)
	if res.Error != "" {
		return fmt.Errorf("%s", res.Error)
	}
	if res.Version == "" {
		return fmt.Errorf("%s 未找到可用版本（要求 %q），请先 vsm install / vsm use", tool, res.Spec)
	}
	installDir := a.Paths.InstallDir(tool, res.Version)
	binDirs := p.BinPaths(installDir)

	exe := findExecutable(binDirs, cmdName)
	if exe == "" && cmdName == tool {
		for _, name := range plugin.DetectCommands()[tool] {
			if exe = findExecutable(binDirs, name); exe != "" {
				break
			}
		}
	}
	if exe == "" {
		return fmt.Errorf("在 %s %s 中找不到可执行文件 %q", tool, res.Version, cmdName)
	}

	cmd := exec.Command(exe, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv(binDirs, p.ExecEnv(installDir))
	if err := cmd.Run(); err != nil {
		// 透传子进程退出码：`vsm exec go test ./...` 失败时必须让脚本/CI 感知真实状态码，
		// 否则 `vsm exec ... && deploy` 这类串联会失效，lint 用 2、git 用 128 也全被抹平。
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return exitCodeError{code: ee.ExitCode(), err: err}
		}
		return err
	}
	return nil
}

// exitCodeError 携带子进程退出码，供 CLI 层透传给 os.Exit。
type exitCodeError struct {
	code int
	err  error
}

func (e exitCodeError) Error() string { return e.err.Error() }
func (e exitCodeError) Unwrap() error { return e.err }

// ExitCode 实现 cli 层的退出码透传约定。
func (e exitCodeError) ExitCode() int { return e.code }

// GenerateShims 为所有已安装工具生成 shim（无 shell hook 环境下的兜底激活）。
func (a Activator) GenerateShims() error {
	if err := os.MkdirAll(a.Paths.Shims(), 0o755); err != nil {
		return err
	}
	tools, err := a.Registry.Tools()
	if err != nil {
		return err
	}
	owners := map[string]string{}
	for _, tool := range tools {
		if plugin.IsServiceTool(tool) {
			if err := a.removeServiceShims(tool); err != nil {
				return err
			}
			continue
		}
		if _, ok := plugin.Get(tool); !ok {
			continue
		}
		versions, err := a.Registry.Installed(tool)
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			for _, command := range plugin.DetectCommands()[tool] {
				if command == tool {
					continue
				}
				name := command
				if runtime.GOOS == "windows" {
					name += ".cmd"
				}
				file := filepath.Join(a.Paths.Shims(), name)
				data, err := config.ReadBounded(file, 64<<10)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				if err == nil && strings.Contains(string(data), " exec "+tool+" -- ") {
					if err := os.Remove(file); err != nil {
						return err
					}
				}
			}
			name := tool
			if runtime.GOOS == "windows" {
				name += ".cmd"
			}
			if err := os.Remove(filepath.Join(a.Paths.Shims(), name)); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := a.writeShim(tool); err != nil {
			return err
		}
		for _, command := range plugin.DetectCommands()[tool] {
			key := command
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if owner := owners[key]; owner != "" && owner != tool {
				return fmt.Errorf("command %s belongs to both %s and %s", command, owner, tool)
			}
			owners[key] = tool
			if command == tool {
				continue
			}
			if _, registered := plugin.Get(command); registered && command != tool {
				return fmt.Errorf("command alias %s conflicts with another registered tool", command)
			}
			if err := a.writeCommandShim(tool, command); err != nil {
				return err
			}
		}

	}
	return nil
}

func (a Activator) writeShim(tool string) error {
	vsm := a.VsmExe
	if vsm == "" {
		vsm = "vsm"
	}
	if runtime.GOOS == "windows" {
		// .cmd 里 % 会被展开，路径中的 % 需写成 %%
		content := "@echo off\r\n\"" + quoteCmd(vsm) + "\" exec " + tool + " %*\r\n"
		return a.saveShim(tool+".cmd", content, 0o644)
	}
	// 单引号字面量：路径里的 $、反引号、空格都不会被 shell 解释
	content := "#!/bin/sh\nexec " + quotePOSIX(vsm) + " exec " + tool + " \"$@\"\n"
	return a.saveShim(tool, content, 0o755)
}

// Services are started explicitly through db; registering their programs must
// not place an unconfigured proxy ahead of an existing system database client.
func (a Activator) removeServiceShims(tool string) error {
	names := append([]string{tool}, plugin.DetectCommands()[tool]...)
	for _, name := range names {
		if runtime.GOOS == "windows" {
			name += ".cmd"
		}
		path := filepath.Join(a.Paths.Shims(), name)
		data, err := config.ReadBounded(path, 64<<10)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		text := string(data)
		if (strings.HasPrefix(text, "@echo off") || strings.HasPrefix(text, "#!/bin/sh")) && strings.Contains(text, " exec "+tool+" ") {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a Activator) writeCommandShim(tool, command string) error {
	if err := validateComponent(command); err != nil {
		return err
	}
	vsm := a.VsmExe
	if vsm == "" {
		vsm = "vsm"
	}
	if runtime.GOOS == "windows" {
		content := "@echo off\r\n\"" + quoteCmd(vsm) + "\" exec " + tool + " -- " + command + " %*\r\n"
		return a.saveShim(command+".cmd", content, 0o644)
	}
	content := "#!/bin/sh\nexec " + quotePOSIX(vsm) + " exec " + tool + " -- " + quotePOSIX(command) + " \"$@\"\n"
	return a.saveShim(command, content, 0o755)
}

func (a Activator) saveShim(name, content string, mode os.FileMode) error {
	root, err := os.OpenRoot(a.Paths.Shims())
	if err != nil {
		return err
	}
	defer root.Close()
	return config.WriteRootAtomic(root, name, []byte(content), mode)
}

func findExecutable(dirs []string, name string) string {
	if err := validateComponent(name); err != nil {
		return ""
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{name + ".exe", name + ".cmd", name + ".bat", name}
	} else {
		candidates = []string{name}
	}
	for _, dir := range dirs {
		for _, c := range candidates {
			full := filepath.Join(dir, c)
			if fileExists(full) {
				return full
			}
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// buildEnv 复制当前环境，把 pathDirs 前置到 PATH，并叠加 extra 环境变量。
func buildEnv(pathDirs []string, extra map[string]string) []string {
	if _, hasGo := extra["GOROOT"]; hasGo {
		cloned := map[string]string{}
		for k, v := range extra {
			cloned[k] = v
		}
		cloned["GOTOOLCHAIN"] = "local"
		extra = cloned
	}
	sep := string(os.PathListSeparator)
	prefix := strings.Join(pathDirs, sep)
	env := make([]string, 0, len(os.Environ())+len(extra))
	pathSet := false
	for _, e := range os.Environ() {
		if k, v, ok := cutEnv(e); ok && strings.EqualFold(k, "PATH") {
			env = append(env, k+"="+prefix+sep+v)
			pathSet = true
		} else {
			overridden := false
			for key := range extra {
				if original, _, ok := cutEnv(e); ok && (original == key || runtime.GOOS == "windows" && strings.EqualFold(original, key)) {
					overridden = true
					break
				}
			}
			if overridden {
				continue
			}
			env = append(env, e)
		}
	}
	if !pathSet {
		env = append(env, "PATH="+prefix)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func cutEnv(e string) (key, val string, ok bool) {
	i := strings.IndexByte(e, '=')
	if i < 0 {
		return "", "", false
	}
	return e[:i], e[i+1:], true
}

// shell 激活 / 进目录自动切换脚本见 shellenv.go（HookScript / EnvScript / EnvFor）。
