package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vsm/internal/core"
	"vsm/internal/database"
)

func runDatabase(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: vsm db use|switch|start|stop|list|logs|connection|exec（查看 vsm help db）")
	}
	if args[0] == "engines" {
		if len(args) != 1 {
			return fmt.Errorf("用法: vsm db engines")
		}
		return printJSON(database.Engines)
	}
	paths, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	manager := database.New(paths)
	interrupt, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(interrupt, 8*time.Minute)
	defer cancel()
	rest, asJSON := jsonFlag(args[1:])
	manager.Progress = func(text string) {
		if !asJSON {
			fmt.Fprintln(os.Stderr, text)
		}
	}
	show := func(s database.Status, err error) error {
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(s)
		}
		state := "已停止"
		if s.Running {
			state = "运行中"
		}
		if s.Error != "" {
			state = "状态待检查"
		}
		version := s.Version
		if actual := s.Versions[s.Version]; actual != nil && actual.ActualVersion != "" {
			version += "（实际 " + actual.ActualVersion + "）"
		}
		fmt.Printf("%s · %s %s · %s · %s\n地址: 127.0.0.1:%d\n数据: %s\n", s.Name, s.Engine, version, s.Backend, state, s.Port, s.DataLocation)
		if s.Error != "" {
			fmt.Println("提示:", s.Error)
		}
		return nil
	}
	switch args[0] {
	case "use":
		if len(rest) < 2 {
			return fmt.Errorf("用法: vsm db use <postgres|mysql|mariadb|redis|valkey> <版本> [--name 名称] [--backend docker|native] [--port 端口]")
		}
		r := database.Request{Engine: rest[0], Version: rest[1], Backend: "docker"}
		for i := 2; i < len(rest); i++ {
			if i+1 >= len(rest) {
				return fmt.Errorf("%s 缺少值", rest[i])
			}
			switch rest[i] {
			case "--name":
				r.Name = rest[i+1]
			case "--backend":
				r.Backend = rest[i+1]
			case "--port":
				port, err := strconv.Atoi(rest[i+1])
				if err != nil {
					return fmt.Errorf("无效端口")
				}
				r.Port = port
			default:
				return fmt.Errorf("未知数据库参数 %s", rest[i])
			}
			i++
		}
		return show(manager.Use(ctx, r))
	case "switch":
		if len(rest) != 2 {
			return fmt.Errorf("用法: vsm db switch <实例名> <版本>")
		}
		return show(manager.Switch(ctx, rest[0], rest[1]))
	case "start", "stop":
		if len(rest) != 1 {
			return fmt.Errorf("用法: vsm db %s <实例名>", args[0])
		}
		if args[0] == "start" {
			return show(manager.Start(ctx, rest[0]))
		}
		return show(manager.Stop(ctx, rest[0]))
	case "list", "status":
		if len(rest) > 0 {
			return fmt.Errorf("用法: vsm db list [--json]")
		}
		list, err := manager.List(ctx)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(list)
		}
		if len(list) == 0 {
			fmt.Println("没有数据库实例；使用 vsm db use postgres 17 --name dev")
		}
		for _, s := range list {
			if err := show(s, nil); err != nil {
				return err
			}
		}
		return nil
	case "versions":
		if len(rest) != 1 {
			return fmt.Errorf("用法: vsm db versions <数据库类型>")
		}
		versions, err := manager.NativeVersions(rest[0])
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(versions)
		}
		fmt.Println(strings.Join(versions, "\n"))
		return nil
	case "logs":
		if len(rest) != 1 {
			return fmt.Errorf("用法: vsm db logs <实例名>")
		}
		text, err := manager.Logs(ctx, rest[0])
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(map[string]string{"logs": text})
		}
		fmt.Print(text)
		return nil
	case "connection":
		if len(rest) != 1 {
			return fmt.Errorf("用法: vsm db connection <实例名> [--json]（包含密码）")
		}
		connection, err := manager.Connection(rest[0])
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(connection)
		}
		fmt.Println(connection.URI)
		return nil
	case "exec":
		// Arguments after -- belong to the database client, including --json.
		if len(args) < 4 || args[2] != "--" {
			return fmt.Errorf("用法: vsm db exec <实例名> -- <客户端参数>")
		}
		text, err := manager.Exec(ctx, args[1], args[3:])
		if text != "" {
			fmt.Print(text)
		}
		return err
	}
	return fmt.Errorf("未知 db 子命令: %s", args[0])
}
