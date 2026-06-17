package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"vsm/internal/config"
	"vsm/internal/core"
	"vsm/internal/netcfg"
	"vsm/internal/plugin"
)

// App 暴露给前端的后端服务。其导出方法会被 Wails 绑定为可在 JS 中调用的函数
// （window.go.main.App.*）。
type App struct {
	ctx   context.Context
	paths core.Paths
	reg   core.Registry
	res   core.Resolver
	inst  core.Installer
	act   core.Activator
}

// NewApp 构造后端服务，组装核心组件。
func NewApp() *App {
	paths, _ := core.DefaultPaths()
	reg := core.Registry{Paths: paths}
	res := core.Resolver{Paths: paths, Registry: reg}
	return &App{
		paths: paths,
		reg:   reg,
		res:   res,
		inst:  core.Installer{Paths: paths},
		act:   core.Activator{Paths: paths, Resolver: res, Registry: reg, VsmExe: locateVsmCLI()},
	}
}

// locateVsmCLI 尽力定位 vsm CLI 可执行文件，供 GUI 生成的 shim 用绝对路径调用：
// 优先 GUI 同目录，其次 PATH；都找不到返回空（shim 回退裸命令 "vsm"）。
func locateVsmCLI() string {
	name := "vsm"
	if runtime.GOOS == "windows" {
		name = "vsm.exe"
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), name)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

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

// Status 返回所有工具的当前状态（已装版本 + 生效版本 + 来源）。
func (a *App) Status() ([]ToolStatus, error) {
	cwd, _ := os.Getwd()
	out := make([]ToolStatus, 0, len(plugin.Names()))
	for _, name := range plugin.Names() {
		installed, _ := a.reg.Installed(name)
		sort.Slice(installed, func(i, j int) bool {
			return core.CompareVersions(installed[i], installed[j]) > 0
		})
		r := a.res.Resolve(name, cwd)
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

// Install 安装某工具版本（version 可为 latest / 模糊版本）。安装进度通过
// "install:progress" 事件实时推送到前端。
// Install 安装某工具版本；dir 为空装到默认根目录，非空装到该自定义目录（链接进 vsm）。
func (a *App) Install(tool, version, dir string) error {
	p, ok := plugin.Get(tool)
	if !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	resolved, err := resolveRemote(p, version)
	if err != nil {
		return err
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "install:progress", "解析到版本 "+resolved)
	}
	err = a.inst.Install(p, resolved, dir, func(msg string) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "install:progress", msg)
		}
	})
	if err != nil {
		return err
	}
	return a.act.GenerateShims()
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
func (a *App) GetInstallRoot() string { return a.paths.Installs() }

// SetInstallRoot 设置全局安装根目录（dir 为空恢复默认），并热重载使之立即生效。
func (a *App) SetInstallRoot(dir string) error {
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	path := a.paths.InstallRootFile()
	dir = strings.TrimSpace(dir)
	if dir == "" {
		_ = os.Remove(path)
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(dir+"\n"), 0o644); err != nil {
			return err
		}
	}
	a.reload()
	return nil
}

// reload 依据配置文件重新加载 paths 与各核心组件（用于安装根目录运行时切换即时生效）。
func (a *App) reload() {
	paths, _ := core.DefaultPaths()
	reg := core.Registry{Paths: paths}
	res := core.Resolver{Paths: paths, Registry: reg}
	a.paths = paths
	a.reg = reg
	a.res = res
	a.inst = core.Installer{Paths: paths}
	a.act = core.Activator{Paths: paths, Resolver: res, Registry: reg, VsmExe: locateVsmCLI()}
}

// SetGlobal 设全局默认版本（写 ~/.vsm/.tool-versions）。
func (a *App) SetGlobal(tool, version string) error {
	if _, ok := plugin.Get(tool); !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	target := a.paths.GlobalToolVersions()
	tv := config.LoadOrNew(target)
	tv.Set(tool, version)
	return tv.Save(target)
}

// Uninstall 卸载某版本。
func (a *App) Uninstall(tool, version string) error {
	if err := a.inst.Uninstall(tool, version); err != nil {
		return err
	}
	return a.act.GenerateShims()
}

// DataDir 返回 VSM 数据目录，便于前端展示。
func (a *App) DataDir() string { return a.paths.Root }

// DetectSystem 探测系统 PATH 中所有非 vsm 管理的语言运行时（含同一语言的多版本，
// 如 java 1.8/11/17/21）。供 GUI 顶部“系统检测”总览一次性列出全部。
func (a *App) DetectSystem() []core.SystemVersion {
	return core.DetectSystem(plugin.DetectCommands(), a.paths.Shims())
}

// DetectTool 仅探测某个工具的系统版本，供详情页按需调用，避免全量扫描的耗时。
func (a *App) DetectTool(tool string) []core.SystemVersion {
	cmds := plugin.DetectCommands()
	names, ok := cmds[tool]
	if !ok {
		names = []string{tool}
	}
	return core.DetectSystem(map[string][]string{tool: names}, a.paths.Shims())
}

// AdoptTool 把系统检测到的某版本纳管进 vsm（创建链接），随后重建 shim。
func (a *App) AdoptTool(tool, version, path string) error {
	p, ok := plugin.Get(tool)
	if !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	if err := a.inst.Adopt(p, version, path); err != nil {
		return err
	}
	return a.act.GenerateShims()
}

// SetLocal 把某版本写入“当前工作目录”的 .tool-versions（等价于 vsm use 不带 -g）。
func (a *App) SetLocal(tool, version string) error {
	if _, ok := plugin.Get(tool); !ok {
		return fmt.Errorf("不支持的工具: %s", tool)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	target := filepath.Join(cwd, ".tool-versions")
	tv := config.LoadOrNew(target)
	tv.Set(tool, version)
	return tv.Save(target)
}

// CurrentDir 返回 GUI 进程当前工作目录（local 版本写入位置），供前端展示。
func (a *App) CurrentDir() string {
	cwd, _ := os.Getwd()
	return cwd
}

// GetMirror 返回当前下载镜像设置（cn / off）。优先环境变量 VSM_MIRROR，其次配置文件。
func (a *App) GetMirror() string {
	if v := strings.TrimSpace(os.Getenv("VSM_MIRROR")); v != "" {
		return strings.ToLower(v)
	}
	b, err := os.ReadFile(filepath.Join(a.paths.Root, "mirror"))
	if err != nil {
		return "off"
	}
	if v := strings.ToLower(strings.TrimSpace(string(b))); v != "" {
		return v
	}
	return "off"
}

// SetMirror 写入下载镜像设置（cn 国内加速 / off 关闭）。
func (a *App) SetMirror(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "cn" && mode != "off" {
		return fmt.Errorf("不支持的镜像: %s（可选 cn / off）", mode)
	}
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.paths.Root, "mirror"), []byte(mode+"\n"), 0o644)
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
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	path := filepath.Join(a.paths.Root, "proxy")
	if stored == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(stored+"\n"), 0o644)
}

// PathInit 在 Windows 上把 shims 目录写入用户级 PATH 并广播，新终端即时生效。
func (a *App) PathInit() (string, error) {
	if err := a.paths.EnsureDirs(); err != nil {
		return "", err
	}
	if err := a.act.GenerateShims(); err != nil {
		return "", err
	}
	return core.PersistShimsPath(a.paths.Shims())
}

// AdoptAll 纳管系统检测到的全部版本（跳过已纳管/已装），返回成功纳管数量。
func (a *App) AdoptAll() (int, error) {
	return a.adoptList(core.DetectSystem(plugin.DetectCommands(), a.paths.Shims())), nil
}

// AdoptToolAll 纳管某工具系统检测到的全部版本，返回成功纳管数量。
func (a *App) AdoptToolAll(tool string) (int, error) {
	cmds := plugin.DetectCommands()
	names, ok := cmds[tool]
	if !ok {
		names = []string{tool}
	}
	return a.adoptList(core.DetectSystem(map[string][]string{tool: names}, a.paths.Shims())), nil
}

func (a *App) adoptList(sys []core.SystemVersion) int {
	n := 0
	for _, s := range sys {
		p, ok := plugin.Get(s.Tool)
		if !ok || s.Version == "" || a.reg.IsInstalled(s.Tool, s.Version) {
			continue
		}
		if err := a.inst.Adopt(p, s.Version, s.Path); err == nil {
			n++
		}
	}
	_ = a.act.GenerateShims()
	return n
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
