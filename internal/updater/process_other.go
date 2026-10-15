//go:build !windows

package updater

import "os/exec"

func hideConsole(cmd *exec.Cmd) {}
