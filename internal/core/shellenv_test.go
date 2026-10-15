package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Windows' System32/bash.exe is a WSL launcher, not Git Bash. Native Windows
// paths rendered for MSYS must be tested with Git Bash, even if WSL is on PATH.
func findTestShell(sh string) (string, error) {
	if runtime.GOOS == "windows" && sh == "bash" {
		if git, err := exec.LookPath("git"); err == nil {
			for _, candidate := range []string{filepath.Join(filepath.Dir(git), "bash.exe"), filepath.Join(filepath.Dir(git), "..", "bin", "bash.exe"), filepath.Join(filepath.Dir(git), "..", "usr", "bin", "bash.exe")} {
				if fileExists(candidate) {
					return candidate, nil
				}
			}
		}
		return "", exec.ErrNotFound
	}
	return exec.LookPath(sh)
}

// ---------- 纯字符串层面的检查 ----------

// TestEnvScriptRestoresPATHWhenNoToolActive 离开项目目录（无工具生效）时，
// 所有 shell 都必须输出重建 PATH 的语句，否则上一个项目的版本会泄漏到后续命令。
func TestEnvScriptRestoresPATHWhenNoToolActive(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "sh", "fish", "powershell", "cmd"} {
		out, err := EnvScript(sh, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !strings.Contains(out, "VSM_PATH_BASE") {
			t.Fatalf("%s: 无工具生效时也必须重建 PATH，实际输出：\n%s", sh, out)
		}
	}
}

// TestEnvScriptRejectsUnknownShell 未知 shell 在任何目录下都要报错。
func TestEnvScriptRejectsUnknownShell(t *testing.T) {
	if _, err := EnvScript("nosuchshell", nil, nil); err == nil {
		t.Fatal("未知 shell 应报错（此前在无工具生效时会静默成功）")
	}
	if _, err := HookScript("nosuchshell", "/tmp/shims", ""); err == nil {
		t.Fatal("HookScript 未知 shell 应报错")
	}
}

// TestPOSIXQuotesAreNotNestedInDoubleQuotes 回归：单引号段被包进双引号后会变成
// 字面字符，PATH 里出现 '/opt/go/bin' 这种不存在的目录（真实执行见下方测试）。
func TestPOSIXQuotesAreNotNestedInDoubleQuotes(t *testing.T) {
	out, err := EnvScript("bash", []string{"/opt/go/bin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "export PATH=") && strings.Contains(line, `"'`) {
			t.Fatalf("PATH 单引号字面量不得出现在双引号内：\n%s", out)
		}
	}
}

func TestShellPathConvertsWindowsDrives(t *testing.T) {
	if runtime.GOOS != "windows" {
		if got := shellPath("/opt/go/bin"); got != "/opt/go/bin" {
			t.Fatalf("非 Windows 应原样返回，实际 %q", got)
		}
		return
	}
	if got := shellPath(`D:\Tools\go\bin`); got != "/d/Tools/go/bin" {
		t.Fatalf("应转换为 /d/Tools/go/bin，实际 %q", got)
	}
}

// ---------- 在真实 shell 中执行生成的脚本 ----------

// 带空格、单引号、$ 的目录：三类最容易被转义写坏的字符
func trickyDirs(t *testing.T) []string {
	t.Helper()
	base := t.TempDir()
	dirs := []string{
		filepath.Join(base, "My Java", "bin"),
		filepath.Join(base, "it's $HOME", "bin"),
	}
	return dirs
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runCmd(t *testing.T, name string, args ...string) (string, string) {
	t.Helper()
	c := exec.Command(name, args...)
	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		t.Fatalf("%s 执行失败: %v\nstdout:\n%s\nstderr:\n%s", name, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

// TestEnvScriptExecutesInPOSIXShells 在 bash / sh / zsh 中真实 eval 生成的脚本，
// 校验 PATH 精确等于「工具目录 + 基础 PATH」、环境变量正确设置，
// 且离开项目后 PATH 还原、GOROOT 被清除、全程无报错输出。
func TestEnvScriptExecutesInPOSIXShells(t *testing.T) {
	for _, sh := range []string{"bash", "sh", "zsh"} {
		sh := sh
		t.Run(sh, func(t *testing.T) {
			bin, err := findTestShell(sh)
			if err != nil {
				t.Skipf("未安装 %s", sh)
			}
			if runtime.GOOS == "windows" && sh != "bash" {
				t.Skip("Windows 上仅测试 Git Bash")
			}
			dirs := trickyDirs(t)
			env := map[string]string{"GOROOT": dirs[0], "JAVA_HOME": dirs[1]}
			enter, err := EnvScript(sh, dirs, env)
			if err != nil {
				t.Fatal(err)
			}
			leave, err := EnvScript(sh, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			enterFile := writeTemp(t, "enter.sh", enter)
			leaveFile := writeTemp(t, "leave.sh", leave)

			script := strings.Join([]string{
				"export VSM_PATH_BASE='/usr/bin:/bin'",
				`eval "$(cat ` + quotePOSIX(shellPath(enterFile)) + `)"`,
				`printf 'IN_PATH=%s\n' "$PATH"`,
				`printf 'IN_GOROOT=%s\n' "$GOROOT"`,
				`printf 'IN_JAVA=%s\n' "$JAVA_HOME"`,
				`eval "$(cat ` + quotePOSIX(shellPath(leaveFile)) + `)"`,
				`printf 'OUT_PATH=%s\n' "$PATH"`,
				`printf 'OUT_GOROOT=%s\n' "${GOROOT-<unset>}"`,
				`printf 'OUT_JAVA=%s\n' "${JAVA_HOME-<unset>}"`,
			}, "\n")
			stdout, stderr := runCmd(t, bin, "-c", script)
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("执行时不应有报错输出（zsh 分词问题会在这里暴露）：\n%s", stderr)
			}
			got := map[string]string{}
			for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
				if i := strings.IndexByte(line, '='); i > 0 {
					got[line[:i]] = strings.TrimRight(line[i+1:], "\r")
				}
			}
			wantIn := shellPath(dirs[0]) + ":" + shellPath(dirs[1]) + ":/usr/bin:/bin"
			if got["IN_PATH"] != wantIn {
				t.Fatalf("进入项目后 PATH 错误\n期望: %s\n实际: %s", wantIn, got["IN_PATH"])
			}
			if got["IN_GOROOT"] != dirs[0] || got["IN_JAVA"] != dirs[1] {
				t.Fatalf("环境变量错误: GOROOT=%q JAVA_HOME=%q", got["IN_GOROOT"], got["IN_JAVA"])
			}
			if got["OUT_PATH"] != "/usr/bin:/bin" {
				t.Fatalf("离开项目后 PATH 应还原为基础 PATH，实际 %q", got["OUT_PATH"])
			}
			if got["OUT_GOROOT"] != "<unset>" || got["OUT_JAVA"] != "<unset>" {
				t.Fatalf("离开项目后应清除注入的变量，实际 GOROOT=%q JAVA_HOME=%q", got["OUT_GOROOT"], got["OUT_JAVA"])
			}
		})
	}
}

// TestEnvScriptExecutesInFish 在 fish 中真实 source 生成的脚本。
func TestEnvScriptExecutesInFish(t *testing.T) {
	bin, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("未安装 fish")
	}
	dirs := trickyDirs(t)
	enter, _ := EnvScript("fish", dirs, map[string]string{"GOROOT": dirs[0]})
	leave, _ := EnvScript("fish", nil, nil)
	ef := writeTemp(t, "enter.fish", enter)
	lf := writeTemp(t, "leave.fish", leave)
	script := "set -gx VSM_PATH_BASE /usr/bin /bin\n" +
		"source " + quoteFish(ef) + "\n" +
		"echo IN_PATH=(string join : $PATH)\n" +
		"echo IN_GOROOT=$GOROOT\n" +
		"source " + quoteFish(lf) + "\n" +
		"echo OUT_PATH=(string join : $PATH)\n" +
		"if set -q GOROOT; echo OUT_GOROOT=set; else; echo OUT_GOROOT=unset; end\n"
	stdout, _ := runCmd(t, bin, "-c", script)
	want := []string{
		"IN_PATH=" + dirs[0] + ":" + dirs[1] + ":/usr/bin:/bin",
		"IN_GOROOT=" + dirs[0],
		"OUT_PATH=/usr/bin:/bin",
		"OUT_GOROOT=unset",
	}
	for _, w := range want {
		if !strings.Contains(stdout, w) {
			t.Fatalf("缺少 %q\n实际输出:\n%s", w, stdout)
		}
	}
}

// TestEnvScriptExecutesInPowerShell 在 PowerShell 中真实执行：$ 与单引号不得被展开/截断。
func TestEnvScriptExecutesInPowerShell(t *testing.T) {
	var bin string
	for _, name := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(name); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		t.Skip("未安装 PowerShell")
	}
	sep := string(os.PathListSeparator)
	base := "BASE_A" + sep + "BASE_B"
	dirs := trickyDirs(t)
	enter, _ := EnvScript("powershell", dirs, map[string]string{"GOROOT": dirs[1]})
	leave, _ := EnvScript("powershell", nil, nil)
	ef := writeTemp(t, "enter.ps1", enter)
	lf := writeTemp(t, "leave.ps1", leave)
	script := "$env:VSM_PATH_BASE = " + quotePowerShell(base) + "\n" +
		"Invoke-Expression ([System.IO.File]::ReadAllText(" + quotePowerShell(ef) + "))\n" +
		"[Console]::Out.WriteLine('IN_PATH=' + $env:PATH)\n" +
		"[Console]::Out.WriteLine('IN_GOROOT=' + $env:GOROOT)\n" +
		"Invoke-Expression ([System.IO.File]::ReadAllText(" + quotePowerShell(lf) + "))\n" +
		"[Console]::Out.WriteLine('OUT_PATH=' + $env:PATH)\n" +
		"[Console]::Out.WriteLine('OUT_GOROOT=' + [string]::IsNullOrEmpty($env:GOROOT))\n"
	stdout, _ := runCmd(t, bin, "-NoProfile", "-NonInteractive", "-Command", script)
	want := []string{
		"IN_PATH=" + dirs[0] + sep + dirs[1] + sep + base,
		"IN_GOROOT=" + dirs[1],
		"OUT_PATH=" + base,
		"OUT_GOROOT=True",
	}
	for _, w := range want {
		if !strings.Contains(stdout, w) {
			t.Fatalf("缺少 %q\n实际输出:\n%s", w, stdout)
		}
	}
}

// TestEnvScriptExecutesInCmd 在 cmd.exe 批处理中执行：% 必须正确转义。
func TestEnvScriptExecutesInCmd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅 Windows")
	}
	dir := filepath.Join(t.TempDir(), "100%", "bin")
	envBat := writeTemp(t, "env.bat", mustEnvScript(t, "cmd", []string{dir}))
	wrapper := writeTemp(t, "run.bat", "@echo off\r\n"+
		"set \"VSM_PATH_BASE=C:\\base\"\r\n"+
		"call \""+envBat+"\"\r\n"+
		"echo PATH=%PATH%\r\n")
	stdout, _ := runCmd(t, "cmd", "/c", wrapper)
	want := "PATH=" + dir + ";C:\\base"
	if !strings.Contains(stdout, want) {
		t.Fatalf("期望含 %q\n实际:\n%s", want, stdout)
	}
}

func mustEnvScript(t *testing.T, sh string, dirs []string) string {
	t.Helper()
	s, err := EnvScript(sh, dirs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestHookScriptRunsWithAbsoluteVsmPath 激活脚本在 vsm 不在 PATH、且路径含空格时
// 仍能调用到 vsm（macOS 上从 GUI 写入 ~/.zshrc 的典型情形）。
func TestHookScriptRunsWithAbsoluteVsmPath(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("未安装 bash")
	}
	dir := filepath.Join(t.TempDir(), "my tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "vsm")
	// 假 vsm：被调用 `env bash` 时输出一行可 eval 的语句
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'export VSM_HOOK_RAN=yes'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shims := filepath.Join(t.TempDir(), "my shims")
	hook, err := HookScript("bash", shims, fake)
	if err != nil {
		t.Fatal(err)
	}
	hf := writeTemp(t, "hook.sh", hook)
	stdout, stderr := runCmd(t, bash, "-c", `. `+quotePOSIX(shellPath(hf))+`; printf 'RAN=%s\nPATH=%s\n' "$VSM_HOOK_RAN" "$PATH"`)
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("激活脚本不应报错：\n%s", stderr)
	}
	if !strings.Contains(stdout, "RAN=yes") {
		t.Fatalf("钩子未调用到绝对路径下的 vsm：\n%s", stdout)
	}
	if !strings.Contains(stdout, "PATH="+shellPath(shims)+":") {
		t.Fatalf("shims 目录未正确前置到 PATH：\n%s", stdout)
	}
}

// TestPersistShellHookIsIdempotent 写入 shell 配置文件的区块只出现一次，且不破坏原内容。
func TestPersistShellHookIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 走注册表 PATH")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("alias ll='ls -l'"), 0o644); err != nil { // 无结尾换行
		t.Fatal(err)
	}
	p, st, err := PersistShellHook("zsh", "/Applications/VSM.app/Contents/MacOS/vsm")
	if err != nil || st != "added" || p != rc {
		t.Fatalf("首次写入应 added，实际 path=%s status=%s err=%v", p, st, err)
	}
	_, st, err = PersistShellHook("zsh", "/Applications/VSM.app/Contents/MacOS/vsm")
	if err != nil || st != "exists" {
		t.Fatalf("重复写入应返回 exists，实际 %s %v", st, err)
	}
	got, _ := os.ReadFile(rc)
	s := string(got)
	if strings.Count(s, hookBlockBegin) != 1 {
		t.Fatalf("区块应只出现一次：\n%s", s)
	}
	if !strings.HasPrefix(s, "alias ll='ls -l'\n") {
		t.Fatalf("原有内容应保留且补上换行：\n%s", s)
	}
	if !strings.Contains(s, `eval "$('/Applications/VSM.app/Contents/MacOS/vsm' activate zsh)"`) {
		t.Fatalf("激活语句不正确：\n%s", s)
	}
}

func TestShellRCFile(t *testing.T) {
	t.Setenv("ZDOTDIR", "")
	home := "/home/u"
	cases := map[string]string{
		"zsh":  filepath.Join(home, ".zshrc"),
		"fish": filepath.Join(home, ".config", "fish", "config.fish"),
	}
	if runtime.GOOS == "darwin" {
		cases["bash"] = filepath.Join(home, ".bash_profile")
	} else {
		cases["bash"] = filepath.Join(home, ".bashrc")
	}
	for sh, want := range cases {
		got, err := ShellRCFile(sh, home)
		if err != nil || got != want {
			t.Fatalf("%s: 期望 %s，实际 %s (%v)", sh, want, got, err)
		}
	}
	if _, err := ShellRCFile("tcsh", home); err == nil {
		t.Fatal("不支持的 shell 应报错")
	}
}
