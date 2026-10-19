//go:build !windows

package core

import "os/exec"

func configureVersionProbe(c *exec.Cmd, bin, flag string) error { return nil }
