package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vsm/internal/plugin"
)

// An IDE/user-activated venv owns its Python and packages. Shell refreshes and
// shims must respect it even when VSM has selected a different base interpreter.
// VSM's own project/adopted environments are tracked separately so changing
// directories or deactivating a venv cannot resurrect an earlier environment.
const managedVirtualEnvVar = "_VSM_MANAGED_VIRTUAL_ENV"

func externalPythonVenv() (string, error) {
	root := os.Getenv("VIRTUAL_ENV")
	if root == "" || os.Getenv(managedVirtualEnvVar) != "" && sameDir(root, os.Getenv(managedVirtualEnvVar)) {
		return "", nil
	}
	if fileExists(filepath.Join(root, ".vsm-python-version")) {
		return "", nil
	}
	if !filepath.IsAbs(root) || strings.ContainsAny(root, "\x00\r\n") || !fileExists(filepath.Join(root, "pyvenv.cfg")) {
		return "", fmt.Errorf("当前激活的 Python venv 不完整：%s；请修复或退出该虚拟环境，已阻止回退到全局 Python", root)
	}
	p, ok := plugin.Get("python")
	if !ok || findExecutable(p.BinPaths(root), "python") == "" && findExecutable(p.BinPaths(root), "python3") == "" {
		return "", fmt.Errorf("当前激活的 Python venv 缺少解释器：%s；请修复或退出该虚拟环境，已阻止回退到全局 Python", root)
	}
	return root, nil
}

func pythonPipCommand(name string) bool {
	name = pythonCommandName(name)
	return name == "pip" || name == "pip3"
}

func pythonCommand(name string) bool {
	name = pythonCommandName(name)
	return name == "python" || name == "python3" || pythonPipCommand(name)
}

func pythonCommandName(name string) string {
	if os.PathSeparator == '\\' {
		name = strings.ToLower(name)
		switch ext := filepath.Ext(name); ext {
		case ".exe", ".cmd", ".bat", ".com":
			name = strings.TrimSuffix(name, ext)
		}
	}
	return name
}
