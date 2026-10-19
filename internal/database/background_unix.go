//go:build !windows

package database

import (
	"os/exec"
	"syscall"
)

func configureBackground(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
