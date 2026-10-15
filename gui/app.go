package main

import (
	"context"
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

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"vsm/internal/config"
	"vsm/internal/core"
	"vsm/internal/netcfg"
	"vsm/internal/plugin"
)

// components 是一组按当前配置组装好的核心组件。
// 安装根目录切换时整体替换（见 reload），读取方通过原子指针拿到一致的快照，
// 避免「一个 goroutine 正在读 paths、另一个正在改写」的数据竞争。
type components struct {
	paths core.Paths
	reg   core.Registry
	res   core.Resolver
	inst  core.Installer
	act   core.Activator
}

func newComponents() *components {
	paths, _ := core.DefaultPaths()
	reg := core.Registry{Paths: paths}
	res := core.Resolver{Paths: paths, Registry: reg}
	return &components{
		paths: paths,
		reg:   reg,
		res:   res,
		inst:  core.Installer{Paths: paths},
		act:   core.Activator{Paths: paths, Resolver: res, Registry: reg, VsmExe: locateVsmCLI()},
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
	a.st.Store(newComponents())
	return a
}

func (a *App) c() *components { return a.st.Load() }

// begin 尝试获取写操作锁，成功时返回解锁函数。
func (a *App) begin() (func(), error) {
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

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// ToolStatus 是前端展示一个工具所需的数据。
type ToolStatus struct {
	Tool      string   `json:"tool"`
	Current   string   `json:"current"`
	Source    string   `json:"source"`
	Spec      string   `json:"spec"`
	Installed []string `json:"installed"`
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
func (a *App) Status() ([]ToolStatus, error) {
	c := a.c()
	dir := a.resolveDir()
	names := plugin.Names()
	out := make([]ToolStatus, 0, len(names))
	for _, name := range names {
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
		out = append(out, ToolStatus{
			Tool:      name,
			Current:   r.Version,
			Source:    r.Source,
			Spec:      r.Spec,
			Installed: installed,
		})
	}
	return out, nil
}

// RemoteVersions 返回某工具可安装的远端版本列表。
func (a *App) RemoteVersions(tool string) ([]string, error) {
	p, ok := plugin.Get(tool)
	if !ok {
		return nil, fmt.Errorf("不支持的工具: %s", tool)
	}
	return p.ListAll()
}

// Install 安装某工具版本（version 可为 latest / 模糊版本）；dir 为空装到默认根目录，
// 非空装到该自定义目录（链接进 vsm）。安装进度通过 "install:progress" 事件实时推送。
func (a *App) Install(tool, version, dir string) error {
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
	if err := c.inst.Install(p, resolved, dir, a.emit); err != nil {
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
func (a *App) SetInstallRoot(dir string) error {
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
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return err
		}
		if err := config.WriteFileAtomic(path, []byte(abs+"\n"), 0o644); err != nil {
			return err
		}
	}
	a.st.Store(newComponents())
	return nil
}

// SetGlobal 设全局默认版本（写 ~/.vsm/.tool-versions）。
func (a *App) SetGlobal(tool, version string) error {
	if _, ok := plugin.Get(tool); !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	c := a.c()
	if err := c.paths.EnsureDirs(); err != nil {
		return err
	}
	return config.SetIn(c.paths.GlobalToolVersions(), tool, version)
}

// Uninstall 卸载某版本。
func (a *App) Uninstall(tool, version string) error {
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

func detectNames(tool string) []string {
	if names, ok := plugin.DetectCommands()[tool]; ok {
		return names
	}
	return []string{tool}
}

// AdoptTool 把系统检测到的某版本纳管进 vsm（创建链接），随后重建 shim。
func (a *App) AdoptTool(tool, version, path string) error {
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
func (a *App) SetProjectDir(dir string) error {
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
	return nil
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
func (a *App) SetLocal(tool, version string) error {
	if _, ok := plugin.Get(tool); !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	dir := a.ProjectDir()
	if dir == "" {
		return errNoProjectDir
	}
	return config.SetIn(filepath.Join(dir, ".tool-versions"), tool, version)
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
func (a *App) SetMirror(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "cn" && mode != "off" {
		return fmt.Errorf("不支持的镜像: %s（可选 cn / off）", mode)
	}
	c := a.c()
	if err := c.paths.EnsureDirs(); err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(c.paths.Root, "mirror"), []byte(mode+"\n"), 0o644)
}

// GetProxy 返回当前网络代理设置的原始值："" 表示跟随系统环境变量，"off" 表示强制直连，
// 其余为代理 URL。优先环境变量 VSM_PROXY，其次配置文件。
func (a *App) GetProxy() string {
	return netcfg.ProxySetting()
}

// SetProxy 写入网络代理设置。入参可为代理 URL（http/https/socks5）、off（强制直连），
// 或空字符串 / system（清除配置，回退系统环境变量）。
func (a *App) SetProxy(input string) error {
	stored, err := netcfg.Normalize(input)
	if err != nil {
		return err
	}
	c := a.c()
	if err := c.paths.EnsureDirs(); err != nil {
		return err
	}
	path := filepath.Join(c.paths.Root, "proxy")
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
	Status string `json:"status"` // added | exists
	Target string `json:"target"` // 写入位置：Windows 为「用户级 PATH」，其它平台为 shell 配置文件路径
}

// PathInit 让新开的终端自动启用 vsm：
//   - Windows：把 shims 写入用户级 PATH 并广播；
//   - macOS / Linux：把激活语句写进 ~/.zshrc 等 shell 配置文件（此前这里直接报错）。
func (a *App) PathInit() (PathInitResult, error) {
	done, err := a.begin()
	if err != nil {
		return PathInitResult{}, err
	}
	defer done()
	c := a.c()
	if err := c.paths.EnsureDirs(); err != nil {
		return PathInitResult{}, err
	}
	if runtime.GOOS != "windows" && c.act.VsmExe == "" {
		// 写入裸 `vsm` 会让每次开终端都报 command not found，宁可明确报错
		return PathInitResult{}, fmt.Errorf("未找到 vsm 命令行程序：请把 vsm 放到 PATH，或与 VSM.app 放在同一目录")
	}
	if err := c.act.GenerateShims(); err != nil {
		return PathInitResult{}, err
	}
	if runtime.GOOS == "windows" {
		st, err := core.PersistShimsPath(c.paths.Shims())
		return PathInitResult{Status: st, Target: "用户级 PATH"}, err
	}
	rc, st, err := core.PersistShellHook(core.DefaultShell(), core.VsmCommand(c.act.VsmExe))
	return PathInitResult{Status: st, Target: rc}, err
}

// AdoptAll 纳管系统检测到的全部版本（跳过已纳管/已装），返回成功纳管数量。
func (a *App) AdoptAll() (int, error) {
	done, err := a.begin()
	if err != nil {
		return 0, err
	}
	defer done()
	return a.adoptList(core.DetectSystem(plugin.DetectCommands(), a.c().paths.Shims()))
}

// AdoptToolAll 纳管某工具系统检测到的全部版本，返回成功纳管数量。
func (a *App) AdoptToolAll(tool string) (int, error) {
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
		p, ok := plugin.Get(s.Tool)
		if !ok || s.Version == "" || c.reg.IsInstalled(s.Tool, s.Version) {
			continue
		}
		if err := c.inst.Adopt(p, s.Version, s.Path); err != nil {
			failed = append(failed, fmt.Sprintf("%s %s（%v）", s.Tool, s.Version, err))
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
	if spec == "" || spec == "latest" {
		return p.LatestStable()
	}
	all, err := p.ListAll()
	if err != nil {
		return "", err
	}
	v := core.BestMatch(spec, all)
	if v == "" {
		return "", fmt.Errorf("找不到匹配 %q 的 %s 版本", spec, p.Name())
	}
	return v, nil
}
