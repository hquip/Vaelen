package cli

import (
	"fmt"
	"os"
	"path/filepath"
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
		if (args[i] == "--path" || args[i] == "-p") && i+1 < len(args) {
			customDir = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) < 1 {
		return fmt.Errorf("用法: vsm install <tool> [version] [--path <安装目录>]")
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
	if args[0] == "default" || args[0] == "-" {
		_ = os.Remove(path)
		fmt.Println("已恢复默认安装根目录:", filepath.Join(a.paths.Root, "installs"))
		return nil
	}
	dir := args[0]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(dir+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("已设置安装根目录:", dir)
	return nil
}

func (a *app) cmdUninstall(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("用法: vsm uninstall <tool> <version>")
	}
	tool, version := args[0], args[1]
	if err := a.inst.Uninstall(tool, version); err != nil {
		return err
	}
	_ = a.act.GenerateShims()
	fmt.Printf("已卸载 %s %s\n", tool, version)
	return nil
}

func (a *app) cmdUse(args []string) error {
	global := false
	var rest []string
	for _, x := range args {
		switch x {
		case "-g", "--global":
			global = true
		default:
			rest = append(rest, x)
		}
	}
	if len(rest) < 2 {
		return fmt.Errorf("用法: vsm use [-g] <tool> <version>")
	}
	tool, version := rest[0], rest[1]
	if _, ok := plugin.Get(tool); !ok {
		return unsupported(tool)
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

	tv := config.LoadOrNew(target)
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
	tools := plugin.Names()
	if len(args) >= 1 {
		tools = []string{args[0]}
	}
	for _, tool := range tools {
		installed, _ := a.reg.Installed(tool)
		fmt.Printf("%s:\n", tool)
		if len(installed) == 0 {
			fmt.Println("    (无已安装版本)")
			continue
		}
		sortVersionsDesc(installed)
		current := a.res.Resolve(tool, a.cwd)
		for _, v := range installed {
			marker := " "
			if v == current.Version {
				marker = "*"
			}
			fmt.Printf("  %s %s\n", marker, v)
		}
	}
	return nil
}

func (a *app) cmdListAll(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: vsm list-all <tool>")
	}
	p, ok := plugin.Get(args[0])
	if !ok {
		return unsupported(args[0])
	}
	versions, err := p.ListAll()
	if err != nil {
		return err
	}
	for _, v := range versions {
		fmt.Println(v)
	}
	return nil
}

func (a *app) cmdCurrent(args []string) error {
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
	script, err := core.HookScript(shell, a.paths.Shims())
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

func (a *app) cmdPathInit(args []string) error {
	if err := a.paths.EnsureDirs(); err != nil {
		return err
	}
	if err := a.act.GenerateShims(); err != nil {
		return err
	}
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

func (a *app) cmdDetect(args []string) error {
	fmt.Println("系统 PATH 中检测到的语言运行时:")
	sys := core.DetectSystem(plugin.DetectCommands(), a.paths.Shims())
	sort.Slice(sys, func(i, j int) bool {
		if sys[i].Tool != sys[j].Tool {
			return sys[i].Tool < sys[j].Tool
		}
		return core.CompareVersions(sys[i].Version, sys[j].Version) > 0
	})
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
	_ = a.act.GenerateShims()
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
	_ = a.act.GenerateShims()
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
	if err := os.WriteFile(path, []byte(mode+"\n"), 0o644); err != nil {
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
		_ = os.Remove(path)
		fmt.Println("已恢复跟随系统环境变量（HTTP_PROXY/HTTPS_PROXY）")
	case "off":
		if err := os.WriteFile(path, []byte("off\n"), 0o644); err != nil {
			return err
		}
		fmt.Println("已禁用代理（强制直连）")
	default:
		if err := os.WriteFile(path, []byte(stored+"\n"), 0o644); err != nil {
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
