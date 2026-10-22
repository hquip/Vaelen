package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

// CommandMatches recognizes the selected binary or an exact dispatcher emitted
// for this tool, home and CLI. It never executes a program during context queries.
func (a Activator) CommandMatches(tool, actual, expected string) bool {
	if !executableFile(actual) || !executableFile(expected) {
		return false
	}
	if sameCommandFile(actual, expected) {
		return true
	}
	if a.VsmExe == "" {
		return false
	}
	shims, err := canonicalPath(a.Paths.Shims())
	if err != nil {
		return false
	}
	parent, err := canonicalPath(filepath.Dir(actual))
	if err != nil || !sameDir(shims, parent) {
		return false
	}
	info, err := os.Lstat(actual)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	commands := append([]string{tool}, plugin.DetectCommands()[tool]...)
	for _, command := range commands {
		alias := command
		if command == tool {
			alias = ""
		}
		name, script := a.shimScript(tool, alias)
		sameName := filepath.Base(actual) == name
		if runtime.GOOS == "windows" {
			sameName = strings.EqualFold(filepath.Base(actual), name)
		}
		if !sameName {
			continue
		}
		data, err := config.ReadBounded(actual, 64<<10)
		return err == nil && string(data) == script
	}
	return false
}
