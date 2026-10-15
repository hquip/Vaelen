package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"vsm/internal/plugin"
)

func (p Project) PythonVenv(version string) string {
	return filepath.Join(p.Root, ".vsm", "env", "venvs", "python-"+version)
}

func (a Activator) PrepareProject(ctx context.Context, dir string) error {
	p, err := FindProject(dir)
	if err != nil || p == nil {
		return err
	}
	if p.Mode != "local" {
		return nil
	}
	if err := p.PrepareVariables(); err != nil {
		return err
	}
	spec, _, err := ProjectRequirement("python", dir)
	if err != nil || spec == "" {
		return err
	}
	res := a.Resolver.match("python", spec, "项目环境")
	if res.Error != "" {
		return fmt.Errorf("%s", res.Error)
	}
	if res.Version == "" {
		return fmt.Errorf("项目 Python 尚未安装，请先执行 vsm run --install-missing -- python --version")
	}
	venv := p.PythonVenv(res.Version)
	projectRoot, err := os.OpenRoot(p.Root)
	if err != nil {
		return err
	}
	defer projectRoot.Close()
	venvRel, err := filepath.Rel(p.Root, venv)
	if err != nil {
		return err
	}
	if data, err := os.ReadFile(filepath.Join(venv, ".vsm-python-version")); err == nil && strings.TrimSpace(string(data)) == res.Version && fileExists(filepath.Join(venv, "pyvenv.cfg")) {
		return nil
	}
	if _, err := os.Lstat(venv); err == nil {
		return fmt.Errorf("Python 环境不完整或不受 vsm 管理: %s", venv)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := projectRoot.MkdirAll(filepath.Dir(venvRel), 0o755); err != nil {
		return err
	}
	if err := projectRoot.Mkdir(venvRel, 0o755); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(venv)
		}
	}()
	python, _ := plugin.Get("python")
	exe := ""
	for _, name := range plugin.DetectCommands()["python"] {
		exe = findExecutable(python.BinPaths(a.Paths.InstallDir("python", res.Version)), name)
		if exe != "" {
			break
		}
	}
	if exe == "" {
		return fmt.Errorf("项目 Python 安装缺少主命令")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-m", "venv", venv)
	hideConsole(cmd)
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PYTHONHOME") || strings.EqualFold(key, "VIRTUAL_ENV") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建项目 Python venv 失败: %w: %.2000s", err, out)
	}
	if err := os.WriteFile(filepath.Join(venv, ".vsm-python-version"), []byte(res.Version+"\n"), 0o644); err != nil {
		return err
	}
	committed = true
	return nil
}
