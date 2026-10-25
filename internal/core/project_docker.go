package core

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"vsm/internal/config"
	"vsm/internal/plugin"
)

func (p Project) DockerArgs(dir, image, command string, args []string) ([]string, error) {
	return p.dockerArgsEnvironment(dir, image, command, args, nil)
}
func (p Project) dockerArgsEnvironment(dir, image, command string, args []string, env map[string]string) ([]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := validateComponent(command); err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(p.Root, dir)
	if err != nil || !pathWithin(p.Root, dir) {
		return nil, fmt.Errorf("Docker working directory must be in the project")
	}
	var mount strings.Builder
	writer := csv.NewWriter(&mount)
	if err := writer.Write([]string{"type=bind", "src=" + p.Root, "dst=/workspace"}); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	uid, gid := os.Getuid(), os.Getgid()
	if uid < 0 {
		uid = 1000
	}
	if gid < 0 {
		gid = 1000
	}
	flags := []string{"run", "--rm", "--init", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=256", "--read-only", "--tmpfs", "/tmp:rw,nosuid,nodev", "--user", strconv.Itoa(uid) + ":" + strconv.Itoa(gid), "--mount", strings.TrimSpace(mount.String()), "--workdir", "/workspace/" + filepath.ToSlash(rel)}
	if request := os.Getenv("VSM_MCP_REQUEST_ID"); request != "" {
		flags = append(flags, "--label", "io.github.hquip.vsm.mcp="+request)
	}
	for _, entry := range []string{"GOTOOLCHAIN=local", "PYTHONNOUSERSITE=1", "HOME=/workspace/.vsm/docker-cache/home", "GOPATH=/workspace/.vsm/docker-cache/go", "GOCACHE=/workspace/.vsm/docker-cache/go-build", "GOMODCACHE=/workspace/.vsm/docker-cache/go-mod", "npm_config_cache=/workspace/.vsm/docker-cache/npm", "PIP_CACHE_DIR=/workspace/.vsm/docker-cache/pip"} {
		flags = append(flags, "--env", entry)
	}
	if registry := plugin.NPMRegistry(); registry != "" {
		flags = append(flags, "--env", "npm_config_registry="+registry)
	}
	for k, v := range env {
		if !config.ValidUserEnv(k, v) {
			return nil, fmt.Errorf("unsafe Docker project variable %s", k)
		}
		flags = append(flags, "--env", k+"="+v)
	}
	flags = append(flags, image, command)
	return append(flags, args...), nil
}

func (p Project) RunDocker(ctx context.Context, dir, command string, args []string) error {
	return p.runDockerEnvironment(ctx, dir, command, args, nil, os.Stdin, os.Stdout, os.Stderr)
}
func (p Project) runDockerEnvironment(ctx context.Context, dir, command string, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := p.ValidateStateRoot(); err != nil {
		return err
	}
	for _, tool := range plugin.Names() {
		if plugin.VersionMode(tool) == "probe" {
			continue
		}
		spec, _, err := ProjectRequirement(tool, dir)
		if err != nil {
			return err
		}
		if spec != "" {
			return fmt.Errorf("%s 使用本机元数据或用户标注版本，不能通过 Docker 的自动版本检查；请使用 versions 模式纳管完整本机安装", tool)
		}
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("Docker 模式需要安装并启动 Docker Desktop / Docker Engine")
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := validateComponent(command); err != nil {
		return err
	}
	for _, name := range []string{"home", "go", "go-build", "go-mod", "npm", "pip"} {
		if err := os.MkdirAll(filepath.Join(p.Root, ".vsm", "docker-cache", name), 0o755); err != nil {
			return err
		}
	}
	inspect := func() (string, error) {
		c := exec.CommandContext(ctx, docker, "image", "inspect", "--format", "{{.Id}}", p.Image)
		hideConsole(c)
		out, err := c.Output()
		return strings.TrimSpace(string(out)), err
	}
	image, err := inspect()
	if err != nil {
		pullCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		pull := exec.CommandContext(pullCtx, docker, "pull", p.Image)
		hideConsole(pull)
		pull.Stdout = os.Stderr
		pull.Stderr = os.Stderr
		err = pull.Run()
		cancel()
		if err != nil {
			return fmt.Errorf("Docker 镜像不可用（确认 Docker 已启动）: %w", err)
		}
		image, err = inspect()
		if err != nil {
			return err
		}
	}
	if !strings.HasPrefix(image, "sha256:") {
		return fmt.Errorf("Docker did not return a content-addressed image ID")
	}
	// Validate the image toolchain before running project code. Never silently use
	// an image's incompatible language version or auto-install host executables.
	for _, tool := range plugin.Names() {
		spec, _, err := ProjectRequirement(tool, dir)
		if err != nil {
			return err
		}
		if spec == "" {
			continue
		}
		found := false
		for _, name := range plugin.DetectCommands()[tool] {
			verArgs := []string{"--version"}
			if tool == "go" || tool == "zig" {
				verArgs = []string{"version"}
			}
			if tool == "java" {
				verArgs = []string{"-version"}
			}
			flags, err := p.DockerArgs(dir, image, name, verArgs)
			if err != nil {
				return err
			}
			probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			c := exec.CommandContext(probeCtx, docker, flags...)
			hideConsole(c)
			out, runErr := c.CombinedOutput()
			cancel()
			if runErr != nil {
				continue
			}
			ver := versionRe.FindString(string(out))
			if ver == "" {
				continue
			}
			if !versionMatches(spec, ver) {
				return fmt.Errorf("Docker 镜像中的 %s %s 不满足项目要求 %s，请选择匹配镜像", tool, ver, spec)
			}
			found = true
			if command == tool {
				command = name
			}
			break
		}
		if !found {
			return fmt.Errorf("Docker 镜像缺少项目要求的 %s %s，请在自定义镜像中安装", tool, spec)
		}
	}
	flags, err := p.dockerArgsEnvironment(dir, image, command, args, env)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, docker, flags...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return exitCodeError{ee.ExitCode(), err}
		}
		return err
	}
	return nil
}
