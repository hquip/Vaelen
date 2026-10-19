package database

import (
	"os/exec"
	"syscall"
)

func configureBackground(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000200}
}
