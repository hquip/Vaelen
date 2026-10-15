package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"vsm/internal/plugin"
)

// RunProject selects the complete project toolchain for one child process.
// No shell command string is evaluated and the parent/project config is unchanged.
func (a Activator) RunProject(dir, command string, args []string) error {
	plan, err := a.PlanProjectEnvironment(dir)
	if err != nil {
		return err
	}
	if err := validateComponent(command); err != nil {
		return fmt.Errorf("run requires a command name (for example go, node or python): %w", err)
	}
	program := findExecutable(plan.Paths, command)
	for _, r := range plan.Versions {
		for _, name := range plugin.DetectCommands()[r.Tool] {
			if command == name {
				p, _ := plugin.Get(r.Tool)
				program = findExecutable(p.BinPaths(a.Paths.InstallDir(r.Tool, r.Version)), command)
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
