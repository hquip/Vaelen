//go:build !windows

package core

import "os/exec"

// hideConsole 在非 Windows 平台无需处理——不存在 GUI 调用子进程闪控制台窗口的问题。
func hideConsole(cmd *exec.Cmd) {}
