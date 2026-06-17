package core

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"vsm/internal/plugin"
)

// EnvFor 计算在 startDir 下所有“已解析且已安装”的工具应注入的 PATH 目录与环境变量。
// 供 shell hook 进入目录时即时切换：把这些 bin 目录前置到 PATH，并设置 GOROOT 等。
func (a Activator) EnvFor(startDir string) (pathDirs []string, env map[string]string) {
	env = map[string]string{}
	for _, tool := range plugin.Names() {
		p, ok := plugin.Get(tool)
		if !ok {
			continue
		}
		res := a.Resolver.Resolve(tool, startDir)
		if res.Version == "" {
			continue
		}
		installDir := a.Paths.InstallDir(tool, res.Version)
		pathDirs = append(pathDirs, p.BinPaths(installDir)...)
		for k, v := range p.ExecEnv(installDir) {
			env[k] = v
		}
	}
	return pathDirs, env
}

// EnvScript 把 pathDirs/env 渲染成某 shell 可 eval 的语句。PATH 基于 VSM_PATH_BASE 重建，
// 因此可在每次进目录/提示符时重复执行而不会让 PATH 累积膨胀。
func EnvScript(shell string, pathDirs []string, env map[string]string) (string, error) {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	switch strings.ToLower(shell) {
	case "bash", "zsh", "sh":
		if len(pathDirs) > 0 {
			b.WriteString(fmt.Sprintf("export PATH=%q\n", strings.Join(pathDirs, ":")+":${VSM_PATH_BASE:-$PATH}"))
		}
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("export %s=%q\n", k, env[k]))
		}
	case "fish":
		if len(pathDirs) > 0 {
			quoted := make([]string, len(pathDirs))
			for i, d := range pathDirs {
				quoted[i] = fmt.Sprintf("%q", d)
			}
			b.WriteString(fmt.Sprintf("set -gx PATH %s $VSM_PATH_BASE\n", strings.Join(quoted, " ")))
		}
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("set -gx %s %q\n", k, env[k]))
		}
	case "powershell", "pwsh":
		if len(pathDirs) > 0 {
			b.WriteString(fmt.Sprintf("$env:PATH = \"%s;$env:VSM_PATH_BASE\"\n", strings.Join(pathDirs, ";")))
		}
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("$env:%s = \"%s\"\n", k, env[k]))
		}
	case "cmd":
		if len(pathDirs) > 0 {
			b.WriteString(fmt.Sprintf("set PATH=%s;%%PATH%%\n", strings.Join(pathDirs, ";")))
		}
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("set %s=%s\n", k, env[k]))
		}
	default:
		return "", fmt.Errorf("不支持的 shell: %s（支持 bash/zsh/fish/powershell/cmd）", shell)
	}
	return b.String(), nil
}

// HookScript 返回某 shell 的激活脚本：把 shims 加入 PATH（兜底），并安装“进目录自动切换”
// 钩子——在 cd / 提示符时 eval `vsm env`，按当前目录 .tool-versions 即时切换版本与环境。
func HookScript(shell, shimsDir string) (string, error) {
	switch strings.ToLower(shell) {
	case "bash":
		return fmt.Sprintf(`export PATH="%s:$PATH"
export VSM_PATH_BASE="${VSM_PATH_BASE:-$PATH}"
_vsm_hook() { eval "$(vsm env bash)"; }
case "${PROMPT_COMMAND:-}" in *_vsm_hook*) ;; *) PROMPT_COMMAND="_vsm_hook;${PROMPT_COMMAND:-}" ;; esac
_vsm_hook`, shimsDir), nil
	case "zsh":
		return fmt.Sprintf(`export PATH="%s:$PATH"
export VSM_PATH_BASE="${VSM_PATH_BASE:-$PATH}"
_vsm_hook() { eval "$(vsm env zsh)"; }
autoload -U add-zsh-hook 2>/dev/null && add-zsh-hook chpwd _vsm_hook
_vsm_hook`, shimsDir), nil
	case "fish":
		return fmt.Sprintf(`set -gx PATH "%s" $PATH
if not set -q VSM_PATH_BASE
    set -gx VSM_PATH_BASE $PATH
end
function _vsm_hook --on-variable PWD
    vsm env fish | source
end
_vsm_hook`, shimsDir), nil
	case "powershell", "pwsh":
		return fmt.Sprintf(`$env:PATH = "%s;$env:PATH"
if (-not $env:VSM_PATH_BASE) { $env:VSM_PATH_BASE = $env:PATH }
function global:prompt {
    vsm env powershell | Out-String | Invoke-Expression
    "PS $($executionContext.SessionState.Path.CurrentLocation)> "
}`, shimsDir), nil
	case "cmd":
		return fmt.Sprintf(`set PATH=%s;%%PATH%%`, shimsDir), nil
	default:
		return "", fmt.Errorf("不支持的 shell: %s（支持 bash/zsh/fish/powershell/cmd）", shell)
	}
}

// PersistShimsPath 在 Windows 上把 shims 目录持久化写入“用户级 PATH”并广播变更：
// 通过 .NET 的 SetEnvironmentVariable(User) 写注册表，会自动发出 WM_SETTINGCHANGE，
// 新开终端即时生效，无需手动改 PATH。返回 "added" / "exists"。
func PersistShimsPath(shimsDir string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("path-init 仅 Windows 需要；其它平台请用 `vsm activate <shell>` 写入 shell 配置")
	}
	// 前置写入：把 shims 放到用户 PATH 最前面，才能覆盖系统里已有的同名命令
	// （如已存在的 java/node）。去重后再前置，避免重复累积。
	ps := fmt.Sprintf(`$d=%q
$p=[Environment]::GetEnvironmentVariable('Path','User'); if (-not $p) { $p='' }
$parts=@($p -split ';' | Where-Object { $_ -ne '' })
if ($parts.Count -gt 0 -and $parts[0] -eq $d) { 'exists' }
else {
  $parts=$parts | Where-Object { $_ -ne $d }
  [Environment]::SetEnvironmentVariable('Path', ((@($d)+$parts) -join ';'), 'User')
  'added'
}`, shimsDir)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("写入用户 PATH 失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
