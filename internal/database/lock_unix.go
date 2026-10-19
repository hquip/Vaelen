//go:build !windows

package database

import (
	"fmt"
	"os"
	"syscall"
)

func lockDatabase(f *os.File) (func(), error) {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("数据库实例正在操作，请稍后重试: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }, nil
}
