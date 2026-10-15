//go:build !windows

package cli

import "os"

func enableTerminalColor(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
