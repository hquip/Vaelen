//go:build !windows

package core

import "fmt"

func (a Activator) VerifyCmdActivation(dir string) (CmdActivationCheck, error) {
	return CmdActivationCheck{}, fmt.Errorf("cmd 激活验证仅用于 Windows")
}
