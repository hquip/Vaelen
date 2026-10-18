package logging

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32.NewProc("LockFileEx")
	unlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func lockFile(f *os.File) (func(), error) {
	var overlapped syscall.Overlapped
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok, _, err := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped))) // exclusive, fail immediately
		if ok != 0 {
			return func() { _, _, _ = unlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped))) }, nil
		}
		if err != syscall.Errno(33) {
			return nil, err
		} // ERROR_LOCK_VIOLATION
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("日志正忙，请稍后重试")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
