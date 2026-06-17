//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

// hideConsole 让子进程不弹出控制台黑窗。
//
// Windows 下的 GUI 程序(用 -H windowsgui 链接)在调用 console 子程序时——比如 detect
// 探测各语言的 `xxx --version`、解压用的 tar、写 PATH 用的 powershell、建 junction 用的
// mklink——系统默认会为每个子进程闪出一个 cmd 黑窗。设置 CREATE_NO_WINDOW 即可抑制。
//
// 注意:不要用于需要在终端前台交互/显示输出的命令(如 `vsm exec`)。
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
