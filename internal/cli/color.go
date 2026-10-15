package cli

import (
	"os"
)

func colorize(file *os.File, color, text string) string {
	if _, off := os.LookupEnv("NO_COLOR"); off || os.Getenv("TERM") == "dumb" {
		return text
	}
	if os.Getenv("FORCE_COLOR") != "1" && !enableTerminalColor(file) {
		return text
	}
	return "\x1b[" + color + "m" + text + "\x1b[0m"
}
