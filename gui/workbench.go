package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"vsm/internal/config"
	"vsm/internal/core"
	"vsm/internal/mcpserver"
	"vsm/internal/plugin"
)

type WorkbenchStatus struct {
	ConfigError     string              `json:"config_error,omitempty"`
	Root            string              `json:"root"`
	Configured      bool                `json:"configured"`
	Trusted         bool                `json:"trusted"`
	Tasks           []core.PlannedTask  `json:"tasks"`
	EnvironmentKeys []string            `json:"environment_keys"`
	Lock            *core.ToolchainLock `json:"lock,omitempty"`
	LockError       string              `json:"lock_error,omitempty"`
}

func (a *App) Workbench() (WorkbenchStatus, error) {
	dir := a.ProjectDir()
	out := WorkbenchStatus{Root: dir, Tasks: []core.PlannedTask{}, EnvironmentKeys: []string{}}
	if dir == "" {
		return out, nil
	}
	w, err := core.FindWorkspace(dir)
	if err != nil {
		if file, lookupErr := core.WorkspaceConfigurationPath(dir); lookupErr == nil {
			out.Configured = true
			out.Root = filepath.Dir(file)
			out.ConfigError = err.Error()
			return out, nil
		}
		return out, err
	}
	if w != nil {
		out.Configured = true
		out.Root = w.Root
		out.Trusted = w.Trusted()
		out.EnvironmentKeys = w.EnvironmentKeys()
		plan, err := core.PlanTasks(dir, nil)
		if err != nil {
			return out, err
		}
		out.Tasks = plan.Tasks
	}
	lock, err := core.LoadLock(dir)
	if err == nil {
		out.Lock = lock
		if err := lock.ValidateRequirements(dir); err != nil {
			out.LockError = err.Error()
		}
	} else if !os.IsNotExist(err) {
		out.LockError = err.Error()
	}
	return out, nil
}

func (a *App) ConfigureWorkspace() (WorkbenchStatus, error) {
	done, err := a.begin()
	if err != nil {
		return WorkbenchStatus{}, err
	}
	defer done()
	if a.ProjectDir() == "" {
		return WorkbenchStatus{}, errNoProjectDir
	}
	if _, err := core.InitializeWorkspace(a.ProjectDir()); err != nil {
		return WorkbenchStatus{}, err
	}
	return a.Workbench()
}

func (a *App) WorkspaceConfiguration() (string, error) {
	if a.ProjectDir() == "" {
		return "", errNoProjectDir
	}
	file, err := core.WorkspaceConfigurationPath(a.ProjectDir())
	if err != nil {
		return "", err
	}
	data, err := config.ReadBounded(file, 1<<20)
	return string(data), err
}

func (a *App) SaveWorkspaceConfiguration(content string) (WorkbenchStatus, error) {
	done, err := a.begin()
	if err != nil {
		return WorkbenchStatus{}, err
	}
	defer done()
	if len(content) > 1<<20 {
		return WorkbenchStatus{}, fmt.Errorf("配置超过 1 MiB")
	}
	if a.ProjectDir() == "" {
		return WorkbenchStatus{}, errNoProjectDir
	}
	file, err := core.WorkspaceConfigurationPath(a.ProjectDir())
	if err != nil {
		return WorkbenchStatus{}, err
	}
	var value config.Workspace
	if err := config.DecodeStrict([]byte(content), &value); err != nil {
		return WorkbenchStatus{}, err
	}
	for tool := range value.Tools {
		if _, ok := plugin.Get(tool); !ok {
			return WorkbenchStatus{}, fmt.Errorf("未知工具 %s", tool)
		}
	}
	if err := value.Save(file); err != nil {
		return WorkbenchStatus{}, err
	}
	return a.Workbench()
}
func (a *App) TrustWorkspace() (WorkbenchStatus, error) {
	done, err := a.begin()
	if err != nil {
		return WorkbenchStatus{}, err
	}
	defer done()
	if a.ProjectDir() == "" {
		return WorkbenchStatus{}, errNoProjectDir
	}
	w, err := core.FindWorkspace(a.ProjectDir())
	if err != nil {
		return WorkbenchStatus{}, err
	}
	if w == nil {
		return WorkbenchStatus{}, fmt.Errorf("请先生成 vsm.json")
	}
	if err := w.Trust(); err != nil {
		return WorkbenchStatus{}, err
	}
	return a.Workbench()
}

func (a *App) LockWorkspace(platforms []string, update bool) (WorkbenchStatus, error) {
	done, err := a.begin()
	if err != nil {
		return WorkbenchStatus{}, err
	}
	defer done()
	if a.ProjectDir() == "" {
		return WorkbenchStatus{}, errNoProjectDir
	}
	if a.c().paths.ProjectMode == "docker" {
		return WorkbenchStatus{}, fmt.Errorf("Docker 项目请固定镜像摘要")
	}
	targets := []plugin.Target{}
	if len(platforms) > 6 {
		return WorkbenchStatus{}, fmt.Errorf("最多 6 个平台")
	}
	for _, value := range platforms {
		target, err := core.ParseTarget(value)
		if err != nil {
			return WorkbenchStatus{}, err
		}
		targets = append(targets, target)
	}
	// Delegate remote operations to the CLI so the context deadline stops the
	// complete process tree, including downloads and extractors.
	args := []string{"lock", "--json"}
	if update {
		args = append(args, "--update")
	}
	for _, target := range targets {
		args = append(args, "--platform", target.OS+"/"+target.Arch)
	}
	if err := a.runWorkspaceCLI(args); err != nil {
		return WorkbenchStatus{}, err
	}
	return a.Workbench()
}

func (a *App) InstallLockedWorkspace() (WorkbenchStatus, error) {
	done, err := a.begin()
	if err != nil {
		return WorkbenchStatus{}, err
	}
	defer done()
	if a.ProjectDir() == "" {
		return WorkbenchStatus{}, errNoProjectDir
	}
	if err := a.runWorkspaceCLI([]string{"install", "--locked"}); err != nil {
		return WorkbenchStatus{}, err
	}
	return a.Workbench()
}

func (a *App) runWorkspaceCLI(args []string) error {
	cli := locateVsmCLI()
	if cli == "" {
		return fmt.Errorf("未找到配套新版 CLI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir = a.ProjectDir()
	mcpserver.ConfigureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	output := &workbenchOutput{emit: func(value string) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "workspace:progress", value)
		}
	}}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("项目操作失败: %w；请查看操作输出并确认 CLI 已更新", err)
	}
	return nil
}

type workbenchOutput struct{ emit func(string) }

func (w *workbenchOutput) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		part := p[:min(len(p), 8192)]
		w.emit(strings.ToValidUTF8(string(part), "�"))
		p = p[len(part):]
	}
	return n, nil
}

func (a *App) RunWorkspaceTask(name string, locked, noCache bool, jobs int) (core.TaskReport, error) {
	done, err := a.begin()
	if err != nil {
		return core.TaskReport{}, err
	}
	defer done()
	dir := a.ProjectDir()
	if dir == "" {
		return core.TaskReport{}, errNoProjectDir
	}
	cli := locateVsmCLI()
	if cli == "" {
		return core.TaskReport{}, fmt.Errorf("未找到配套新版 CLI")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.workMu.Lock()
	a.taskCancel = cancel
	a.workMu.Unlock()
	defer func() { cancel(); a.workMu.Lock(); a.taskCancel = nil; a.workMu.Unlock() }()
	report, err := core.RunTasks(ctx, dir, cli, []string{name}, core.TaskOptions{Jobs: jobs, Locked: locked, NoCache: noCache, OnEvent: func(event core.TaskEvent) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "workspace:task", event)
		}
	}})
	if err != nil {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "workspace:progress", err.Error())
		}
		return report, err
	}
	return report, nil
}

func (a *App) CancelWorkspaceTask() bool {
	a.workMu.Lock()
	defer a.workMu.Unlock()
	if a.taskCancel == nil {
		return false
	}
	a.taskCancel()
	return true
}
func (a *App) ToolCatalog(query string) []plugin.CatalogTool { return plugin.Catalog(query) }

func (a *App) ReloadTools() error {
	done, err := a.begin()
	if err != nil {
		return err
	}
	defer done()
	return plugin.ReloadExternalTools()
}

func (a *App) AddGitHubTool(name, repo, command, bin, match string) error {
	done, err := a.begin()
	if err != nil {
		return err
	}
	defer done()
	if _, ok := plugin.Get(name); ok {
		return fmt.Errorf("工具已存在，不覆盖")
	}
	m, err := plugin.GitHubManifest(name, repo, command, bin, match)
	if err != nil {
		return err
	}
	global, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	path := filepath.Join(global.Root, "plugins", name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return plugin.ReloadExternalTools()
}
