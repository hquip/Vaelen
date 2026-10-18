package cli

import (
	"fmt"
	"os"
	"strings"
	"vsm/internal/config"
	"vsm/internal/core"
	"vsm/internal/plugin"
)

func (a *app) cmdRun(args []string) error {
	installMissing := false
	locked := false
	extraEnv := map[string]string{}
	commandStart := -1
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			commandStart = i + 1
			break
		}
		switch arg {
		case "--env":
			if i+1 >= len(args) {
				return fmt.Errorf("--env requires KEY=VALUE")
			}
			i++
			key, value, ok := strings.Cut(args[i], "=")
			if !ok || !config.ValidUserEnv(key, value) {
				return fmt.Errorf("unsafe --env assignment")
			}
			extraEnv[key] = value
		case "--locked":
			locked = true
		case "--install-missing":
			installMissing = true
		case "--require-checksum":
		default:
			return fmt.Errorf("用法: vsm run [--install-missing] -- <command> [args...]")
		}
	}
	if commandStart < 0 || commandStart >= len(args) {
		return fmt.Errorf("用法: vsm run [--install-missing] -- <command> [args...]")
	}
	if locked {
		if err := a.useLock(installMissing); err != nil {
			return err
		}
		return a.act.RunProjectEnvironment(a.cwd, args[commandStart], args[commandStart+1:], extraEnv)
	}
	project, err := core.FindProject(a.cwd)
	if err != nil {
		return err
	}
	if project != nil && project.Mode == "docker" {
		return a.act.RunProjectEnvironment(a.cwd, args[commandStart], args[commandStart+1:], extraEnv)
	}
	if installMissing {
		for _, tool := range plugin.Names() {
			spec, src, err := core.ProjectRequirement(tool, a.cwd)
			if err != nil {
				return err
			}
			if src == "" {
				r := a.res.Resolve(tool, a.cwd)
				if r.Error != "" {
					return fmt.Errorf("%s", r.Error)
				}
				spec = r.Spec
			}
			if spec == "" {
				continue
			}
			installed, err := a.reg.Installed(tool)
			if err != nil {
				return err
			}
			if core.BestMatch(spec, installed) != "" {
				continue
			}
			p, _ := plugin.Get(tool)
			version, err := resolveRemote(p, spec)
			if err != nil {
				return err
			}
			installer := a.inst
			installer.RequireChecksum = true
			if err := installer.Install(p, version, "", func(s string) { fmt.Fprintln(os.Stderr, "[vsm]", s) }); err != nil {
				return err
			}
		}
		if err := a.act.GenerateShims(); err != nil {
			return err
		}
	}
	return a.act.RunProjectEnvironment(a.cwd, args[commandStart], args[commandStart+1:], extraEnv)
}
