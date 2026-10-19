package database

import (
	"fmt"
	"syscall"
)

func processIdentity(pid int) (string, error) {
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err == syscall.Errno(87) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(handle)
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", err
	}
	if exited.HighDateTime != 0 || exited.LowDateTime != 0 {
		return "", nil
	}
	return fmt.Sprintf("%08x%08x", created.HighDateTime, created.LowDateTime), nil
}
