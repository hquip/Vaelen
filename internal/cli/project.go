package cli

import (
	"fmt"
	"vsm/internal/core"
)

func (a *app) cmdProject(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: vsm project init [--mode local|versions|docker] [--image 镜像] | status [--json]")
	}
	switch args[0] {
	case "import":
		apply := false
		source, file := "mise", ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--apply":
				apply = true
			case "--json":
			case "--from", "--file":
				if i+1 >= len(args) {
					return fmt.Errorf("%s needs a value", args[i])
				}
				key := args[i]
				i++
				if key == "--from" {
					source = args[i]
				} else {
					file = args[i]
				}
			default:
				return fmt.Errorf("unknown import option %s", args[i])
			}
		}
		if source != "mise" {
			return fmt.Errorf("--from currently accepts mise")
		}
		preview, err := core.ImportMise(a.cwd, file, apply)
		if err != nil {
			return err
		}
		return printJSON(preview)
	case "configure":
		if len(args) != 1 {
			return fmt.Errorf("用法: vsm project configure")
		}
		w, err := core.InitializeWorkspace(a.cwd)
		if err != nil {
			return err
		}
		fmt.Println("项目配置:", w.File)
		return nil
	case "trust":
		if len(args) != 1 {
			return fmt.Errorf("用法: vsm project trust")
		}
		w, err := core.FindWorkspace(a.cwd)
		if err != nil {
			return err
		}
		if w == nil {
			return fmt.Errorf("没有 vsm.json")
		}
		if err := w.Trust(); err != nil {
			return err
		}
		fmt.Println("已信任当前项目配置；配置或环境文件变化后需重新信任")
		return nil
	case "inspect":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			return fmt.Errorf("用法: vsm project inspect [--json]")
		}
		w, err := core.FindWorkspace(a.cwd)
		if err != nil {
			return err
		}
		if w == nil {
			return printJSON(map[string]any{"configured": false})
		}
		plan, err := core.PlanTasks(a.cwd, nil)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"configured": true, "root": w.Root, "trusted": w.Trusted(), "tools": w.Config.Tools, "environment_keys": w.EnvironmentKeys(), "env_files": w.Config.EnvFiles, "tasks": plan.Tasks})
	case "init":
		mode, image := "local", ""
		for i := 1; i < len(args); i++ {
			if i+1 >= len(args) {
				return fmt.Errorf("选项 %s 缺少值", args[i])
			}
			switch args[i] {
			case "--mode":
				mode = args[i+1]
			case "--image":
				image = args[i+1]
			default:
				return fmt.Errorf("未知选项: %s", args[i])
			}
			i++
		}
		p, err := core.InitProject(a.cwd, mode, image)
		if err != nil {
			return err
		}
		return printJSON(p)
	case "status":
		rest, asJSON := jsonFlag(args[1:])
		if len(rest) > 0 {
			return fmt.Errorf("未知 project status 参数")
		}
		p, err := core.FindProject(a.cwd)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(map[string]any{"initialized": p != nil, "project": p})
		}
		if p == nil {
			fmt.Println("项目环境尚未初始化；执行 vsm project init（默认 local）")
			return nil
		}
		fmt.Printf("项目: %s\n隔离模式: %s\n", p.Root, p.Mode)
		if p.Mode == "docker" {
			fmt.Println("容器镜像:", p.Image)
		} else {
			fmt.Println("工具目录:", p.Paths(a.paths).Installs())
		}
		return nil
	}
	return fmt.Errorf("未知 project 子命令 %q", args[0])
}
