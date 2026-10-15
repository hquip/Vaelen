package core

import (
	"runtime"
	"strings"
	"testing"
)

func TestEnvironmentInvariantsInRealShells(t *testing.T) {
	for _, sh := range []string{"bash", "sh", "zsh", "fish", "powershell", "cmd"} {
		t.Run(sh, func(t *testing.T) {
			if runtime.GOOS == "windows" && (sh == "sh" || sh == "zsh" || sh == "fish") {
				t.Skip("POSIX-only shell")
			}
			bin, err := findTestShell(sh)
			if err != nil {
				t.Skip("shell unavailable")
			}
			dirs := trickyDirs(t)
			a, _ := EnvScript(sh, dirs[:1], map[string]string{"GOROOT": "version-A", "JAVA_HOME": "java-A"})
			b, _ := EnvScript(sh, dirs[1:], map[string]string{"GOROOT": "version-B"})
			leave, _ := EnvScript(sh, nil, nil)
			na, _ := SwitchNoticeScript(sh, "go@1.0")
			nb, _ := SwitchNoticeScript(sh, "go@2.0")
			nl, _ := SwitchNoticeScript(sh, "")
			var script string
			var args []string
			switch sh {
			case "bash", "sh", "zsh":
				script = "export GOROOT='original-root' JAVA_HOME='original-java' VSM_NOTIFY=1\nunset VSM_PATH_BASE _VSM_ENV_KEYS _VSM_ACTIVE_VERSIONS\n" + a + na + "_test_path=$PATH\n" + a + na + "[ \"$PATH\" = \"$_test_path\" ] || exit 11\n" + b + nb + "[ \"$JAVA_HOME\" = original-java ] || exit 12\n" + leave + nl + "printf 'ROOT=%s\\nJAVA=%s\\n' \"$GOROOT\" \"$JAVA_HOME\"\n"
				args = []string{"-c", script}
			case "fish":
				script = "set -gx GOROOT original-root\nset -gx JAVA_HOME original-java\nset -gx VSM_NOTIFY 1\nset -e VSM_PATH_BASE _VSM_ENV_KEYS _VSM_ACTIVE_VERSIONS\n" + a + na + "set -l _test_path (string join : $PATH)\n" + a + na + "test (string join : $PATH) = $_test_path; or exit 11\n" + b + nb + "test $JAVA_HOME = original-java; or exit 12\n" + leave + nl + "echo ROOT=$GOROOT\necho JAVA=$JAVA_HOME\n"
				args = []string{"-c", script}
			case "powershell":
				script = "$ErrorActionPreference='Stop'\n$env:GOROOT='original-root';$env:JAVA_HOME='original-java';$env:VSM_NOTIFY='1'\nRemove-Item env:VSM_PATH_BASE,env:_VSM_ENV_KEYS,env:_VSM_ACTIVE_VERSIONS -ErrorAction SilentlyContinue\n" + a + na + "$_test_path=$env:PATH\n" + a + na + "if($env:PATH -ne $_test_path){exit 11}\n" + b + nb + "if($env:JAVA_HOME -ne 'original-java'){exit 12}\n" + leave + nl + "Write-Output ('ROOT='+$env:GOROOT)\nWrite-Output ('JAVA='+$env:JAVA_HOME)\n"
				args = []string{"-NoProfile", "-NonInteractive", "-Command", script}
			case "cmd":
				script = "@echo off\nset \"GOROOT=original-root\"\nset \"JAVA_HOME=original-java\"\nset \"VSM_NOTIFY=1\"\nset \"VSM_PATH_BASE=\"\nset \"_VSM_ENV_KEYS=\"\nset \"_VSM_ACTIVE_VERSIONS=\"\n" + a + na + "set \"_test_path=%PATH%\"\n" + a + na + "if not \"%PATH%\"==\"%_test_path%\" exit /b 11\n" + b + nb + "if not \"%JAVA_HOME%\"==\"original-java\" exit /b 12\n" + leave + nl + "echo ROOT=%GOROOT%\necho JAVA=%JAVA_HOME%\n"
				args = []string{"/D", "/V:OFF", "/c", writeTemp(t, "invariants.bat", strings.ReplaceAll(strings.ReplaceAll(script, "\r\n", "\n"), "\n", "\r\n"))}
			}
			out, stderr := runCmd(t, bin, args...)
			if !strings.Contains(out, "ROOT=original-root") || !strings.Contains(out, "JAVA=original-java") {
				t.Fatalf("did not restore original variables: %s stderr: %s", out, stderr)
			}
			if n := strings.Count(stderr, "[vsm]"); n != 3 {
				t.Fatalf("expected 3 transitions, got %d: %s", n, stderr)
			}
		})
	}
}

func TestEnvironmentRejectsCodeBearingKeys(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish", "powershell", "cmd"} {
		for _, key := range []string{"PATH", "VSM_PATH_BASE", "_VSM_ENV_KEYS", "X;touch injected", "X\nY"} {
			if _, err := EnvScript(sh, nil, map[string]string{key: "x"}); err == nil {
				t.Fatalf("%s accepted key %q", sh, key)
			}
		}
	}
}
