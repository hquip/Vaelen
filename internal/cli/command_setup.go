package cli

import (
	"fmt"
	"os"

	"vsm/internal/core"
	"vsm/internal/logging"
)

func registerCommand() error {
	paths, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	act := core.Activator{Paths: paths, VsmExe: exe, TraceID: logging.NewOperationID()}
	result, err := act.RegisterWindowsCommand()
	if err != nil {
		return err
	}
	fmt.Println("vsm 命令已注册（" + result.Status + "）: " + result.Shims)
	fmt.Println("新 cmd 自动加载；尚未加载的已有 cmd 执行一次: " + result.CurrentCmd)
	return nil
}
