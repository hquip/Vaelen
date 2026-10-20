//go:build !windows

package core

import "fmt"

func (a Activator) VerifyCmdActivation(dir string) (CmdActivationCheck, error) {
	return CmdActivationCheck{}, fmt.Errorf("cmd 激活验证仅用于 Windows")
}

func (a Activator) OpenActivatedCmd(dir string) error {
	return fmt.Errorf("cmd 终端仅用于 Windows，请新开已配置的终端")
}
