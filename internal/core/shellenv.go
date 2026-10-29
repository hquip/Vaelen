package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"vsm/internal/logging"
	"vsm/internal/plugin"
)

// SupportedShells 是 EnvScript / HookScript 支持的 shell 列表。
var SupportedShells = []string{"bash", "zsh", "sh", "fish", "powershell", "pwsh", "cmd"}

// normalizeShell 归一化并校验 shell 名。
//
// 早期实现到 switch 的 default 分支才报错，而该分支在「当前目录没有任何工具生效」
// 时不可达（脚本提前 return），于是 `vsm env nosuchshell` 在空目录里静默成功、
// 在项目目录里报错 —— 行为随 cwd 变化。现在统一在入口校验。
func normalizeShell(shell string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(shell))
	if s == "" {
		s = "bash"
	}
	for _, ok := range SupportedShells {
		if s == ok {
			return s, nil
		}
	}
	return "", fmt.Errorf("不支持的 shell: %s（支持 %s）", shell, strings.Join(SupportedShells, "/"))
}

// envKeysVar 记录上一轮由 vsm 注入的环境变量名，便于在离开项目时 unset 掉，
// 避免 GOROOT / JAVA_HOME 之类的值泄漏到后续目录。
const envKeysVar = "_VSM_ENV_KEYS"

// quotePOSIX 生成 POSIX shell 单引号字面量：单引号内不做任何展开，
// 内嵌的单引号写成「结束引号 + 转义引号 + 重新开始引号」。
//
// 注意：结果只能**直接**用作单词或与其它单词拼接，不能再包进双引号里 ——
// 双引号内的单引号是字面字符，PATH 里会出现带引号的不存在目录。
func quotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteFish 生成 fish 单引号字面量。与 POSIX 不同，fish 在单引号内会处理
// 反斜杠转义（\\ 与 \'），因此两者都要转义。
func quoteFish(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// quotePowerShell 生成 PowerShell 单引号字面量（内嵌单引号写成两个单引号）。
// 单引号字符串不做变量展开，$ 与反引号按字面处理 —— 双引号写法下，
// 路径里的 $work 会被当成变量、反引号会被当成转义符。
func quotePowerShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// quoteCmd 转义 cmd.exe 批处理里会被展开的 %（写成 %%）。
// 配合 set "VAR=value" 形式使用，可同时避免行尾空格与 & 的歧义。
func quoteCmd(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

// shellPath 把 Windows 原生路径转成 Git Bash / MSYS2 认识的形式（C:\a\b → /c/a/b）。
// POSIX shell 的 PATH 以冒号分隔，原样放入 "C:\..." 会在盘符的冒号处被截断。
// 非 Windows 平台原样返回。
func shellPath(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	if len(p) >= 2 && p[1] == ':' {
		return "/" + strings.ToLower(p[:1]) + strings.ReplaceAll(p[2:], `\`, "/")
	}
	return strings.ReplaceAll(p, `\`, "/")
}

// posixCmd 返回 POSIX 系 shell 里调用 vsm 的写法（裸命令或带引号的绝对路径）。
func posixCmd(vsmCmd string) string {
	if vsmCmd == "" || vsmCmd == "vsm" {
		return "vsm"
	}
	return quotePOSIX(shellPath(vsmCmd))
}

// VsmCommand 返回 shell 脚本里调用 vsm 时应使用的命令：
// 若 PATH 中的 vsm 就是当前这份可执行文件，用裸命令 "vsm"（升级/移动后依然有效，
// 例如 Homebrew 的版本化目录）；否则用绝对路径，保证 vsm 不在 PATH 时钩子也能工作
// （典型场景：macOS 从 Finder 启动的 GUI 写入 ~/.zshrc）。
func VsmCommand(vsmExe string) string {
	if vsmExe == "" {
		return "vsm"
	}
	if p, err := exec.LookPath("vsm"); err == nil {
		a, errA := os.Stat(p)
		b, errB := os.Stat(vsmExe)
		if errA == nil && errB == nil && os.SameFile(a, b) {
			return "vsm"
		}
	}
	return vsmExe
}

// EnvFor 计算在 startDir 下所有「已解析且已安装」的工具应注入的 PATH 目录与环境变量。
// 供 shell hook 进入目录时即时切换：把这些 bin 目录前置到 PATH，并设置 GOROOT 等。
func (a Activator) EnvFor(startDir string) (pathDirs []string, env map[string]string) {
	plan, _ := a.PlanEnvironment(startDir)
	return plan.Paths, plan.Variables
}

// EnvScript 把 pathDirs/env 渲染成某 shell 可 eval 的语句。
//
// PATH 一律基于 VSM_PATH_BASE 重建，因此每次进目录/提示符重复执行都不会让 PATH 膨胀。
// **即使 pathDirs 为空也要输出 PATH 那一行**：那正是用户 cd 出项目的时刻，
// 不重建的话上一个项目的版本会泄漏到整个终端会话。
// 上一轮注入过、本轮不再需要的环境变量（GOROOT/JAVA_HOME 等）会被清除。
func EnvScript(shell string, pathDirs []string, env map[string]string) (string, error) {
	sh, err := normalizeShell(shell)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		if !validEnvKey(k) || strings.ContainsAny(env[k], "\x00\r\n") {
			return "", fmt.Errorf("unsafe environment variable: %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	switch sh {
	case "bash", "zsh", "sh":
		b.WriteString("if [ \"${VSM_PATH_BASE+x}\" != x ]; then export VSM_PATH_BASE=\"$PATH\"; fi\n")
		// 形如 export PATH='/a':'/b':"${VSM_PATH_BASE:-$PATH}"——
		// 单引号段与双引号段相邻即拼接，引号由 shell 剥掉，不会残留到 PATH 里。
		var prefix strings.Builder
		for _, d := range pathDirs {
			prefix.WriteString(quotePOSIX(shellPath(d)))
			prefix.WriteByte(':')
		}
		b.WriteString("export PATH=" + prefix.String() + `"${VSM_PATH_BASE}"` + "\n")
		// zsh 默认不对未加引号的参数做分词，需用 ${=VAR} 强制分词
		b.WriteString(restoreManagedVars(sh))
		for _, k := range keys {
			b.WriteString(backupManagedVar(sh, k))
			b.WriteString("export " + k + "=" + quotePOSIX(env[k]) + "\n")
		}
		b.WriteString("export " + envKeysVar + "=" + quotePOSIX(strings.Join(keys, " ")) + "\n")
	case "fish":
		b.WriteString(fishPathBase())
		var parts []string
		for _, d := range pathDirs {
			parts = append(parts, quoteFish(shellPath(d)))
		}
		parts = append(parts, "$VSM_PATH_BASE")
		b.WriteString("set -gx --path PATH " + strings.Join(parts, " ") + "\n")
		b.WriteString(restoreManagedVars(sh))
		for _, k := range keys {
			b.WriteString(backupManagedVar(sh, k))
			b.WriteString("set -gx " + k + " " + quoteFish(env[k]) + "\n")
		}
		b.WriteString("set -gx " + envKeysVar + " " + quoteFish(strings.Join(keys, " ")) + "\n")
	case "powershell", "pwsh":
		b.WriteString("if (-not $env:VSM_PATH_BASE) { $env:VSM_PATH_BASE = $env:PATH }\n")
		if len(pathDirs) == 0 {
			b.WriteString("$env:PATH = $env:VSM_PATH_BASE\n")
		} else {
			// 目录整体放进一个单引号字面量，再与 $env:VSM_PATH_BASE 拼接。
			// 分隔符随平台（macOS/Linux 上的 pwsh 是冒号）。
			sep := string(os.PathListSeparator)
			b.WriteString("$env:PATH = " + quotePowerShell(strings.Join(pathDirs, sep)+sep) +
				" + $env:VSM_PATH_BASE\n")
		}
		b.WriteString(restoreManagedVars(sh))
		for _, k := range keys {
			b.WriteString(backupManagedVar(sh, k))
			b.WriteString("$env:" + k + " = " + quotePowerShell(env[k]) + "\n")
		}
		b.WriteString("$env:" + envKeysVar + " = " + quotePowerShell(strings.Join(keys, " ")) + "\n")
	case "cmd":
		// 供批处理调用（vsm env cmd > x.bat && call x.bat）：% 需写成 %%
		b.WriteString("if not defined VSM_PATH_BASE set \"VSM_PATH_BASE=%PATH%\"\n")
		if len(pathDirs) == 0 {
			b.WriteString("set \"PATH=%VSM_PATH_BASE%\"\n")
		} else {
			b.WriteString("set \"PATH=" + quoteCmd(strings.Join(pathDirs, ";")) + ";%VSM_PATH_BASE%\"\n")
		}
		known := map[string]bool{"GOROOT": true, "JAVA_HOME": true}
		for _, k := range keys {
			known[k] = true
		}
		for _, k := range strings.Fields(os.Getenv(envKeysVar)) {
			if validEnvKey(k) {
				known[k] = true
			}
		}
		for _, tool := range plugin.Names() {
			p, ok := plugin.Get(tool)
			if !ok {
				continue
			}
			for k := range p.ExecEnv("") {
				if validEnvKey(k) {
					known[k] = true
				}
			}
		}
		var restoreKeys []string
		for k := range known {
			restoreKeys = append(restoreKeys, k)
		}
		sort.Strings(restoreKeys)
		for _, k := range restoreKeys {
			b.WriteString("if defined _VSM_HAD_" + k + " (\n  if \"%_VSM_HAD_" + k + "%\"==\"1\" (set \"" + k + "=%_VSM_ORIG_" + k + "%\") else (set \"" + k + "=\")\n  set \"_VSM_HAD_" + k + "=\"\n  set \"_VSM_ORIG_" + k + "=\"\n)\n")
		}
		for _, k := range keys {
			b.WriteString(backupManagedVar(sh, k))
			b.WriteString("set \"" + k + "=" + quoteCmd(env[k]) + "\"\n")
		}
		b.WriteString("set \"" + envKeysVar + "=" + strings.Join(keys, " ") + "\"\n")
	}
	if sh == "cmd" {
		return strings.ReplaceAll(b.String(), "\n", "\r\n"), nil
	}
	return b.String(), nil
}

// HookScript 返回某 shell 的激活脚本：把 shims 加入 PATH（兜底），并安装「进目录自动切换」
// 钩子——在 cd / 提示符时执行 `vsm env`，按当前目录 .tool-versions 即时切换版本与环境。
// vsmCmd 为钩子里调用 vsm 的命令（见 VsmCommand），为空时用裸命令 "vsm"。
func HookScript(shell, shimsDir, vsmCmd string) (string, error) {
	sh, err := normalizeShell(shell)
	if err != nil {
		return "", err
	}
	if vsmCmd == "" {
		vsmCmd = "vsm"
	}
	posixVsm := posixCmd(vsmCmd)
	shims := quotePOSIX(shellPath(shimsDir))
	posixInit := "case \":$PATH:\" in *:" + shims + ":*) ;; *) export PATH=" + shims + ":\"$PATH\" ;; esac\nif [ \"${VSM_PATH_BASE+x}\" != x ]; then export VSM_PATH_BASE=\"$PATH\"; fi\n"
	switch sh {
	case "bash":
		return posixInit + fmt.Sprintf(`
_vsm_hook() { eval "$(%s env bash)"; }
case "${PROMPT_COMMAND:-}" in *_vsm_hook*) ;; *) PROMPT_COMMAND="_vsm_hook;${PROMPT_COMMAND:-}" ;; esac
_vsm_hook`, posixVsm), nil
	case "zsh":
		return posixInit + fmt.Sprintf(`
_vsm_hook() { eval "$(%s env zsh)"; }
autoload -U add-zsh-hook 2>/dev/null && { add-zsh-hook chpwd _vsm_hook; add-zsh-hook precmd _vsm_hook; }
_vsm_hook`, posixVsm), nil
	case "sh":
		return posixInit + fmt.Sprintf(`
_vsm_hook() { eval "$(%s env sh)"; }
_vsm_hook`, posixVsm), nil
	case "fish":
		fishVsm := vsmCmd
		if vsmCmd != "vsm" {
			fishVsm = quoteFish(shellPath(vsmCmd))
		}
		fishShims := quoteFish(shellPath(shimsDir))
		return fishPathBase() + fmt.Sprintf(`if type -q fish_add_path
    fish_add_path --global --path --move %s
else
    set -l _vsm_other_paths
    for _vsm_path in $PATH
        if test "$_vsm_path" != %s
            set -a _vsm_other_paths "$_vsm_path"
        end
    end
    set -gx --path PATH %s $_vsm_other_paths
end
set -l _vsm_base_paths
for _vsm_path in $VSM_PATH_BASE
    if test "$_vsm_path" != %s
        set -a _vsm_base_paths "$_vsm_path"
    end
end
set -gx --path VSM_PATH_BASE %s $_vsm_base_paths
function _vsm_hook --on-variable PWD
    %s env fish | source
    set -g _VSM_FISH_REFRESHED_PWD "$PWD"
end
function _vsm_prompt --on-event fish_prompt
    if set -q _VSM_FISH_REFRESHED_PWD; and test "$_VSM_FISH_REFRESHED_PWD" = "$PWD"
        set -e _VSM_FISH_REFRESHED_PWD
        return
    end
    _vsm_hook
    set -e _VSM_FISH_REFRESHED_PWD
end
_vsm_hook`, fishShims, fishShims, fishShims, fishShims, fishShims, fishVsm), nil
	case "powershell", "pwsh":
		psVsm := vsmCmd
		if vsmCmd != "vsm" {
			psVsm = "& " + quotePowerShell(vsmCmd)
		}
		return fmt.Sprintf(`$env:PATH = (@(%s) + @($env:PATH -split [regex]::Escape([string][IO.Path]::PathSeparator) | Where-Object { $_ -and $_.TrimEnd('\','/') -ine %s })) -join [IO.Path]::PathSeparator
if (-not (Test-Path env:VSM_PATH_BASE)) { $env:VSM_PATH_BASE = $env:PATH }
function global:_vsm_refresh {
    $vsmExitCode = $global:LASTEXITCODE
    try { %s env powershell | Out-String | ForEach-Object { if ($_) { Invoke-Expression $_ } } }
    finally { $global:LASTEXITCODE = $vsmExitCode }
}
if (-not (Get-Variable _VSM_OriginalPrompt -Scope Global -ErrorAction SilentlyContinue)) {
    $vsmPreviousPrompt = if (Test-Path Function:prompt) { (Get-Item Function:prompt).ScriptBlock } else { $null }
    $vsmLegacyPrompt = $vsmPreviousPrompt -and $vsmPreviousPrompt.ToString().Contains('env powershell | Out-String | Invoke-Expression') -and $vsmPreviousPrompt.ToString().Contains('PS $($executionContext.SessionState.Path.CurrentLocation)> ')
    $global:_VSM_OriginalPrompt = if ($vsmPreviousPrompt -and -not $vsmLegacyPrompt) { $vsmPreviousPrompt } else { { "PS $($executionContext.SessionState.Path.CurrentLocation)> " } }
    Remove-Variable vsmPreviousPrompt,vsmLegacyPrompt -ErrorAction SilentlyContinue
}
function global:prompt { _vsm_refresh; & $global:_VSM_OriginalPrompt }
_vsm_refresh
`, quotePowerShell(shimsDir), quotePowerShell(strings.TrimRight(shimsDir, `\/`)), psVsm), nil
	case "cmd":
		return "set \"PATH=" + quoteCmd(shimsDir) + ";%PATH%\"", nil
	}
	return "", fmt.Errorf("不支持的 shell: %s", shell)
}

// PersistShimsPath 在 Windows 上把 shims 目录持久化写入「用户级 PATH」并广播变更：
// 保留注册表原有字符串类型，写入后广播 WM_SETTINGCHANGE，
// 用户 PATH 不会覆盖排在前面的系统 PATH；Windows PowerShell 还需启动钩子。
// 返回 "added" / "exists"。
func PersistShimsPath(shimsDir string, operationIDs ...string) (status string, err error) {
	return persistShimsPathAt(shimsDir, "Environment", operationIDs...)
}

// A separate registry location lets subprocess tests exercise persistence
// without changing the user's actual PATH or broadcasting fixture paths.
func persistShimsPathAt(shimsDir, registryKey string, operationIDs ...string) (status string, err error) {
	operationID := logging.NewOperationID()
	if len(operationIDs) > 0 && operationIDs[0] != "" {
		operationID = operationIDs[0]
	}
	finish := logging.Start("activation", "user-path.persist", logging.Fields{"operation_id": operationID, "shims_dir": shimsDir, "target": "user-path", "process_path_changes": false})
	defer func() { finish(err) }()
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("PersistShimsPath 仅用于 Windows；其它平台见 PersistShellHook")
	}
	// 前置到用户 PATH；系统 PATH 仍可能排在它前面。去重避免重复累积。
	// 目录用单引号字面量，避免路径里的 $ 被 PowerShell 展开。
	ps := fmt.Sprintf(`$ErrorActionPreference='Stop'
$d=%s
$expected=[IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($d.Trim('"'))).TrimEnd('\','/')
$key=[Microsoft.Win32.Registry]::CurrentUser.CreateSubKey(%s)
try {
$options=[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
$p=$key.GetValue('Path','',$options)
if ($p -isnot [string]) { throw 'User PATH is not a string; left unchanged' }
$kind=if($null -eq $key.GetValue('Path',$null,$options)){[Microsoft.Win32.RegistryValueKind]::ExpandString}else{$key.GetValueKind('Path')}
$parts=@($p -split ';' | Where-Object { $_ -ne '' })
$parts=@($parts | Where-Object {
  try { [IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($_.Trim('"'))).TrimEnd('\','/') -ine $expected }
  catch { $true }
})
$next=(@($d)+$parts) -join ';'
if ($next -ceq $p) { 'exists' }
else {
  $key.SetValue('Path',$next,$kind)
  if ($key.GetValue('Path',$null,$options) -cne $next) { throw 'User PATH read-back mismatch' }
  if (%s -eq 'Environment') {
    Add-Type -Namespace Vsm -Name EnvironmentBroadcast -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("user32.dll", CharSet=System.Runtime.InteropServices.CharSet.Unicode, SetLastError=true)]
public static extern System.IntPtr SendMessageTimeout(System.IntPtr window, uint message, System.UIntPtr parameter, string name, uint flags, uint timeout, out System.UIntPtr result);
'@
    $broadcastResult=[UIntPtr]::Zero
    [void][Vsm.EnvironmentBroadcast]::SendMessageTimeout([IntPtr]0xffff,0x1a,[UIntPtr]::Zero,'Environment',2,2000,[ref]$broadcastResult)
  }
  'added'
}
} finally { $key.Dispose() }`, quotePowerShell(shimsDir), quotePowerShell(registryKey), quotePowerShell(registryKey))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	c.WaitDelay = time.Second
	hideConsole(c)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("写入用户 PATH 失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	status = strings.TrimSpace(string(out))
	logging.Record("info", "activation", "user-path.result", logging.Fields{"operation_id": operationID, "status": status, "shims_dir": shimsDir, "process_path_changes": false})
	return status, nil
}

// hookBlockBegin / hookBlockEnd 标记 vsm 写入 shell 配置文件的区块，保证幂等、可识别、可删除。
const (
	hookBlockBegin = "# >>> vsm >>>"
	hookBlockEnd   = "# <<< vsm <<<"
)

// DefaultShell 返回用户的登录 shell 名（bash/zsh/fish）。
// 从 Finder/Dock 启动的 macOS 应用不一定有 $SHELL，此时按系统默认推断
// （macOS Catalina 起默认 zsh，Linux 通常为 bash）。
func DefaultShell() string {
	if sh := filepath.Base(os.Getenv("SHELL")); sh != "" && sh != "." {
		switch sh {
		case "bash", "zsh", "fish":
			return sh
		}
	}
	if runtime.GOOS == "darwin" {
		return "zsh"
	}
	return "bash"
}

// ShellRCFile 返回某 shell 的交互式配置文件路径。
// macOS 的终端默认开「登录 shell」，bash 读 ~/.bash_profile 而不是 ~/.bashrc。
func ShellRCFile(shell, home string) (string, error) {
	switch shell {
	case "zsh":
		if zd := os.Getenv("ZDOTDIR"); zd != "" {
			return filepath.Join(zd, ".zshrc"), nil
		}
		return filepath.Join(home, ".zshrc"), nil
	case "bash":
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, ".bash_profile"), nil
		}
		return filepath.Join(home, ".bashrc"), nil
	case "fish":
		if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
			return filepath.Join(dir, "fish", "config.fish"), nil
		}
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	}
	return "", fmt.Errorf("不支持自动写入 %s 的配置文件（支持 bash/zsh/fish）", shell)
}

// PersistShellHook 在 macOS / Linux 上把激活语句写进用户的 shell 配置文件，
// 作用等同于 Windows 的 PersistShimsPath：之后新开的终端自动启用 shims 与
// 「进目录自动切换」。写入的是 `eval "$(vsm activate zsh)"` 这类**动态**语句，
// 因此 vsm 升级后无需重新写入。重复调用是幂等的。
// 返回配置文件路径与 "added" / "exists"。
func PersistShellHook(shell, vsmCmd string) (rcPath, status string, err error) {
	if runtime.GOOS == "windows" {
		return "", "", fmt.Errorf("Windows 请使用 PersistShimsPath")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	rcPath, err = ShellRCFile(shell, home)
	if err != nil {
		return "", "", err
	}
	if vsmCmd == "" {
		vsmCmd = "vsm"
	}
	var line string
	switch shell {
	case "fish":
		cmd := vsmCmd
		if vsmCmd != "vsm" {
			cmd = quoteFish(vsmCmd)
		}
		line = cmd + " activate fish | source"
	default:
		cmd := vsmCmd
		if vsmCmd != "vsm" {
			cmd = quotePOSIX(vsmCmd)
		}
		line = `eval "$(` + cmd + ` activate ` + shell + `)"`
	}

	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return rcPath, "", err
	}
	if strings.Contains(string(existing), hookBlockBegin) {
		return rcPath, "exists", nil
	}
	if err := os.MkdirAll(filepath.Dir(rcPath), 0o755); err != nil {
		return rcPath, "", err
	}
	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return rcPath, "", err
	}
	defer f.Close()
	var block strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		block.WriteByte('\n')
	}
	block.WriteString("\n" + hookBlockBegin + "\n")
	block.WriteString("# 由 vsm 自动添加：启用 shims 与进目录自动切换版本（删除整个区块即可撤销）\n")
	block.WriteString(line + "\n")
	block.WriteString(hookBlockEnd + "\n")
	if _, err := f.WriteString(block.String()); err != nil {
		return rcPath, "", err
	}
	return rcPath, "added", nil
}
