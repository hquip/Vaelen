package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"vsm/internal/logging"
)

// Diagnostics remain accessible when project/install-root config is broken.
func runLogs(args []string) error {
	store, err := logging.Default()
	if err != nil {
		return err
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "path":
			if len(args) != 1 {
				return fmt.Errorf("用法: vsm logs path")
			}
			fmt.Println(store.Directory())
			return nil
		case "export":
			if len(args) != 2 {
				return fmt.Errorf("用法: vsm logs export <新文件.jsonl>")
			}
			if err := store.Export(args[1]); err != nil {
				return err
			}
			fmt.Println("日志已导出:", args[1])
			return nil
		case "clear":
			if len(args) != 1 {
				return fmt.Errorf("用法: vsm logs clear")
			}
			if err := store.Clear(); err != nil {
				return err
			}
			fmt.Println("已清理 VSM 日志（保留工具、配置和下载缓存）")
			return nil
		default:
			return fmt.Errorf("未知日志子命令；运行 vsm help logs 查看帮助")
		}
	}
	tail, level, asJSON := 100, "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tail":
			if i+1 >= len(args) {
				return fmt.Errorf("--tail 需要条数（1–1000）")
			}
			i++
			tail, err = strconv.Atoi(args[i])
			if err != nil {
				return fmt.Errorf("--tail 需要整数（1–1000）")
			}
		case "--level":
			if i+1 >= len(args) {
				return fmt.Errorf("--level 需要 debug/info/warn/error")
			}
			i++
			level = args[i]
		case "--json":
			asJSON = true
		default:
			return fmt.Errorf("用法: vsm logs [--tail 1–1000] [--level debug|info|warn|error] [--json]")
		}
	}
	snapshot, err := store.Read(tail, level)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(snapshot)
	}
	fmt.Printf("日志目录: %s\n记录级别: %s\n时间: 本地时区（JSON/导出保留 UTC）\n", snapshot.Directory, snapshot.Level)
	if len(snapshot.Entries) == 0 {
		fmt.Println("暂无符合条件的日志")
		return nil
	}
	for _, entry := range snapshot.Entries {
		fields, err := json.Marshal(entry.Fields)
		if err != nil {
			return err
		}
		fmt.Printf("%s %-5s %s %s %s\n", logging.LocalTime(entry.Time), strings.ToUpper(entry.Level), entry.Component, entry.Event, fields)
	}
	return nil
}
