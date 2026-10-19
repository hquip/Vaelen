package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGlobalSwitchInRealPowerShell(t *testing.T) {
	var shell string
	for _, name := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(name); err == nil {
			shell = p
			break
		}
	}
	if shell == "" {
		t.Skip("PowerShell unavailable")
	}
	bin, home, project := buildVsm(t), t.TempDir(), t.TempDir()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	for _, version := range []string{"17.0.12", "21.0.1"} {
		dir := filepath.Join(home, "installs", "java", version, "bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		name := "java"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := "$ErrorActionPreference='Stop'\n" +
		"Remove-Item env:VSM_PATH_BASE,env:VSM_JAVA_VERSION,env:_VSM_ENV_KEYS -ErrorAction SilentlyContinue\n" +
		"function global:prompt { 'dev> ' }\n" +
		"& " + quote(bin) + " use -g java 17.0.12\nif($LASTEXITCODE -ne 0){throw 'global use failed'}\n" +
		"& " + quote(bin) + " activate powershell | Out-String | Invoke-Expression\n" +
		"[Console]::Out.WriteLine('FIRST='+$env:JAVA_HOME)\n" +
		"& " + quote(bin) + " use -g java 21.0.1\nif($LASTEXITCODE -ne 0){throw 'global use failed'}\n" +
		"$null=prompt\n[Console]::Out.WriteLine('SECOND='+$env:JAVA_HOME)\n" +
		"& " + quote(bin) + " use java 17.0.12\n$null=prompt\n[Console]::Out.WriteLine('PROJECT='+$env:JAVA_HOME)\n"
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "VSM_HOME="+home, "VSM_PROJECT_BOUNDARY="+project)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, item := range []struct{ label, version string }{{"FIRST", "17.0.12"}, {"SECOND", "21.0.1"}, {"PROJECT", "17.0.12"}} {
		if !strings.Contains(string(out), item.label+"="+filepath.Join(home, "installs", "java", item.version)) {
			t.Fatalf("global/project switch failed:\n%s", out)
		}
	}
}
