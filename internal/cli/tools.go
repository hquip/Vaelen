package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vsm/internal/core"
	"vsm/internal/plugin"
)

func (a *app) cmdTools(args []string) error {
	if len(args) == 0 || args[0] == "list" || args[0] == "search" {
		if len(args) > 0 {
			args = args[1:]
		}
		rest, asJSON := jsonFlag(args)
		if len(rest) > 1 {
			return fmt.Errorf("用法: vsm tools search [关键词] [--json]")
		}
		query := ""
		if len(rest) > 0 {
			query = rest[0]
		}
		items := plugin.Catalog(query)
		if asJSON {
			return printJSON(items)
		}
		for _, item := range items {
			mode := "识别/切换"
			if item.Installable {
				mode = "可安装"
			}
			if item.Support.Mode == "unavailable" {
				mode = "平台不支持"
			} else if item.Support.Mode == "release" {
				mode = "按发行包匹配"
			}
			fmt.Printf("%-18s %-12s %s · %s\n", item.Name, mode, item.Support.DisplayName, strings.Join(item.Support.Languages, " / "))
		}
		return nil
	}
	if args[0] != "add" || len(args) < 3 {
		return fmt.Errorf("用法: vsm tools add <名称> github:<owner/repo> [--command 命令] [--bin 子目录] [--match 资产关键词]")
	}
	name, source := args[1], args[2]
	if _, exists := plugin.Get(name); exists {
		return fmt.Errorf("工具 %s 已存在，不覆盖已有定义", name)
	}
	if !strings.HasPrefix(source, "github:") {
		return fmt.Errorf("此入口接受 github:owner/repo")
	}
	repo := strings.TrimPrefix(source, "github:")
	command, bin, match := "", "", ""
	for i := 3; i < len(args); i++ {
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires value", args[i])
		}
		value := args[i+1]
		switch args[i] {
		case "--command":
			command = value
		case "--bin":
			bin = value
		case "--match":
			match = value
		default:
			return fmt.Errorf("unknown tool option %s", args[i])
		}
		i++
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
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("工具定义文件已存在")
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Printf("已添加 %s；下次运行可用 vsm install %s latest --require-checksum\n", name, name)
	return nil
}
