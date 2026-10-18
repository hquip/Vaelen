package cli

import (
	"context"
	"fmt"
	"os"
	"time"
	"vsm/internal/buildinfo"
	"vsm/internal/updater"
)

// Updating VSM is independent of a project's language configuration. Keep this
// entry point before newApp so broken project files cannot prevent recovery.
func runUpdate(args []string) error {
	check, asJSON, gui := false, false, false
	version := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--json":
			asJSON = true
		case "--gui":
			gui = true
		case "--version":
			if i+1 >= len(args) {
				return fmt.Errorf("--version 需要版本号")
			}
			i++
			version = args[i]
		default:
			return fmt.Errorf("用法: vsm update [--check] [--version <版本>] [--gui] [--json]")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := updater.New()
	// Human-readable progress belongs to stderr; JSON stdout remains a single
	// result object, including when output is consumed by an AI client.
	if !asJSON && !check {
		client = client.WithProgress(updateProgressPrinter(os.Stderr))
	}
	info, err := client.Check(ctx, buildinfo.DisplayVersion(), version)
	if err != nil {
		return err
	}
	if check {
		if asJSON {
			return printJSON(info)
		}
		fmt.Printf("当前版本: %s\nGitHub 最新版本: %s\n%s\n", info.Current, info.Latest, info.URL)
		if info.Available {
			fmt.Println("可用 vsm update 更新；同步相邻 GUI 可加 --gui")
		}
		return nil
	}
	if !info.Available {
		if asJSON {
			return printJSON(map[string]any{"updated": false, "current": info.Current, "latest": info.Latest})
		}
		fmt.Printf("当前 %s 已不低于目标 %s，无需更新。\n", info.Current, info.Latest)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	result, err := client.Apply(ctx, info, exe, gui)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(result)
	}
	fmt.Printf("已更新到 %s。下次启动使用新版本。\n", result.Version)
	for _, path := range result.Updated {
		fmt.Println("  " + path)
	}
	for _, path := range result.Backups {
		fmt.Println("旧程序备份（退出后可删除）: " + path)
	}
	return nil
}
