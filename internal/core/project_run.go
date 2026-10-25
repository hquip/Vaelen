package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"vsm/internal/config"
	"vsm/internal/mcpserver"
	"vsm/internal/plugin"
)

// RunProject selects the complete project toolchain for one child process.
// No shell command string is evaluated and the parent/project config is unchanged.
func (a Activator) RunProject(dir, command string, args []string) error {
	return a.RunProjectContext(context.Background(), dir, command, args, os.Stdin, os.Stdout, os.Stderr)
}
func (a Activator) RunProjectContext(ctx context.Context, dir, command string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return a.runProjectEnvironment(ctx, dir, command, args, stdin, stdout, stderr, nil)
}
func (a Activator) RunProjectEnvironment(dir, command string, args []string, extra map[string]string) error {
	return a.runProjectEnvironment(context.Background(), dir, command, args, os.Stdin, os.Stdout, os.Stderr, extra)
}
func (a Activator) runProjectEnvironment(ctx context.Context, dir, command string, args []string, stdin io.Reader, stdout, stderr io.Writer, extra map[string]string) error {
	for k, v := range extra {
		if !config.ValidUserEnv(k, v) {
			return fmt.Errorf("unsafe run environment key %s", k)
		}
	}

	project, err := FindProject(dir)
	if err != nil {
		return err
	}
	if project != nil && project.Mode == "docker" {
		vars := map[string]string{}
		workspace, err := FindWorkspace(dir)
		if err != nil {
			return err
		}
		if workspace != nil {
			vars, err = workspace.Environment()
			if err != nil {
				return err
			}
		}
		for k, v := range extra {
			vars[k] = v
		}
		return project.runDockerEnvironment(ctx, dir, command, args, vars, stdin, stdout, stderr)
	}
	if err := a.PrepareProject(ctx, dir); err != nil {
		return err
	}
	plan, err := a.PlanProjectEnvironment(dir)
	if err != nil {
		return err
	}
	if err := validateComponent(command); err != nil {
		return fmt.Errorf("run requires a command name (for example go, node or python): %w", err)
	}
	program := findExecutable(plan.Paths, command)
	identity := command
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
		switch filepath.Ext(identity) {
		case ".exe", ".cmd", ".bat":
			identity = strings.TrimSuffix(identity, filepath.Ext(identity))
		}
	}
	matchesCommand := func(name string) bool {
		if runtime.GOOS == "windows" {
			return identity == strings.ToLower(name)
		}
		return command == name
	}
	if project != nil && project.Mode == "local" {
		for tool, names := range plugin.DetectCommands() {
			for _, name := range names {
				if matchesCommand(name) {
					configured := false
					for _, r := range plan.Versions {
						if r.Tool == tool {
							configured = true
							break
						}
					}
					if !configured {
						return fmt.Errorf("项目未配置 %s；先执行 vsm use %s <版本> 并在项目环境中安装", tool, tool)
					}
				}
			}
		}
	}
	for _, r := range plan.Versions {
		for _, name := range plugin.DetectCommands()[r.Tool] {
			if matchesCommand(name) {
				p, ok := plugin.Get(r.Tool)
				if !ok {
					return fmt.Errorf("工具定义 %s 已变更，请重试", r.Tool)
				}
				dirs := p.BinPaths(a.Paths.InstallDir(r.Tool, r.Version))
				if project != nil && project.Mode == "local" && r.Tool == "python" {
					dirs = []string{project.pythonVenvBin(r.Version)}
				}
				program = findExecutable(dirs, command)
				if program == "" {
					return fmt.Errorf("%s %s is missing executable %s; refusing system fallback", r.Tool, r.Version, command)
				}
			}
		}
	}
	if program == "" {
		program, err = exec.LookPath(command)
		if err != nil {
			return err
		}
	}
	for _, r := range plan.Versions {
		plan.Variables["VSM_"+strings.ToUpper(r.Tool)+"_VERSION"] = r.Version
	}
	plan.Variables["_VSM_ACTIVE_VERSIONS"] = plan.Label()
	for k, v := range extra {
		plan.Variables[k] = v
	}
	cmd, err := projectCommandContext(ctx, program, args)
	if err != nil {
		return err
	}
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = buildEnv(plan.Paths, plan.Variables)
	if ctx.Done() != nil {
		mcpserver.ConfigureProcess(cmd)
		cmd.WaitDelay = 2 * time.Second
	}
	for _, r := range plan.Versions {
		if r.Tool == "rebol" {
			for _, name := range plugin.DetectCommands()[r.Tool] {
				if matchesCommand(name) {
					if stdin == nil {
						cmd.Stdin = strings.NewReader("")
					}
				}
			}
		}
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return exitCodeError{ee.ExitCode(), err}
		}
		return err
	}
	return nil
}
