package updater

import (
	"os"
	"syscall"
	"unsafe"
)

var updateKernel32 = syscall.NewLazyDLL("kernel32.dll")
var updateLockFileEx = updateKernel32.NewProc("LockFileEx")
var updateUnlockFileEx = updateKernel32.NewProc("UnlockFileEx")

func lockUpdateFile(file *os.File) (func(), error) {
	var overlapped syscall.Overlapped
	ok, _, err := updateLockFileEx.Call(file.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return nil, err
	}
	return func() { _, _, _ = updateUnlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped))) }, nil
}
