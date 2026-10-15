package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"vsm/internal/config"
	"vsm/internal/core"
	"vsm/internal/netcfg"
	"vsm/internal/plugin"
)

func (a *app) cmdInstall(args []string) error {
	var rest []string
	customDir := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" || args[i] == "-p" {
			// 结尾裸 --path 此前会被当成工具名，报出「不支持的工具: --path」这种误导性错误
			if i+1 >= len(args) {
				return fmt.Errorf("--path 需要指定目录，例如: vsm install go 1.22.0 --path D:\\sdk")
			}
			customDir = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) < 1 {
		return fmt.Errorf("用法: vsm install <tool> [version] [--path <安装目录>]")
	}
	if len(rest) > 2 {
		return fmt.Errorf("参数过多: %s（用法: vsm install <tool> [version] [--path <安装目录>]）",
			strings.Join(rest[2:], " "))
	}
	tool := rest[0]
	p, ok := plugin.Get(tool)
	if !ok {
		return unsupported(tool)
	}
	spec := "latest"
	if len(rest) >= 2 {
		spec = rest[1]
	}
	// 相对路径会随 cwd 漂移（在 A 目录装的、到 B 目录就找不到了），统一转绝对路径
	if customDir != "" {
		abs, err := filepath.Abs(customDir)
		if err != nil {
			return fmt.Errorf("无法解析 --path %q: %w", customDir, err)
		}
		customDir = abs
	}
	version, err := resolveRemote(p, spec)
	if err != nil {
		return err
	}
	fmt.Printf("安装 %s %s ...\n", tool, version)
	if err := a.inst.Install(p, version, customDir, func(msg string) { fmt.Println("  " + msg) }); err != nil {
		return err
	}
	if err := a.act.GenerateShims(); err != nil {
		return err
	}
	fmt.Printf("完成。可用 `vsm use %s %s` 设为当前版本。\n", tool, version)
	return nil
}

func (a *app) cmdInstallRoot(args []string) error {
	path := a.paths.InstallRootFile()
	if len(args) == 0 {
		fmt.Println("当前安装根目录:", a.paths.Installs())
		fmt.Println("修改: vsm install-root <目录>   恢复默认: vsm install-root default")
		return nil
	}
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	if args[0] == "default" {
		// 此前忽略删除错误，会在文件只读/被占用时谎报「已恢复默认」，
		// 之后安装仍落在旧的自定义目录里。
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("恢复默认安装根目录失败: %w", err)
		}
		fmt.Println("已恢复默认安装根目录:", a.paths.Installs())
		return nil
	}
	dir := args[0]
	// 相对路径会随 cwd 漂移：在 A 目录设的根目录，到 B 目录就失效了。
	// 统一转成绝对路径后再落盘。
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("无法解析目录 %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(path, []byte(abs+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("已设置安装根目录:", abs)
	return nil
}

func (a *app) cmdUninstall(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("用法: vsm uninstall <tool> <version>")
	}
	if len(args) > 2 {
		return fmt.Errorf("参数过多: %s（用法: vsm uninstall <tool> <version>）", strings.Join(args[2:], " "))
	}
	tool, version := args[0], args[1]
	if err := a.inst.Uninstall(tool, version); err != nil {
		return err
	}
	// shim 生成失败必须让用户看见：残留的 shim 会指向已删除的版本，
	// 之后每条 go/node 命令都会报「未找到可用版本」，很难归因。
	if err := a.act.GenerateShims(); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 重新生成 shim 失败:", err)
	}
	fmt.Printf("已卸载 %s %s\n", tool, version)
	return nil
}

func (a *app) cmdUse(args []string) error {
	global := false
	var rest []string
	parsingFlags := true
	for _, x := range args {
		if parsingFlags && x == "--" {
			parsingFlags = false
			continue
		}
		if parsingFlags && (x == "-g" || x == "--global") {
			global = true
			continue
		}
		rest = append(rest, x)
	}
	if len(rest) < 2 {
		return fmt.Errorf("用法: vsm use [-g] <tool> <version>")
	}
	if len(rest) > 2 {
		// 此前静默忽略多余参数，`vsm use go 1.22.0 1.23.0` 会写错版本还不报错
		return fmt.Errorf("参数过多: %s（用法: vsm use [-g] <tool> <version>）", strings.Join(rest[2:], " "))
	}
	tool, version := strings.TrimSpace(rest[0]), strings.TrimSpace(rest[1])
	if _, ok := plugin.Get(tool); !ok {
		return unsupported(tool)
	}
	// 空版本会写出 "go " 这种下次解析就被丢弃的行，等于「报告成功但没生效」
	if version == "" {
		return fmt.Errorf("版本号不能为空（用法: vsm use [-g] <tool> <version>）")
	}
	if strings.ContainsAny(version, " \t") {
		return fmt.Errorf("版本号不能包含空格: %q", version)
	}

	var target string
	if global {
		if err := a.paths.EnsureDirs(); err != nil {
			return err
		}
		target = a.paths.GlobalToolVersions()
	} else {
		target = filepath.Join(a.cwd, ".tool-versions")
	}

	tv, err := config.LoadOrNew(target)
	if err != nil {
		return err
	}
	if warns := tv.Warnings(); len(warns) > 0 {
		fmt.Fprintf(os.Stderr, "提示: %s 中有 %d 行缺少版本号，已跳过: %s\n",
			target, len(warns), strings.Join(warns, ", "))
	}
	tv.Set(tool, version)
	if err := tv.Save(target); err != nil {
		return err
	}
	fmt.Printf("已设置 %s = %s （%s）\n", tool, version, target)
	if a.res.Resolve(tool, a.cwd).Version == "" {
		fmt.Printf("提示: 未找到已安装且匹配 %q 的版本，运行 `vsm install %s %s`\n", version, tool, version)
	}
	return nil
}

func (a *app) cmdList(args []string) error {
	args, asJSON := jsonFlag(args)
	tools := plugin.Names()
	if len(args) >= 1 {
		if _, ok := plugin.Get(args[0]); !ok {
			return unsupported(args[0])
		}
		tools = []string{args[0]}
	}
	type item struct {
		Tool      string   `json:"tool"`
		Current   string   `json:"current"`
		Installed []string `json:"installed"`
	}
	var items []item
	for _, tool := range tools {
		// 此前忽略这里的错误：权限/IO 问题会显示成「(无已安装版本)」并退出 0，
		// 可能诱导用户重新下载几个 G。现在让真实错误冒出来。
		installed, err := a.reg.Installed(tool)
		if err != nil {
			return fmt.Errorf("读取 %s 的已安装版本失败: %w", tool, err)
		}
		sortVersionsDesc(installed)
		current := a.res.Resolve(tool, a.cwd)
		if installed == nil {
			installed = []string{}
		}
		items = append(items, item{tool, current.Version, installed})
	}
	if asJSON {
		return printJSON(items)
	}
	for _, it := range items {
		fmt.Printf("%s:\n", it.Tool)
		if len(it.Installed) == 0 {
			fmt.Println("    (无已安装版本)")
			continue
		}
		for _, v := range it.Installed {
			marker := " "
			if v == it.Current {
				marker = "*"
			}
			fmt.Printf("  %s %s\n", marker, v)
		}
	}
	return nil
}

func (a *app) cmdListAll(args []string) error {
	args, asJSON := jsonFlag(args)
	if len(args) < 1 {
		return fmt.Errorf("用法: vsm list-all <tool> [--json]")
	}
	p, ok := plugin.Get(args[0])
	if !ok {
		return unsupported(args[0])
	}
	versions, err := p.ListAll()
	if err != nil {
		return err
	}
	if asJSON {
		if versions == nil {
			versions = []string{}
		}
		return printJSON(versions)
	}
	for _, v := range versions {
		fmt.Println(v)
	}
	return nil
}

func (a *app) cmdCurrent(args []string) error {
	if _, asJSON := jsonFlag(args); asJSON {
		var out []toolStatusJSON
		for _, st := range a.collectStatus() {
			if st.Spec != "" {
				out = append(out, st)
			}
		}
		if out == nil {
			out = []toolStatusJSON{}
		}
		return printJSON(out)
	}
	for _, tool := range plugin.Names() {
		r := a.res.Resolve(tool, a.cwd)
		if r.Spec == "" {
			fmt.Printf("%-6s  -\n", tool)
			continue
		}
		status := r.Version
		if status == "" {
			status = fmt.Sprintf("(要求 %s, 未安装)", r.Spec)
		}
		fmt.Printf("%-6s  %-22s  <- %s\n", tool, status, r.Source)
	}
	return nil
}

func (a *app) cmdWhich(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: vsm which <tool>")
	}
	path, err := a.act.Which(args[0], a.cwd)
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

func (a *app) cmdExec(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: vsm exec <tool> [args...]  或  vsm exec <tool> -- <cmd> [args...]")
	}
	tool := args[0]
	rest := args[1:]

	cmdName := tool
	cmdArgs := rest
	if i := indexOf(rest, "--"); i >= 0 {
		after := rest[i+1:]
		if len(after) == 0 {
			return fmt.Errorf("-- 之后需要指定要执行的命令")
		}
		cmdName = after[0]
		cmdArgs = after[1:]
	}
	return a.act.Run(tool, a.cwd, cmdName, cmdArgs)
}

func indexOf(ss []string, target string) int {
	for i, s := range ss {
		if s == target {
			return i
		}
	}
	return -1
}

func (a *app) cmdReshim(args []string) error {
	if err := a.act.GenerateShims(); err != nil {
		return err
	}
	fmt.Println("已重新生成 shim:", a.paths.Shims())
	return nil
}

func (a *app) cmdActivate(args []string) error {
	shell := "bash"
	if len(args) >= 1 {
		shell = args[0]
	}
	script, err := core.HookScript(shell, a.paths.Shims(), core.VsmCommand(a.act.VsmExe))
	if err != nil {
		return err
	}
	fmt.Println(script)
	return nil
}

func (a *app) cmdEnv(args []string) error {
	shell := "bash"
	if len(args) >= 1 {
		shell = args[0]
	}
	dirs, env := a.act.EnvFor(a.cwd)
	script, err := core.EnvScript(shell, dirs, env)
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

// cmdPathInit 让新开的终端自动启用 vsm：
//   - Windows：把 shims 写入用户级 PATH 并广播；
//   - macOS / Linux：把 `eval "$(vsm activate <shell>)"` 写入 shell 配置文件
//     （此前这里直接报错，macOS 用户只能手动改 ~/.zshrc）。
func (a *app) cmdPathInit(args []string) error {
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	if err := a.act.GenerateShims(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		res, err := core.PersistShimsPath(a.paths.Shims())
		if err != nil {
			return err
		}
		if res == "added" {
			fmt.Println("已把 shims 目录写入用户级 PATH 并广播：", a.paths.Shims())
			fmt.Println("新开终端即可直接使用 go/node 等（按目录 .tool-versions 自动切换）。")
		} else {
			fmt.Println("shims 目录已在用户 PATH 中：", a.paths.Shims())
		}
		return nil
	}
	shell := core.DefaultShell()
	if len(args) >= 1 {
		shell = args[0]
	}
	rc, res, err := core.PersistShellHook(shell, core.VsmCommand(a.act.VsmExe))
	if err != nil {
		return err
	}
	if res == "added" {
		fmt.Println("已写入", rc)
		fmt.Println("新开终端即生效（或执行 `source " + rc + "` 立即生效）。")
	} else {
		fmt.Println(rc, "中已配置 vsm，无需重复写入。")
	}
	return nil
}

func (a *app) cmdDetect(args []string) error {
	_, asJSON := jsonFlag(args)
	sys := core.DetectSystem(plugin.DetectCommands(), a.paths.Shims())
	sort.Slice(sys, func(i, j int) bool {
		if sys[i].Tool != sys[j].Tool {
			return sys[i].Tool < sys[j].Tool
		}
		return core.CompareVersions(sys[i].Version, sys[j].Version) > 0
	})
	if asJSON {
		if sys == nil {
			sys = []core.SystemVersion{}
		}
		return printJSON(map[string]any{"runtimes": sys, "env": core.DetectEnvVars()})
	}
	fmt.Println("系统 PATH 中检测到的语言运行时:")
	if len(sys) == 0 {
		fmt.Println("  (未检测到)")
	}
	for _, s := range sys {
		ver := s.Version
		if ver == "" {
			ver = "?"
		}
		fmt.Printf("  %-8s %-14s %s\n", s.Tool, ver, s.Path)
	}
	if env := core.DetectEnvVars(); len(env) > 0 {
		fmt.Println("\n相关环境变量:")
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %-12s %s\n", k, env[k])
		}
	}
	return nil
}

func (a *app) cmdAdopt(args []string) error {
	if len(args) >= 1 && (args[0] == "--all" || args[0] == "-a") {
		return a.adoptAll()
	}
	if len(args) < 1 {
		return fmt.Errorf("用法: vsm adopt <tool> [version] [可执行路径]  或  vsm adopt --all")
	}
	tool := args[0]
	p, ok := plugin.Get(tool)
	if !ok {
		return unsupported(tool)
	}
	var version, execPath string
	if len(args) >= 2 {
		version = args[1]
	}
	if len(args) >= 3 {
		execPath = args[2]
	}
	if execPath == "" {
		names := plugin.DetectCommands()[tool]
		if names == nil {
			names = []string{tool}
		}
		sys := core.DetectSystem(map[string][]string{tool: names}, a.paths.Shims())
		if len(sys) == 0 {
			return fmt.Errorf("系统中未检测到 %s；可显式提供：vsm adopt %s <version> <可执行路径>", tool, tool)
		}
		switch {
		case version != "":
			for _, s := range sys {
				if s.Version == version {
					execPath = s.Path
				}
			}
			if execPath == "" {
				return fmt.Errorf("系统检测中没有 %s %s", tool, version)
			}
		case len(sys) == 1:
			version, execPath = sys[0].Version, sys[0].Path
		default:
			var b strings.Builder
			fmt.Fprintf(&b, "检测到多个 %s 版本，请指定其一：vsm adopt %s <version>\n", tool, tool)
			for _, s := range sys {
				fmt.Fprintf(&b, "  %-12s %s\n", s.Version, s.Path)
			}
			return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
		}
	}
	if err := a.inst.Adopt(p, version, execPath); err != nil {
		return err
	}
	if err := a.act.GenerateShims(); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 重新生成 shim 失败:", err)
	}
	fmt.Printf("已纳管 %s %s\n  -> %s\n", tool, version, execPath)
	return nil
}

func (a *app) adoptAll() error {
	sys := core.DetectSystem(plugin.DetectCommands(), a.paths.Shims())
	n := 0
	for _, s := range sys {
		p, ok := plugin.Get(s.Tool)
		if !ok || s.Version == "" {
			continue
		}
		if a.reg.IsInstalled(s.Tool, s.Version) {
			continue
		}
		if err := a.inst.Adopt(p, s.Version, s.Path); err != nil {
			fmt.Printf("  跳过 %s %s：%v\n", s.Tool, s.Version, err)
			continue
		}
		fmt.Printf("  纳管 %s %s\n", s.Tool, s.Version)
		n++
	}
	if err := a.act.GenerateShims(); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 重新生成 shim 失败:", err)
	}
	fmt.Printf("共纳管 %d 个版本\n", n)
	return nil
}

func (a *app) cmdMirror(args []string) error {
	path := filepath.Join(a.paths.Root, "mirror")
	if len(args) == 0 {
		cur := "off"
		if b, err := os.ReadFile(path); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				cur = v
			}
		}
		if v := strings.TrimSpace(os.Getenv("VSM_MIRROR")); v != "" {
			cur = v + "（来自环境变量 VSM_MIRROR，优先生效）"
		}
		fmt.Println("当前下载镜像:", cur)
		fmt.Println("可选: cn（国内加速：go/node/github）, off（关闭）")
		return nil
	}
	mode := strings.ToLower(args[0])
	if mode != "cn" && mode != "off" {
		return fmt.Errorf("不支持的镜像: %s（可选 cn / off）", mode)
	}
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(path, []byte(mode+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("已设置下载镜像:", mode)
	return nil
}

func (a *app) cmdProxy(args []string) error {
	path := filepath.Join(a.paths.Root, "proxy")
	if len(args) == 0 {
		value, source := netcfg.Describe()
		switch {
		case value == "":
			fmt.Println("当前代理: 未设置（跟随系统环境变量 HTTP_PROXY/HTTPS_PROXY）")
			if env := netcfg.SystemProxyEnv(); env != "" {
				fmt.Println("  系统环境变量代理:", env)
			}
		case strings.EqualFold(value, "off"):
			fmt.Printf("当前代理: 已禁用（强制直连）  <- %s\n", source)
		default:
			fmt.Printf("当前代理: %s  <- %s\n", value, source)
		}
		fmt.Println("用法:")
		fmt.Println("  vsm proxy <url>     设置代理，如 http://127.0.0.1:7890 或 socks5://127.0.0.1:1080")
		fmt.Println("  vsm proxy off       强制直连（忽略系统环境变量）")
		fmt.Println("  vsm proxy system    跟随系统环境变量（默认）")
		return nil
	}
	stored, err := netcfg.Normalize(args[0])
	if err != nil {
		return err
	}
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	switch stored {
	case "":
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清除代理设置失败: %w", err)
		}
		fmt.Println("已恢复跟随系统环境变量（HTTP_PROXY/HTTPS_PROXY）")
	case "off":
		if err := config.WriteFileAtomic(path, []byte("off\n"), 0o644); err != nil {
			return err
		}
		fmt.Println("已禁用代理（强制直连）")
	default:
		if err := config.WriteFileAtomic(path, []byte(stored+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Println("已设置代理:", stored)
	}
	return nil
}

// resolveRemote 把版本要求（如 "20" / "latest"）解析为远端的具体版本号。
func resolveRemote(p plugin.Plugin, spec string) (string, error) {
	if spec == "latest" || spec == "" {
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

func sortVersionsDesc(vs []string) {
	sort.Slice(vs, func(i, j int) bool {
		return core.CompareVersions(vs[i], vs[j]) > 0
	})
}

func unsupported(tool string) error {
	return fmt.Errorf("不支持的工具: %s（可用: %s）", tool, strings.Join(plugin.Names(), ", "))
}
