package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"vsm/internal/logging"
	"vsm/internal/plugin"
)

// Diagnostics remain accessible when project/install-root config is broken.
func runLogs(args []string) error {
	store, err := logging.Default()
	if err != nil {
		return err
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "_capture-cmd":
			// Called by our startup helper. Diagnostics must not affect shell
			// startup, print output, or replace the helper's final exit code.
			if len(args) == 3 && (args[1] == "before" || args[1] == "after") && (args[2] == "ON" || args[2] == "OFF") {
				_ = captureActivationLogs(nil, args[1], args[2], true)
			}
			return nil
		case "diagnose":
			if len(args) > 2 {
				return fmt.Errorf("用法: vsm logs diagnose [tool]")
			}
			return captureActivationLogs(args[1:], "manual", "", false)
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

func captureActivationLogs(tools []string, phase, mode string, quiet bool) error {
	if quiet && logging.Level() == "off" {
		return nil
	}
	if !quiet && logging.Level() != "info" && logging.Level() != "debug" {
		return fmt.Errorf("详细诊断需要 VSM_LOG_LEVEL=info 或 debug，当前为 %s", logging.Level())
	}
	operationID := os.Getenv("VSM_ACTIVATION_OPERATION")
	if operationID == "" {
		operationID = logging.NewOperationID()
	}
	scope := "current-process"
	if quiet {
		scope = "cmd-session"
		if os.Getenv("_VSM_CMD_SCOPE") == "controlled-cmd" {
			scope = "controlled-cmd"
		}
	}
	dir, _ := os.Getwd()
	exe, _ := os.Executable()
	logging.Record("info", "activation", "capture.started", logging.Fields{"operation_id": operationID, "scope": scope, "phase": phase, "cmd_mode": mode, "directory": dir, "cli": exe, "helper_session": os.Getenv("_VSM_CMD_TRACE_ID")})
	a, err := newApp()
	if err != nil {
		logging.Record("warn", "activation", "capture.failed", logging.Fields{"operation_id": operationID, "scope": scope, "phase": phase, "error": logging.ErrorMessage(err)})
		return err
	}
	a.act.TraceID = operationID
	for _, tool := range tools {
		if _, ok := plugin.Get(tool); !ok {
			return unsupported(tool)
		}
	}
	a.act.LogActivationSnapshot(scope, phase, mode, a.cwd, tools)
	logging.Record("info", "activation", "capture.completed", logging.Fields{"operation_id": operationID, "scope": scope, "phase": phase, "helper_session": os.Getenv("_VSM_CMD_TRACE_ID")})
	if !quiet {
		store, err := logging.Default()
		if err != nil {
			return err
		}
		snapshot, err := store.Read(logging.MaxTail, "")
		if err != nil {
			return fmt.Errorf("读取刚写入的诊断失败: %w", err)
		}
		recorded := false
		for _, entry := range snapshot.Entries {
			if entry.Event == "capture.completed" && entry.Fields["operation_id"] == operationID {
				recorded = true
				break
			}
		}
		if !recorded {
			return fmt.Errorf("诊断未写入日志，请检查 %s 的权限和可用空间", store.Directory())
		}
		fmt.Println("当前终端诊断已记录，编号:", operationID)
		fmt.Println("日志目录:", store.Directory())
		fmt.Println("运行 vsm logs export <新文件.jsonl>，或在 GUI 日志窗口导出。")
	}
	return nil
}
