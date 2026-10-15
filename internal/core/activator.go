package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
	for _, tool := range tools {
		if _, ok := plugin.Get(tool); !ok {
			continue
		}
		versions, err := a.Registry.Installed(tool)
		if err != nil {
			return err
		}
		if len(versions) == 0 {
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
		return os.WriteFile(filepath.Join(a.Paths.Shims(), tool+".cmd"), []byte(content), 0o644)
	}
	// 单引号字面量：路径里的 $、反引号、空格都不会被 shell 解释
	content := "#!/bin/sh\nexec " + quotePOSIX(vsm) + " exec " + tool + " \"$@\"\n"
	return os.WriteFile(filepath.Join(a.Paths.Shims(), tool), []byte(content), 0o755)
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
