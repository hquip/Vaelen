package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"vsm/internal/buildinfo"
	"vsm/internal/config"
	"vsm/internal/core"
	"vsm/internal/logging"
	"vsm/internal/mcpconfig"
	"vsm/internal/mcpserver"
	"vsm/internal/netcfg"
	"vsm/internal/plugin"
	"vsm/internal/updater"
)

// components 是一组按当前配置组装好的核心组件。
// 安装根目录切换时整体替换（见 reload），读取方通过原子指针拿到一致的快照，
// 避免「一个 goroutine 正在读 paths、另一个正在改写」的数据竞争。
type components struct {
	projectDir string
	initErr    error
	paths      core.Paths
	reg        core.Registry
	res        core.Resolver
	inst       core.Installer
	act        core.Activator
}

func newComponents() *components {
	return newComponentsForDir("")
}

func newComponentsForDir(dir string) *components {
	paths, initErr := core.DefaultPaths()
	if initErr == nil && dir != "" {
		paths, initErr = core.PathsForDirectory(paths, dir)
	}
	reg := core.Registry{Paths: paths}
	res := core.Resolver{Paths: paths, Registry: reg}
	return &components{
		projectDir: dir,
		initErr:    initErr,
		paths:      paths,
		reg:        reg,
		res:        res,
		inst:       core.Installer{Paths: paths},
		act:        core.Activator{Paths: paths, Resolver: res, Registry: reg, VsmExe: locateVsmCLI()},
	}
}

// App 暴露给前端的后端服务。其导出方法会被 Wails 绑定为可在 JS 中调用的函数
// （window.go.main.App.*），每次调用都在独立的 goroutine 中执行。
type App struct {
	ctx context.Context
	st  atomic.Pointer[components]

	// mu 串行化会改动磁盘状态的操作（安装/卸载/纳管/重建 shim/切换安装根目录）。
	// 用 TryLock 而不是排队等待：安装可能持续数分钟，排队会让按钮看起来毫无反应。
	mu sync.Mutex

	workMu     sync.Mutex
	taskCancel context.CancelFunc
	dirMu      sync.RWMutex
	projectDir string // 「设为本目录」写入 .tool-versions 的目标目录；空表示尚未选择
}

// errBusy 在已有写操作进行中时返回给前端。
var errBusy = errors.New("有安装/纳管任务正在进行，请等它完成后再试")

// errNoProjectDir 提示前端需要先让用户选择项目目录（前端据此弹出目录选择框）。
var errNoProjectDir = errors.New("请先选择项目目录：版本会写入该目录的 .tool-versions")

// NewApp 构造后端服务，组装核心组件。
func NewApp() *App {
	a := &App{projectDir: initialProjectDir()}
	a.st.Store(newComponentsForDir(a.projectDir))
	return a
}

func (a *App) c() *components { return a.st.Load() }

// begin 尝试获取写操作锁，成功时返回解锁函数。
func (a *App) begin() (func(), error) {
	if err := a.c().initErr; err != nil {
		return nil, err
	}
	if !a.mu.TryLock() {
		return nil, errBusy
	}
	return a.mu.Unlock, nil
}

// initialProjectDir 推断「本目录」的初始值：在终端里 `cd 项目 && vsm-gui` 启动时就是该目录。
// 从 Finder / 资源管理器双击启动时，工作目录是 "/"、用户主目录或程序所在目录，
// 都没有「项目」含义——往这些地方写 .tool-versions 会影响其下所有目录——因此返回空，
// 由用户在界面里明确选择。
func initialProjectDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cwd = filepath.Clean(cwd)
	if cwd == filepath.VolumeName(cwd)+string(os.PathSeparator) {
		return "" // 磁盘根目录（macOS 从 Finder 启动时就是 "/"）
	}
	if home, err := os.UserHomeDir(); err == nil && samePath(cwd, home) {
		return ""
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if samePath(cwd, exeDir) || strings.Contains(cwd, ".app"+string(os.PathSeparator)+"Contents") {
			return ""
		}
	}
	return cwd
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// locateVsmCLI 尽力定位 vsm CLI 可执行文件，供 GUI 生成的 shim 用绝对路径调用：
// 优先 GUI 同目录（macOS 即 VSM.app/Contents/MacOS，发行包已内置 CLI），
// 其次 .app 同级目录与 PATH；都找不到返回空。
func locateVsmCLI() string {
	name := "vsm"
	if runtime.GOOS == "windows" {
		name = "vsm.exe"
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands := []string{filepath.Join(dir, name)}
		if runtime.GOOS == "darwin" {
			if app := filepath.Dir(filepath.Dir(dir)); strings.HasSuffix(app, ".app") {
				cands = append(cands, filepath.Join(filepath.Dir(app), name))
			}
		}
		for _, cand := range cands {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// 从 Finder/Dock 启动的 macOS 应用拿不到 shell 的 PATH，补查常见安装位置
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		for _, cand := range []string{
			"/opt/homebrew/bin/vsm",
			"/usr/local/bin/vsm",
			filepath.Join(home, ".local", "bin", "vsm"),
			filepath.Join(home, "bin", "vsm"),
		} {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
		}
	}
	return ""
}

// Platform 返回运行平台（windows / darwin / linux），前端据此做平台适配
// （例如 macOS 隐藏标题栏后给红绿灯按钮留位、切换系统字体）。
func (a *App) Platform() string { return runtime.GOOS }

func (a *App) Version() string { return buildinfo.DisplayVersion() }

func (a *App) CheckUpdate() (updater.Info, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return updater.New().Check(ctx, buildinfo.DisplayVersion(), "")
}

func (a *App) ApplyUpdate() (updater.Result, error) {
	if !a.mu.TryLock() {
		return updater.Result{}, errBusy
	}
	defer a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := updater.New().WithProgress(a.emitUpdateProgress)
	info, err := client.Check(ctx, buildinfo.DisplayVersion(), "")
	if err != nil {
		return updater.Result{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return updater.Result{}, err
	}
	return client.Apply(ctx, info, exe, true)
}

func (a *App) emitUpdateProgress(progress updater.Progress) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "update:progress", progress)
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	logging.Record("info", "gui", "app.started", logging.Fields{"platform": runtime.GOOS})
	if err := a.c().initErr; err != nil {
		logging.Record("error", "gui", "initialization.failed", logging.Fields{"error": err.Error()})
	}
}
func (a *App) shutdown(ctx context.Context) {
	a.CancelWorkspaceTask()
	logging.Record("info", "gui", "app.stopped", nil)
}

// ToolStatus 是前端展示一个工具所需的数据。
type ToolStatus struct {
	Tool       string                 `json:"tool"`
	Current    string                 `json:"current"`
	Source     string                 `json:"source"`
	Spec       string                 `json:"spec"`
	Installed  []string               `json:"installed"`
	Activation *core.ActivationStatus `json:"activation,omitempty"`
}

// Tools 返回所有支持的工具名。
func (a *App) Tools() []string { return plugin.Names() }

// resolveDir 返回解析版本时的起点目录：已选项目目录，否则用户主目录
// （等价于「新开一个终端」时看到的版本）。
func (a *App) resolveDir() string {
	if d := a.ProjectDir(); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// Status 返回所有工具的当前状态（已装版本 + 生效版本 + 来源）。
func (a *App) Status() (status []ToolStatus, err error) {
	defer logGUIError("status.failed", &err)()
	c := a.c()
	if c.initErr != nil {
		return nil, c.initErr
	}
	dir := c.projectDir
	if dir == "" {
		dir = a.resolveDir()
	}
	names := plugin.Names()
	out := make([]ToolStatus, 0, len(names))
	for _, name := range names {
		if c.paths.ProjectMode == "docker" {
			spec, source, err := core.ProjectRequirement(name, dir)
			if err != nil {
				return nil, err
			}
			out = append(out, ToolStatus{Tool: name, Source: source, Spec: spec, Installed: []string{}})
			continue
		}
		installed, err := c.reg.Installed(name)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 的已安装版本失败: %w", name, err)
		}
		if installed == nil {
			installed = []string{} // 前端直接取 .length，避免 null
		}
		sort.Slice(installed, func(i, j int) bool {
			return core.CompareVersions(installed[i], installed[j]) > 0
		})
		r := c.res.Resolve(name, dir)
		if r.Error != "" {
			return nil, fmt.Errorf("%s: %s", name, r.Error)
		}
		var activation *core.ActivationStatus
		if c.paths.ProjectMode != "local" && r.Version != "" {
			checked := c.act.ActivationStatus(name, dir)
			activation = &checked
		}
		out = append(out, ToolStatus{
			Tool:       name,
			Current:    r.Version,
			Source:     r.Source,
			Spec:       r.Spec,
			Installed:  installed,
			Activation: activation,
		})
	}
	return out, nil
}

// RemoteVersions 返回某工具可安装的远端版本列表。
func (a *App) RemoteVersions(tool string) (versions []string, err error) {
	defer logGUI("versions.remote", logging.Fields{"tool": tool}, &err)()
	p, ok := plugin.Get(tool)
	if !ok {
		return nil, fmt.Errorf("不支持的工具: %s", tool)
	}
	return p.ListAll()
}

// Install 安装某工具版本（version 可为 latest / 模糊版本）；dir 为空装到默认根目录，
// 非空装到该自定义目录（链接进 vsm）。安装进度通过 "install:progress" 事件实时推送。
func (a *App) Install(tool, version, dir string) error {
	return a.install(tool, version, dir, false)
}

func (a *App) InstallVerified(tool, version, dir string, requireChecksum bool) error {
	return a.install(tool, version, dir, requireChecksum)
}

func (a *App) install(tool, version, dir string, requireChecksum bool) (err error) {
	defer logGUI("install", logging.Fields{"tool": tool, "version": version}, &err)()
	done, err := a.begin()
	if err != nil {
		return err
	}
	defer done()
	c := a.c()
	if c.paths.ProjectMode == "docker" {
		return core.ErrDockerHostInstall
	}
	p, ok := plugin.Get(tool)
	if !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	if dir = strings.TrimSpace(dir); dir != "" {
		if dir, err = filepath.Abs(dir); err != nil {
			return err
		}
	}
	resolved, err := resolveRemote(p, version)
	if err != nil {
		return err
	}
	a.emit("解析到版本 " + resolved)
	installer := c.inst
	installer.RequireChecksum = requireChecksum
	if err := installer.Install(p, resolved, dir, a.emit); err != nil {
		return err
	}
	return c.act.GenerateShims()
}

func (a *App) emit(msg string) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "install:progress", msg)
	}
}

// PickDirectory 弹出系统文件夹选择对话框，返回所选目录（取消返回空串）。
func (a *App) PickDirectory(title string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面未就绪")
	}
	if title == "" {
		title = "选择目录"
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: title})
}

// GetInstallRoot 返回当前全局安装根目录。
func (a *App) GetInstallRoot() string { return a.c().paths.Installs() }

// SetInstallRoot 设置全局安装根目录（dir 为空恢复默认），并热重载使之立即生效。
func (a *App) SetInstallRoot(dir string) (err error) {
	defer logGUI("install_root.set", logging.Fields{}, &err)()
	if a.c().paths.ProjectMode == "local" || a.c().paths.ProjectMode == "docker" {
		return fmt.Errorf("项目隔离环境使用固定目录，不修改全局安装根目录")
	}
	done, err := a.begin()
	if err != nil {
		return err
	}
	defer done()
	c := a.c()
	if err := c.paths.EnsureDirs(); err != nil {
		return err
	}
	path := c.paths.InstallRootFile()
	if dir = strings.TrimSpace(dir); dir == "" {
		// 删除失败时不能谎报「已恢复默认」，否则之后的安装仍落在旧目录
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("恢复默认安装目录失败: %w", err)
		}
	} else {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if err := c.paths.ValidateInstallRoot(abs); err != nil {
			return err
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return err
		}
		if err := config.WriteFileAtomic(path, []byte(abs+"\n"), 0o644); err != nil {
			return err
		}
	}
	a.st.Store(newComponentsForDir(a.ProjectDir()))
	return nil
}

// SetGlobal 设全局默认版本（写 ~/.vsm/.tool-versions）。
func (a *App) SetGlobal(tool, version string) (err error) {
	defer logGUI("version.global", logging.Fields{"tool": tool, "version": version}, &err)()
	done, err := a.begin()
	if err != nil {
		return err
	}
	defer done()
	c := a.c()
	if err := checkActivationCLI(c.act.VsmExe); err != nil {
		return err
	}
	if err := c.act.SetGlobalVersion(tool, version); err != nil {
		return err
	}
	status := c.act.ActivationStatus(tool, a.resolveDir())
	for _, warning := range status.Warnings {
		logging.Record("warn", "gui", "version.global.activation", logging.Fields{"tool": tool, "version": version, "warning": warning})
	}
	return nil
}

// Uninstall 卸载某版本。
func (a *App) Uninstall(tool, version string) (err error) {
	defer logGUI("uninstall", logging.Fields{"tool": tool, "version": version}, &err)()
	done, err := a.begin()
	if err != nil {
		return err
	}
	defer done()
	c := a.c()
	if err := c.inst.Uninstall(tool, version); err != nil {
		return err
	}
	return c.act.GenerateShims()
}

// DataDir 返回 VSM 数据目录，便于前端展示。
func (a *App) DataDir() string { return a.c().paths.Root }

// DetectSystem 探测系统 PATH 中所有非 vsm 管理的语言运行时（含同一语言的多版本，
// 如 java 1.8/11/17/21）。供 GUI 顶部「系统检测」总览一次性列出全部。
func (a *App) DetectSystem() []core.SystemVersion {
	return core.DetectSystem(plugin.DetectCommands(), a.c().paths.Shims())
}

// DetectTool 仅探测某个工具的系统版本，供详情页按需调用，避免全量扫描的耗时。
func (a *App) DetectTool(tool string) []core.SystemVersion {
	return core.DetectSystem(map[string][]string{tool: detectNames(tool)}, a.c().paths.Shims())
}

func (a *App) InspectToolPath(tool, path string) (core.SystemVersion, error) {
	return core.InspectToolPath(tool, path)
}

func (a *App) AdoptCustomTool(tool, path, alias string) (result core.SystemVersion, err error) {
	defer logGUI("adopt.custom", logging.Fields{"tool": tool}, &err)()
	done, err := a.begin()
	if err != nil {
		return result, err
	}
	defer done()
	c := a.c()
	result, err = c.inst.AdoptPath(tool, path, alias)
	if err != nil {
		return result, err
	}
	return result, c.act.GenerateShims()
}

// AdoptDetected reuses the explicit scan snapshot; adoption does not scan again.
func (a *App) AdoptDetected(items []core.SystemVersion) (count int, err error) {
	defer logGUI("adopt.detected", logging.Fields{"count": len(items)}, &err)()
	if len(items) > 1024 {
		return 0, fmt.Errorf("一次最多纳管 1024 项")
	}
	done, err := a.begin()
	if err != nil {
		return 0, err
	}
	defer done()
	return a.adoptList(items)
}

func detectNames(tool string) []string {
	if names, ok := plugin.DetectCommands()[tool]; ok {
		return names
	}
	return []string{tool}
}

// AdoptTool 把系统检测到的某版本纳管进 vsm（创建链接），随后重建 shim。
func (a *App) AdoptTool(tool, version, path string) (err error) {
	defer logGUI("adopt", logging.Fields{"tool": tool, "version": version}, &err)()
	done, err := a.begin()
	if err != nil {
		return err
	}
	defer done()
	c := a.c()
	p, ok := plugin.Get(tool)
	if !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	if err := c.inst.Adopt(p, version, path); err != nil {
		return err
	}
	return c.act.GenerateShims()
}

// ProjectDir 返回「设为本目录」的目标目录（空表示尚未选择）。
func (a *App) ProjectDir() string {
	a.dirMu.RLock()
	defer a.dirMu.RUnlock()
	return a.projectDir
}

// SetProjectDir 设置项目目录（之后的「设为本目录」写入该目录的 .tool-versions，
// 版本解析也以它为起点）。空串表示清除。
func (a *App) SetProjectDir(dir string) (err error) {
	defer logGUI("project.select", logging.Fields{}, &err)()
	if !a.mu.TryLock() {
		return errBusy
	}
	defer a.mu.Unlock()
	dir = strings.TrimSpace(dir)
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("%s 不是目录", abs)
		}
		dir = abs
	}
	a.dirMu.Lock()
	a.projectDir = dir
	a.dirMu.Unlock()
	a.st.Store(newComponentsForDir(dir))
	return nil
}

func (a *App) ProjectEnvironment() (project *core.Project, err error) {
	defer logGUIError("project.status.failed", &err)()
	dir := a.ProjectDir()
	if dir == "" {
		return nil, nil
	}
	return core.FindProject(dir)
}

func (a *App) InitProjectEnvironment(mode, image string) (project *core.Project, err error) {
	defer logGUI("project.init", logging.Fields{"mode": mode}, &err)()
	done, err := a.begin()
	if err != nil {
		return nil, err
	}
	defer done()
	dir := a.ProjectDir()
	if dir == "" {
		return nil, errNoProjectDir
	}
	p, err := core.InitProject(dir, mode, image)
	if err != nil {
		return nil, err
	}
	a.st.Store(newComponentsForDir(dir))
	return &p, nil
}

func (a *App) MCPConfig() (map[string]any, error) {
	dir := a.ProjectDir()
	if dir == "" {
		return nil, errNoProjectDir
	}
	exe := locateVsmCLI()
	if exe == "" {
		return nil, fmt.Errorf("未找到 vsm CLI。请将 v1.0.2 或更新版本的 CLI 重命名为 vsm（Windows 为 vsm.exe），放在 GUI 同目录后重试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "mcp", "config", "--project", dir)
	cmd.Dir = dir
	mcpserver.ConfigureProcess(cmd)
	cmd.WaitDelay = time.Second
	return mcpConfigFromCLI(cmd)
}

func (a *App) MCPClients() []mcpconfig.Client { return mcpconfig.Clients(runtime.GOOS) }

func (a *App) MCPConfigForClient(client string, readOnly bool) (configuration mcpconfig.Configuration, err error) {
	defer logGUI("mcp.config", logging.Fields{"client": client, "read_only": readOnly}, &err)()
	config, err := a.MCPConfig()
	if err != nil {
		return mcpconfig.Configuration{}, err
	}
	servers := config["mcpServers"].(map[string]any)
	server := servers["vsm"].(map[string]any)
	command := server["command"].(string)
	rawArgs := server["args"].([]any)
	args := make([]string, 0, len(rawArgs)+1)
	for _, raw := range rawArgs {
		arg, ok := raw.(string)
		if !ok {
			return mcpconfig.Configuration{}, fmt.Errorf("CLI 返回了无效的 MCP 参数，请更新 CLI 后重试")
		}
		args = append(args, arg)
	}
	if readOnly {
		args = append(args, "--read-only")
	}
	return mcpconfig.Generate(client, runtime.GOOS, command, args)
}

// Ask the selected CLI for its configuration so an older executable cannot
// silently produce a configuration for a command it does not implement.
func mcpConfigFromCLI(cmd *exec.Cmd) (map[string]any, error) {
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("无法使用 CLI %s 生成 MCP 配置：%w。请检查该文件是否为 v1.0.2 或更新版本，并将新版 CLI 放在 GUI 同目录", cmd.Path, err)
	}
	var config map[string]any
	if err := json.Unmarshal(output, &config); err != nil {
		return nil, fmt.Errorf("CLI %s 未返回有效的 MCP 配置，请更新 CLI 后重试", cmd.Path)
	}
	servers, _ := config["mcpServers"].(map[string]any)
	server, _ := servers["vsm"].(map[string]any)
	command, _ := server["command"].(string)
	args, _ := server["args"].([]any)
	if command == "" || len(args) < 2 || args[0] != "mcp" || args[1] != "serve" {
		return nil, fmt.Errorf("CLI %s 不支持所需的 MCP 配置，请更新 CLI 后重试", cmd.Path)
	}
	return config, nil
}

// PickProjectDir 弹出目录选择框让用户选择项目目录；取消时保持原值并返回空串。
func (a *App) PickProjectDir() (string, error) {
	d, err := a.PickDirectory("选择项目目录（版本将写入其中的 .tool-versions）")
	if err != nil || d == "" {
		return "", err
	}
	if err := a.SetProjectDir(d); err != nil {
		return "", err
	}
	return a.ProjectDir(), nil
}

// SetLocal 把某版本写入项目目录的 .tool-versions（等价于在该目录执行 vsm use）。
func (a *App) SetLocal(tool, version string) (err error) {
	defer logGUI("version.local", logging.Fields{"tool": tool, "version": version}, &err)()
	if _, ok := plugin.Get(tool); !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	dir := a.ProjectDir()
	if dir == "" {
		return errNoProjectDir
	}
	return core.SetProjectVersion(dir, tool, version)
}

// CurrentDir 返回项目目录，供前端展示（保留旧方法名以兼容前端调用）。
func (a *App) CurrentDir() string { return a.ProjectDir() }

// GetMirror 返回当前下载镜像设置（cn / off）。优先环境变量 VSM_MIRROR，其次配置文件。
func (a *App) GetMirror() string {
	if m := plugin.MirrorMode(); m != "" {
		return m
	}
	return "off"
}

// SetMirror 写入下载镜像设置（cn 国内加速 / off 关闭）。
func (a *App) SetMirror(mode string) (err error) {
	defer logGUI("mirror.set", logging.Fields{"mode": mode}, &err)()
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "cn" && mode != "off" && mode != "custom" {
		return fmt.Errorf("不支持的镜像: %s（可选 cn / off / custom）", mode)
	}
	global, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(global.Root, 0755); err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(global.Root, "mirror"), []byte(mode+"\n"), 0o644)
}

// GetProxy 返回当前网络代理设置的原始值："" 表示跟随系统环境变量，"off" 表示强制直连，
// 其余为代理 URL。优先环境变量 VSM_PROXY，其次配置文件。
func (a *App) GetProxy() string {
	return netcfg.ProxySetting()
}

// SetProxy 写入网络代理设置。入参可为代理 URL（http/https/socks5）、off（强制直连），
// 或空字符串 / system（清除配置，回退系统环境变量）。
func (a *App) SetProxy(input string) (err error) {
	defer logGUI("proxy.set", logging.Fields{}, &err)()
	stored, err := netcfg.Normalize(input)
	if err != nil {
		return err
	}
	global, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(global.Root, 0755); err != nil {
		return err
	}
	path := filepath.Join(global.Root, "proxy")
	if stored == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return config.WriteFileAtomic(path, []byte(stored+"\n"), 0o644)
}

// PathInitResult 描述「激活 PATH」的结果。
type PathInitResult struct {
	Status  string `json:"status"` // added | exists
	Target  string `json:"target"` // 写入位置：Windows 为「用户级 PATH」，其它平台为 shell 配置文件路径
	Message string `json:"message"`
	Command string `json:"command,omitempty"`
}

func checkActivationCLI(path string) error {
	if path == "" {
		return fmt.Errorf("未找到配套 CLI，请将新版 vsm 与 GUI 放在同一目录")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	mcpserver.ConfigureProcess(cmd)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "vsm "+buildinfo.DisplayVersion() {
		return fmt.Errorf("CLI 与 GUI 版本不一致或无法读取：%s；请同时更新 CLI 和 GUI（GUI %s），并放在同一目录", path, buildinfo.DisplayVersion())
	}
	return nil
}

// PathInit 让新开的终端自动启用 vsm：
//   - Windows：把 shims 写入用户级 PATH 并配置 PowerShell 启动钩子；
//   - macOS / Linux：把激活语句写进 ~/.zshrc 等 shell 配置文件（此前这里直接报错）。
func (a *App) PathInit() (result PathInitResult, err error) {
	defer logGUI("path.init", logging.Fields{}, &err)()
	if a.c().paths.ProjectMode == "local" || a.c().paths.ProjectMode == "docker" {
		return PathInitResult{}, fmt.Errorf("隔离项目使用 vsm run，不把项目目录写入系统 PATH")
	}
	done, err := a.begin()
	if err != nil {
		return PathInitResult{}, err
	}
	defer done()
	c := a.c()
	if err := c.paths.EnsureDirs(); err != nil {
		return PathInitResult{}, err
	}
	if err := checkActivationCLI(c.act.VsmExe); err != nil {
		return PathInitResult{}, err
	}
	if err := c.act.GenerateShims(); err != nil {
		return PathInitResult{}, err
	}
	if runtime.GOOS == "windows" {
		st, err := core.PersistShimsPath(c.paths.Shims())
		if err != nil {
			return PathInitResult{}, err
		}
		profiles, err := core.PersistPowerShellHooks(c.act.VsmExe)
		if err != nil {
			return PathInitResult{}, fmt.Errorf("用户 PATH 已写入，但 PowerShell 激活配置失败: %w", err)
		}
		message := "已配置用户 PATH 和 PowerShell 启动钩子。请新开 PowerShell；已有终端执行 vsm activate powershell | Out-String | Invoke-Expression。cmd 使用 vsm exec <tool> -- <命令>，或将 shims 前置到当前 cmd 的 PATH。"
		if len(profiles.Warnings) > 0 {
			message += "\n" + strings.Join(profiles.Warnings, "\n")
		}
		return PathInitResult{Status: st, Target: "用户级 PATH、" + strings.Join(profiles.Paths, "、"), Message: message, Command: core.PowerShellActivationCommand(c.act.VsmExe)}, nil
	}
	rc, st, err := core.PersistShellHook(core.DefaultShell(), core.VsmCommand(c.act.VsmExe))
	return PathInitResult{Status: st, Target: rc}, err
}

// AdoptAll 纳管系统检测到的全部版本（跳过已纳管/已装），返回成功纳管数量。
func (a *App) AdoptAll() (count int, err error) {
	defer logGUI("adopt_all", logging.Fields{}, &err)()
	done, err := a.begin()
	if err != nil {
		return 0, err
	}
	defer done()
	return a.adoptList(core.DetectSystem(plugin.DetectCommands(), a.c().paths.Shims()))
}

// AdoptToolAll 纳管某工具系统检测到的全部版本，返回成功纳管数量。
func (a *App) AdoptToolAll(tool string) (count int, err error) {
	defer logGUI("adopt_tool_all", logging.Fields{"tool": tool}, &err)()
	done, err := a.begin()
	if err != nil {
		return 0, err
	}
	defer done()
	return a.adoptList(core.DetectSystem(map[string][]string{tool: detectNames(tool)}, a.c().paths.Shims()))
}

// adoptList 逐个纳管；部分失败时返回成功数量与汇总错误（此前失败项被静默跳过）。
func (a *App) adoptList(sys []core.SystemVersion) (int, error) {
	c := a.c()
	n := 0
	var failed []string
	for _, s := range sys {
		version := s.RegistryVersion()
		p, ok := plugin.Get(s.Tool)
		if !ok || s.Version == "" || c.reg.IsInstalled(s.Tool, version) {
			continue
		}
		if err := c.inst.Adopt(p, version, s.Path); err != nil {
			failed = append(failed, fmt.Sprintf("%s %s（%v）", s.Tool, version, err))
			continue
		}
		n++
	}
	if err := c.act.GenerateShims(); err != nil {
		return n, fmt.Errorf("已纳管 %d 个，但重建 shim 失败: %w", n, err)
	}
	if len(failed) > 0 {
		return n, fmt.Errorf("已纳管 %d 个，以下失败：%s", n, strings.Join(failed, "；"))
	}
	return n, nil
}

// resolveRemote 把版本要求（latest / "20" 等）解析为远端具体版本号。
func resolveRemote(p plugin.Plugin, spec string) (string, error) {
	return core.ResolveToolVersion(p, spec)
}
