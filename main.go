// Command vsm 是一个多语言版本管理器，统一管理 Go / Node 等语言环境的
// 安装、版本切换与激活。核心逻辑在 internal/ 下，main 仅作入口。
package main

import (
	"os"

	"vsm/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
