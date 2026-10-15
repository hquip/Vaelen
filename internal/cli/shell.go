package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isWindows 报告当前平台是否为 Windows。
func isWindows() bool { return os.PathSeparator == '\\' }

// normalizeShellName 校验 shell 名（与 core.SupportedShells 保持一致）。
func normalizeShellName(sh string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(sh))
	switch s {
	case "bash", "zsh", "sh", "fish", "powershell", "pwsh", "cmd":
		return s, nil
	}
	return "", fmt.Errorf("不支持的 shell: %s（支持 bash/zsh/sh/fish/powershell/pwsh/cmd）", sh)
}

// setEnv 在 KEY=VALUE 列表里覆盖/追加一个变量（Windows 上按大小写不敏感匹配）。
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if !isWindows() {
			if strings.HasPrefix(e, prefix) {
				env[i] = prefix + value
				return env
			}
			continue
		}
		if len(e) >= len(prefix) && strings.EqualFold(e[:len(prefix)], prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// runShell 用给定环境启动一个交互式 shell，把控制权交给用户。
// 返回的退出码会一路透传到 vsm 的进程退出码。
func runShell(sh string, env []string) error {
	var cmd *exec.Cmd
	switch sh {
	case "cmd":
		cmd = exec.Command("cmd")
	case "powershell", "pwsh":
		exe := sh
		if _, err := exec.LookPath(exe); err != nil {
			exe = "powershell"
		}
		cmd = exec.Command(exe, "-NoLogo")
	case "fish":
		cmd = exec.Command("fish", "-i")
		if _, err := exec.LookPath("fish"); err != nil {
			return fmt.Errorf("未找到 fish")
		}
	default:
		cmd = exec.Command(sh, "-i")
		if _, err := exec.LookPath(sh); err != nil {
			return fmt.Errorf("未找到 %s", sh)
		}
	}
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}
