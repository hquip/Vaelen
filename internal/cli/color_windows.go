//go:build windows

package cli

import (
	"golang.org/x/sys/windows"
	"os"
)

func enableTerminalColor(file *os.File) bool {
	var mode uint32
	handle := windows.Handle(file.Fd())
	if windows.GetConsoleMode(handle, &mode) != nil {
		return false
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
