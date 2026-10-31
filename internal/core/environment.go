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
		p, ok := plugin.Get(tool)
		if !ok {
			return EnvironmentPlan{}, fmt.Errorf("工具定义 %s 已变更，请重试", tool)
		}
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
	// Maven can be selected without an explicit Java entry. If a project or
	// global Java version is available, include it in the same child environment
	// so mvn.cmd receives JAVA_HOME and cannot pick an unrelated JDK.
	if hasResolution(plan.Versions, "maven") && !hasResolution(plan.Versions, "java") {
		javaRes := a.Resolver.Resolve("java", startDir)
		if javaRes.Error != "" {
			return EnvironmentPlan{}, fmt.Errorf("Maven 依赖的 Java 版本解析失败: %s", javaRes.Error)
		}
		if javaRes.Version != "" {
			java, ok := plugin.Get("java")
			if !ok {
				return EnvironmentPlan{}, fmt.Errorf("Maven 需要 Java，但 VSM 未加载 Java 工具定义")
			}
			javaDir := a.Paths.InstallDir("java", javaRes.Version)
			for _, bin := range java.BinPaths(javaDir) {
				key := filepath.Clean(bin)
				if os.PathSeparator == '\\' {
					key = strings.ToLower(key)
				}
				if !seen[key] {
					plan.Paths = append(plan.Paths, bin)
					seen[key] = true
				}
			}
			for key, value := range java.ExecEnv(javaDir) {
				plan.Variables[key] = value
			}
			plan.Versions = append(plan.Versions, javaRes)
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

func hasResolution(resolutions []Resolution, tool string) bool {
	for _, resolution := range resolutions {
		if resolution.Tool == tool {
			return true
		}
	}
	return false
}

// ProjectRequirement reports the nearest project declaration, ignoring overrides/global defaults.
func ProjectRequirement(tool, startDir string) (string, string, error) {
	return findToolVersionChecked(tool, startDir)
}
