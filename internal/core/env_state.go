package core

import (
	"fmt"
	"strings"
)

func validEnvKey(k string) bool {
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		return false
	}
	for _, c := range k {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	upper := strings.ToUpper(k)
	return upper != "PATH" && upper != "VSM_PATH_BASE" && !strings.HasPrefix(upper, "_VSM_")
}

func restoreManagedVars(sh string) string {
	switch sh {
	case "bash", "sh", "zsh":
		list := "${_VSM_ENV_KEYS:-}"
		if sh == "zsh" {
			list = "${=_VSM_ENV_KEYS}"
		}
		return `if [ -n "${_VSM_ENV_KEYS:-}" ]; then
  for _vsm_k in ` + list + `; do
    case "$_vsm_k" in ''|[0-9]*|*[!a-zA-Z0-9_]*) continue ;; esac
    eval '_vsm_had=${_VSM_HAD_'"$_vsm_k"':-0}'
    eval '_vsm_old=${_VSM_ORIG_'"$_vsm_k"'-}'
    if [ "$_vsm_had" = 1 ]; then export "$_vsm_k=$_vsm_old"; else unset "$_vsm_k"; fi
    unset "_VSM_HAD_$_vsm_k" "_VSM_ORIG_$_vsm_k"
  done
  unset _vsm_k _vsm_had _vsm_old
fi
`
	case "fish":
		return `if set -q _VSM_ENV_KEYS
    for _vsm_k in (string split ' ' -- $_VSM_ENV_KEYS)
        if not string match -rq '^[A-Za-z_][A-Za-z0-9_]*$' -- $_vsm_k; continue; end
        set -l _vsm_had _VSM_HAD_$_vsm_k
        set -l _vsm_orig _VSM_ORIG_$_vsm_k
        if set -q $_vsm_had; and test "$$_vsm_had" = 1
            set -gx $_vsm_k "$$_vsm_orig"
        else
            set -e $_vsm_k
        end
        set -e $_vsm_had
        set -e $_vsm_orig
    end
end
`
	case "powershell", "pwsh":
		return `if ($env:_VSM_ENV_KEYS) {
  foreach ($_vsm_k in ($env:_VSM_ENV_KEYS -split ' ')) {
    if ($_vsm_k -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') { continue }
    $_vsm_had = [Environment]::GetEnvironmentVariable("_VSM_HAD_$_vsm_k", 'Process')
    $_vsm_old = if ($_vsm_had -eq '1') { [Environment]::GetEnvironmentVariable("_VSM_ORIG_$_vsm_k", 'Process') } else { $null }
    [Environment]::SetEnvironmentVariable($_vsm_k, $_vsm_old, 'Process')
    [Environment]::SetEnvironmentVariable("_VSM_HAD_$_vsm_k", $null, 'Process')
    [Environment]::SetEnvironmentVariable("_VSM_ORIG_$_vsm_k", $null, 'Process')
  }
  Remove-Variable _vsm_k,_vsm_had,_vsm_old -ErrorAction SilentlyContinue
}
`
	}
	return ""
}

func backupManagedVar(sh, k string) string {
	switch sh {
	case "bash", "sh", "zsh":
		return "if [ \"${" + k + "+x}\" = x ]; then export _VSM_HAD_" + k + "=1; export _VSM_ORIG_" + k + "=\"${" + k + "}\"; else export _VSM_HAD_" + k + "=0; unset _VSM_ORIG_" + k + "; fi\n"
	case "fish":
		return "if set -q " + k + "\n    set -gx _VSM_HAD_" + k + " 1\n    set -gx _VSM_ORIG_" + k + " \"$" + k + "\"\nelse\n    set -gx _VSM_HAD_" + k + " 0\nend\n"
	case "powershell", "pwsh":
		return "if (Test-Path env:" + k + ") { $env:_VSM_HAD_" + k + " = '1'; $env:_VSM_ORIG_" + k + " = $env:" + k + " } else { $env:_VSM_HAD_" + k + " = '0' }\n"
	case "cmd":
		return "if defined " + k + " (set \"_VSM_HAD_" + k + "=1\" & set \"_VSM_ORIG_" + k + "=%" + k + "%\") else (set \"_VSM_HAD_" + k + "=0\")\n"
	}
	return ""
}

// SwitchNoticeScript updates machine-readable shell state, with opt-in stderr output.
func SwitchNoticeScript(sh, label string) (string, error) {
	sh, err := normalizeShell(sh)
	if err != nil {
		return "", err
	}
	message := "[vsm] " + label
	if label == "" {
		message = "[vsm] 已还原基础环境"
	}
	switch sh {
	case "bash", "sh", "zsh":
		return "if [ \"${VSM_NOTIFY:-0}\" = 1 ] && [ \"${_VSM_ACTIVE_VERSIONS:-}\" != " + quotePOSIX(label) + " ]; then printf '%s\\n' " + quotePOSIX(message) + " >&2; fi\nexport _VSM_ACTIVE_VERSIONS=" + quotePOSIX(label) + "\n", nil
	case "fish":
		return "if test \"$VSM_NOTIFY\" = 1; and test \"$_VSM_ACTIVE_VERSIONS\" != " + quoteFish(label) + "\n    printf '%s\\n' " + quoteFish(message) + " >&2\nend\nset -gx _VSM_ACTIVE_VERSIONS " + quoteFish(label) + "\n", nil
	case "powershell", "pwsh":
		return "if ($env:VSM_NOTIFY -eq '1' -and $env:_VSM_ACTIVE_VERSIONS -ne " + quotePowerShell(label) + ") { [Console]::Error.WriteLine(" + quotePowerShell(message) + ") }\n$env:_VSM_ACTIVE_VERSIONS = " + quotePowerShell(label) + "\n", nil
	case "cmd":
		return fmt.Sprintf("if \"%%VSM_NOTIFY%%\"==\"1\" if not \"%%_VSM_ACTIVE_VERSIONS%%\"==\"%s\" echo %s 1>&2\r\nset \"_VSM_ACTIVE_VERSIONS=%s\"\r\n", quoteCmd(label), quoteCmd(message), quoteCmd(label)), nil
	}
	return "", nil
}
