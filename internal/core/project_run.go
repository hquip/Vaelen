package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"vsm/internal/plugin"
)

// RunProject selects the complete project toolchain for one child process.
// No shell command string is evaluated and the parent/project config is unchanged.
func (a Activator) RunProject(dir, command string, args []string) error {
	project, err := FindProject(dir)
	if err != nil {
		return err
	}
	if project != nil && project.Mode == "docker" {
		return project.RunDocker(context.Background(), dir, command, args)
	}
	if err := a.PrepareProject(context.Background(), dir); err != nil {
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
	if project != nil && project.Mode == "local" {
		for tool, names := range plugin.DetectCommands() {
			for _, name := range names {
				if identity == strings.ToLower(name) || runtime.GOOS != "windows" && command == name {
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
			if identity == strings.ToLower(name) || runtime.GOOS != "windows" && command == name {
				p, _ := plugin.Get(r.Tool)
				dirs := p.BinPaths(a.Paths.InstallDir(r.Tool, r.Version))
				if project != nil && project.Mode == "local" && r.Tool == "python" {
					bin := "bin"
					if os.PathSeparator == '\\' {
						bin = "Scripts"
					}
					dirs = append([]string{project.PythonVenv(r.Version) + string(os.PathSeparator) + bin}, dirs...)
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
	cmd, err := projectCommand(program, args)
	if err != nil {
		return err
	}
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv(plan.Paths, plan.Variables)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return exitCodeError{ee.ExitCode(), err}
		}
		return err
	}
	return nil
}
