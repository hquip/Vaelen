package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"vsm/internal/plugin"
)

// EnvironmentPlan is computed completely before any shell script is emitted.
type EnvironmentPlan struct {
	Paths     []string
	Variables map[string]string
	Versions  []Resolution
}

func (p EnvironmentPlan) Label() string {
	var labels []string
	for _, r := range p.Versions {
		labels = append(labels, r.Tool+"@"+r.Version)
	}
	return strings.Join(labels, ",")
}

func (a Activator) PlanEnvironment(startDir string) (EnvironmentPlan, error) {
	return a.planEnvironment(startDir, false)
}

// PlanProjectEnvironment explicitly prefers project declarations to inherited overrides.
func (a Activator) PlanProjectEnvironment(startDir string) (EnvironmentPlan, error) {
	return a.planEnvironment(startDir, true)
}

func (a Activator) planEnvironment(startDir string, preferProject bool) (EnvironmentPlan, error) {
	a.Resolver = a.Resolver.Snapshot(startDir)
	plan := EnvironmentPlan{Variables: map[string]string{}}
	project, err := a.Resolver.scan.project()
	if err != nil {
		return plan, err
	}
	if project != nil && project.Mode == "docker" {
		return plan, fmt.Errorf("Docker 项目使用 vsm run 在容器中执行，不能激活宿主机工具链")
	}
	if project != nil {
		for k, v := range project.Variables() {
			plan.Variables[k] = v
		}
	}
	seen := map[string]bool{}
	if registry := plugin.NPMRegistry(); registry != "" {
		plan.Variables["npm_config_registry"] = registry
	}
	for _, tool := range plugin.Names() {
		p, _ := plugin.Get(tool)
		r := a.Resolver.Resolve(tool, startDir)
		if preferProject && a.Resolver.Pinned[tool] == "" {
			spec, source, err := a.Resolver.projectRequirement(tool, startDir)
			if err != nil {
				return EnvironmentPlan{}, err
			}
			if source != "" {
				r = a.Resolver.match(tool, spec, source)
			}
		}
		if r.Error != "" {
			return EnvironmentPlan{}, fmt.Errorf("%s: %s", tool, r.Error)
		}
		if r.Spec != "" && r.Version == "" {
			return EnvironmentPlan{}, fmt.Errorf("%s requires %s but no installed version matches", tool, r.Spec)
		}
		if r.Version == "" {
			continue
		}
		if err := validateInstall(tool, r.Version); err != nil {
			return EnvironmentPlan{}, err
		}
		dir := a.Paths.InstallDir(tool, r.Version)
		for _, bin := range p.BinPaths(dir) {
			if !pathWithin(dir, bin) || strings.ContainsAny(bin, "\x00\r\n") {
				return EnvironmentPlan{}, fmt.Errorf("plugin bin directory escapes installation: %s", tool)
			}
			key := filepath.Clean(bin)
			if os.PathSeparator == '\\' {
				key = strings.ToLower(key)
			}
			if !seen[key] {
				plan.Paths = append(plan.Paths, bin)
				seen[key] = true
			}
		}
		for k, v := range p.ExecEnv(dir) {
			if !validEnvKey(k) || strings.ContainsAny(v, "\x00\r\n") {
				return EnvironmentPlan{}, fmt.Errorf("unsafe plugin environment key %q", k)
			}
			if old, exists := plan.Variables[k]; exists && old != v {
				return EnvironmentPlan{}, fmt.Errorf("conflicting values for environment variable %s", k)
			}
			plan.Variables[k] = v
		}
		plan.Versions = append(plan.Versions, r)
		if project != nil && project.Mode == "local" && tool == "python" {
			venv := project.PythonVenv(r.Version)
			ready, err := project.checkPythonVenv(r.Version)
			if err != nil {
				return EnvironmentPlan{}, err
			}
			if ready {
				plan.Paths = append([]string{project.pythonVenvBin(r.Version)}, plan.Paths...)
				plan.Variables["VIRTUAL_ENV"] = venv
			}
		}
	}
	workspace, err := a.Resolver.scan.findWorkspace()
	if err != nil {
		return EnvironmentPlan{}, err
	}
	if workspace != nil {
		env, err := workspace.Environment()
		if err != nil {
			return EnvironmentPlan{}, err
		}
		for k, v := range env {
			plan.Variables[k] = v
		}
	}
	return plan, nil
}

// ProjectRequirement reports the nearest project declaration, ignoring overrides/global defaults.
func ProjectRequirement(tool, startDir string) (string, string, error) {
	return findToolVersionChecked(tool, startDir)
}
