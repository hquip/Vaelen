package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"vsm/internal/config"
)

const cmdRegistryKey = `Software\Microsoft\Command Processor`

// CmdActivationCommand runs in the current cmd, rather than a child that cannot
// update its parent's PATH. The startup file itself contains only ASCII; %~dp0
// expands Unicode paths in cmd without depending on the console code page.
func CmdActivationCommand(root string) (string, error) {
	file := filepath.Join(root, "vsm-cmd-init.cmd")
	if strings.ContainsAny(file, "%!\"\x00\r\n") {
		return "", fmt.Errorf("cmd 自动激活的数据路径不能包含 %%、! 或引号，请使用不含这些字符的 VSM_HOME")
	}
	return `call "` + file + `"`, nil
}

func cmdStartupScript(root string) string {
	// Keep the caller's delayed-expansion mode. The ON branch must expand PATH
	// late using !PATH!: percent expansion there would corrupt literal ! in
	// existing entries. Helper paths containing ! are rejected before writing.
	prefix := filepath.Join(root, "shims") + ";"
	length := len(utf16.Encode([]rune(prefix)))
	return fmt.Sprintf("@rem VSM cmd activation; delete the VSM AutoRun entry to disable.\r\n"+
		"@if not defined VSM_HOME set \"VSM_HOME=%%~dp0\"\r\n"+
		"@if not defined PATHEXT set \"PATHEXT=.COM;.EXE;.BAT;.CMD\"\r\n"+
		"@if \"!!\"==\"\" goto vsm_delayed\r\n"+
		"@setlocal EnableDelayedExpansion\r\n"+
		"@set \"_vsm_exts=;!PATHEXT!;\"\r\n"+
		"@if /I \"!_vsm_exts:;.CMD;=;!\"==\"!_vsm_exts!\" (for /f \"delims=\" %%%%E in (\"!PATHEXT!;.CMD\") do (endlocal & set \"PATHEXT=%%%%E\")) else (endlocal)\r\n"+
		"@setlocal EnableDelayedExpansion\r\n"+
		"@set \"_vsm_expected=%%~dp0shims;\"\r\n"+
		"@if /I \"!PATH:~0,%d!\"==\"!_vsm_expected!\" (endlocal & exit /b 0)\r\n"+
		"@set \"_vsm_nextPath=%%~dp0shims;!PATH!\"\r\n"+
		"@for /f \"delims=\" %%%%P in (\"!_vsm_nextPath!\") do (endlocal & set \"PATH=%%%%P\")\r\n"+
		"@exit /b 0\r\n"+
		":vsm_delayed\r\n"+
		"@set \"_VSM_CMD_EXTS=;!PATHEXT!;\"\r\n"+
		"@if /I \"!_VSM_CMD_EXTS:;.CMD;=;!\"==\"!_VSM_CMD_EXTS!\" set \"PATHEXT=!PATHEXT!;.CMD\"\r\n"+
		"@set \"_VSM_CMD_EXTS=\"\r\n"+
		"@set \"_VSM_CMD_EXPECTED=%%~dp0shims;\"\r\n"+
		"@if /I \"!PATH:~0,%d!\"==\"!_VSM_CMD_EXPECTED!\" goto vsm_delayed_done\r\n"+
		"@set \"PATH=%%~dp0shims;!PATH!\"\r\n"+
		":vsm_delayed_done\r\n"+
		"@set \"_VSM_CMD_EXPECTED=\"\r\n"+
		"@exit /b 0\r\n", length, length)
}

// PersistCmdHook configures this user's cmd AutoRun, after preserving its prior
// command. It does not touch machine PATH, existing JDKs or execution policy.
func PersistCmdHook(root string) (string, error) {
	return persistCmdHookAt(root, cmdRegistryKey)
}

func persistCmdHookAt(root, registryKey string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("cmd 自动激活仅用于 Windows")
	}
	command, err := CmdActivationCommand(root)
	if err != nil {
		return "", err
	}
	// CALL returns to cmd's normal startup flow after our batch file. Its only
	// argument is a quoted path, already checked for percent expansion above.
	path := filepath.Join(root, "vsm-cmd-init.cmd")
	if err := config.WriteFileAtomic(path, []byte(cmdStartupScript(root)), 0o644); err != nil {
		return "", err
	}
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)
$key=[Microsoft.Win32.Registry]::CurrentUser.CreateSubKey(%s)
try {
  $options=[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
  $current=$key.GetValue('AutoRun',$null,$options)
  $managed=$key.GetValue('VSM.AutoRun.Managed',$null,$options)
  if ($null -ne $current -and $current -isnot [string]) { throw 'cmd AutoRun is not a string; left unchanged' }
  if ($null -ne $managed -and $current -cne $managed) { throw 'cmd AutoRun changed outside VSM; left unchanged. Remove VSM.AutoRun.Managed after reviewing the existing AutoRun to configure again.' }
  if ($null -ne $managed) {
    $original=$key.GetValue('VSM.AutoRun.Previous','',$options)
    $kind=[Microsoft.Win32.RegistryValueKind]$key.GetValue('VSM.AutoRun.Kind','String')
  } else {
    $original=if($null -eq $current){''}else{$current}
    $kind=if($null -eq $current){[Microsoft.Win32.RegistryValueKind]::String}else{$key.GetValueKind('AutoRun')}
  }
  if ($kind -ne [Microsoft.Win32.RegistryValueKind]::String -and $kind -ne [Microsoft.Win32.RegistryValueKind]::ExpandString) { throw 'Unsupported cmd AutoRun type; left unchanged' }
  $command=%s
  $next=if([string]::IsNullOrWhiteSpace($original)){$command}else{$original+' & '+$command}
  $status=if($current -ceq $next){'exists'}else{'added'}
  if($status -ne 'exists') {
    $key.SetValue('VSM.AutoRun.Previous',$original,[Microsoft.Win32.RegistryValueKind]::String)
    $key.SetValue('VSM.AutoRun.Kind',$kind.ToString(),[Microsoft.Win32.RegistryValueKind]::String)
    $key.SetValue('AutoRun',$next,$kind)
    $key.SetValue('VSM.AutoRun.Managed',$next,[Microsoft.Win32.RegistryValueKind]::String)
  }
  $status | ConvertTo-Json -Compress
} finally { $key.Dispose() }
`, quotePowerShell(registryKey), quotePowerShell(command))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideConsole(cmd)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("配置 cmd 自动激活失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	var status string
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("读取 cmd 激活结果失败: %w", err)
	}
	return status, nil
}

func windowsCmdPath() string { return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe") }
