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
	"vsm/internal/logging"
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
	// TraceID connects one GUI/CLI action to its core diagnostic steps.
	TraceID string
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
		return "", a.unavailableVersion(tool, res)
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
	managedDirs, managedEnv, envErr := a.runtimeEnvironment(tool, startDir, installDir, p, binDirs)
	if envErr != nil {
		return envErr
	}
	cmd.Env = buildEnv(managedDirs, managedEnv)
	fields := a.activationFields()
	if a.TraceID == "" {
		operationID := os.Getenv("VSM_ACTIVATION_OPERATION")
		if operationID == "" {
			operationID = logging.NewOperationID()
		}
		fields["operation_id"] = operationID
	}
	fields["scope"] = "runtime-process"
	if os.Getenv("_VSM_CMD_SCOPE") == "controlled-cmd" {
		fields["scope"] = "controlled-cmd"
	}
	fields["tool"], fields["selected_version"], fields["source"] = tool, res.Version, res.Source
	fields["selected_executable"], fields["directory"], fields["parent_pid"] = exe, startDir, os.Getppid()
	finish := logging.Start("activation", "runtime.dispatch", fields)
	if err := cmd.Run(); err != nil {
		finish(err)
		// 透传子进程退出码：`vsm exec go test ./...` 失败时必须让脚本/CI 感知真实状态码，
		// 否则 `vsm exec ... && deploy` 这类串联会失效，lint 用 2、git 用 128 也全被抹平。
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return exitCodeError{code: ee.ExitCode(), err: err}
		}
		return err
	}
	finish(nil)
	return nil
}

// runtimeEnvironment adds companion runtimes required by a tool. Maven is a
// Java application: setting MAVEN_HOME alone is insufficient because mvn.cmd
// refuses to start when JAVA_HOME is missing and can otherwise pick a different
// JDK from the inherited PATH.
func (a Activator) runtimeEnvironment(tool, startDir, installDir string, p plugin.Plugin, binDirs []string) ([]string, map[string]string, error) {
	dirs := append([]string(nil), binDirs...)
	env := cloneEnv(p.ExecEnv(installDir))
	if tool != "maven" {
		return dirs, env, nil
	}
	java, ok := plugin.Get("java")
	if !ok {
		return dirs, env, fmt.Errorf("Maven 需要 Java，但 VSM 未加载 Java 工具定义")
	}
	javaRes := a.Resolver.Resolve("java", startDir)
	if javaRes.Error != "" {
		return nil, nil, fmt.Errorf("Maven 依赖的 Java 版本解析失败: %s", javaRes.Error)
	}
	if javaRes.Version == "" {
		// A user may intentionally use a system JDK. Preserve that supported
		// fallback, while making the missing VSM selection diagnosable by the
		// Maven launcher itself instead of silently selecting another VSM JDK.
		return dirs, env, nil
	}
	javaDir := a.Paths.InstallDir("java", javaRes.Version)
	dirs = append(java.BinPaths(javaDir), dirs...)
	for key, value := range java.ExecEnv(javaDir) {
		env[key] = value
	}
	return dirs, env, nil
}

func cloneEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
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
	// Each entry belongs to the configuration directory that generated it.
	// Never let an unrelated VSM_HOME inherited by a long-running terminal
	// silently redirect that entry to another configuration and version set.
	root, err := filepath.Abs(a.Paths.Root)
	if err != nil {
		return err
	}
	if a.Paths.Root == "" {
		return fmt.Errorf("生成 shim 需要明确的 VSM 数据目录")
	}
	a.Paths.Root = root
	if err := os.MkdirAll(a.Paths.Shims(), 0o755); err != nil {
		return err
	}
	if a.VsmExe != "" {
		if err := a.GenerateCLIShim(); err != nil {
			return err
		}
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

// GenerateCLIShim exposes the paired CLI even before any tools are installed.
// Command registration must not regenerate language shims or inspect projects.
func (a Activator) GenerateCLIShim() error {
	if a.Paths.Root == "" || a.VsmExe == "" {
		return fmt.Errorf("生成 vsm 命令入口需要明确的数据目录和 CLI 路径")
	}
	root, err := filepath.Abs(a.Paths.Root)
	if err != nil {
		return err
	}
	a.Paths.Root = root
	if sameDir(filepath.Dir(a.VsmExe), a.Paths.Shims()) {
		return fmt.Errorf("CLI 不能位于 shims 目录，否则会递归调用自身")
	}
	if err := os.MkdirAll(a.Paths.Shims(), 0o755); err != nil {
		return err
	}
	// Keep vsm on the same dispatcher as language shims, including when the
	// official download retains its vsm-windows-amd64.exe filename.
	name, content := "vsm", a.shimPreamble()+"exec "+quotePOSIX(a.VsmExe)+" \"$@\"\n"
	if runtime.GOOS == "windows" {
		name = "vsm.cmd"
		content = a.shimPreamble() + "\"" + quoteCmd(a.VsmExe) + "\" %*\r\n"
	}
	return a.saveShim(name, content, 0o755)
}

func (a Activator) writeShim(tool string) error {
	name, content := a.shimScript(tool, "")
	return a.saveShim(name, content, 0o755)
}

// The same renderer is used when writing and recognizing an owned dispatcher.
func (a Activator) shimScript(tool, command string) (name, content string) {
	vsm := a.VsmExe
	if vsm == "" {
		vsm = "vsm"
	}
	name = tool
	if command != "" {
		name = command
	}
	if runtime.GOOS == "windows" {
		// .cmd 里 % 会被展开，路径中的 % 需写成 %%
		args := tool
		if command != "" {
			args += " -- " + command
		}
		return name + ".cmd", a.shimPreamble() + "\"" + quoteCmd(vsm) + "\" exec " + args + " %*\r\n"
	}
	// 单引号字面量：路径里的 $、反引号、空格都不会被 shell 解释
	args := tool
	if command != "" {
		args += " -- " + quotePOSIX(command)
	}
	return name, a.shimPreamble() + "exec " + quotePOSIX(vsm) + " exec " + args + " \"$@\"\n"
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
	name, content := a.shimScript(tool, command)
	return a.saveShim(name, content, 0o755)
}

func (a Activator) shimPreamble() string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\nsetlocal DisableDelayedExpansion\r\nset \"VSM_HOME=" + quoteCmd(a.Paths.Root) + "\"\r\n"
	}
	return "#!/bin/sh\nexport VSM_HOME=" + quotePOSIX(a.Paths.Root) + "\n"
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
			if executableFile(full) {
				return full
			}
		}
	}
	return ""
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	_, err = exec.LookPath(abs)
	return err == nil
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
