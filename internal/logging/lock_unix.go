//go:build linux || darwin

package logging

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func lockFile(f *os.File) (func(), error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("日志正忙，请稍后重试")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
