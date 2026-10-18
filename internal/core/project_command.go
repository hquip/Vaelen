package core

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func projectCommand(program string, args []string) (*exec.Cmd, error) {
	return projectCommandContext(context.Background(), program, args)
}
func projectCommandContext(ctx context.Context, program string, args []string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(program), ".cmd") || strings.EqualFold(filepath.Ext(program), ".bat")) {
		name := strings.ToLower(strings.TrimSuffix(filepath.Base(program), filepath.Ext(program)))
		// Node's portable npm/npx wrappers can run without cmd.exe parsing args.
		if name == "npm" || name == "npx" {
			node := filepath.Join(filepath.Dir(program), "node.exe")
			js := filepath.Join(filepath.Dir(program), "node_modules", "npm", "bin", name+"-cli.js")
			if fileExists(node) && fileExists(js) {
				return exec.CommandContext(ctx, node, append([]string{js}, args...)...), nil
			}
		}
		return nil, fmt.Errorf("batch command %q requires an explicit shell; run cmd.exe or PowerShell explicitly", program)
	}
	return exec.CommandContext(ctx, program, args...), nil
}
