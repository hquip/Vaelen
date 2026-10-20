package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"vsm/internal/config"
)

func runRegistryScript(t *testing.T, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$ErrorActionPreference='Stop';[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);"+script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("registry fixture: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCmdHookPreservesAutoRunAndRefusesExternalChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "已有环境 & tools")
	key := `Software\VSM\Tests\` + filepath.Base(t.TempDir())
	// Always use a private test key; never touch the real Command Processor key.
	t.Cleanup(func() {
		runRegistryScript(t, "[Microsoft.Win32.Registry]::CurrentUser.DeleteSubKeyTree("+quotePowerShell(key)+",$false)")
	})
	original := `set "ORIGINAL=%USERPROFILE%"`
	runRegistryScript(t, "$k=[Microsoft.Win32.Registry]::CurrentUser.CreateSubKey("+quotePowerShell(key)+");$k.SetValue('AutoRun',"+quotePowerShell(original)+",[Microsoft.Win32.RegistryValueKind]::ExpandString);$k.Dispose()")
	for i, want := range []string{"added", "exists"} {
		if status, err := persistCmdHookAt(root, key); err != nil || status != want {
			t.Fatalf("pass %d: %s %v", i, status, err)
		}
	}
	read := "$k=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey(" + quotePowerShell(key) + ");[pscustomobject]@{current=$k.GetValue('AutoRun',$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames);previous=$k.GetValue('VSM.AutoRun.Previous');kind=$k.GetValueKind('AutoRun').ToString()}|ConvertTo-Json -Compress;$k.Dispose()"
	var result struct{ Current, Previous, Kind string }
	if err := json.Unmarshal([]byte(runRegistryScript(t, read)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Previous != original || !strings.HasPrefix(result.Current, original+" & call ") || result.Kind != "ExpandString" {
		t.Fatalf("lost startup: %+v", result)
	}
	// Execute the generated registry command as a Unicode command line, as cmd
	// does at startup, including preservation of the previous environment setup.
	probe := exec.Command(windowsCmdPath())
	probe.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + windowsCmdPath() + `" /D /V:OFF /S /C "` + result.Current + ` & set ORIGINAL & set VSM_HOME & set PATH"`}
	if out, err := probe.CombinedOutput(); err != nil || !strings.Contains(string(out), "ORIGINAL=") || !strings.Contains(string(out), "VSM_HOME=") {
		t.Fatalf("AutoRun execution: %v %s", err, out)
	}
	// A changed install directory replaces our managed tail, never the user's prefix.
	newRoot := filepath.Join(t.TempDir(), "new home")
	if _, err := persistCmdHookAt(newRoot, key); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(runRegistryScript(t, read)), &result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Current, root) || !strings.Contains(result.Current, newRoot) || result.Previous != original {
		t.Fatalf("stale update: %+v", result)
	}
	runRegistryScript(t, "$k=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("+quotePowerShell(key)+",$true);$k.SetValue('AutoRun','echo user changed it');$k.Dispose()")
	if _, err := persistCmdHookAt(root, key); err == nil {
		t.Fatal("overwrote externally changed AutoRun")
	}
	if err := json.Unmarshal([]byte(runRegistryScript(t, read)), &result); err != nil || result.Current != "echo user changed it" {
		t.Fatalf("external command changed: %+v %v", result, err)
	}
}

func TestCmdStartupOverridesSystemPATHWithoutCodePageOrDuplicates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "中文 & tools (test)")
	if err := config.WriteFileAtomic(filepath.Join(root, "vsm-cmd-init.cmd"), []byte(cmdStartupScript(root)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "shims"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shims", "vsm-fixture.cmd"), []byte("@echo SELECTED\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command, err := CmdActivationCommand(root)
	if err != nil {
		t.Fatal(err)
	}
	// CALL loads the same helper used by AutoRun. Repeat to emulate nested cmds
	// and execute an actual shim, rather than only checking generated strings.
	batch := "@echo off\r\nset \"ORIGINAL=kept\"\r\n" + command + "\r\n" + command + "\r\nset PATH\r\nset VSM_HOME\r\ncall vsm-fixture\r\nset ORIGINAL\r\n"
	batch = strings.ReplaceAll(batch, command, `call "%VSM_TEST_INIT%"`)
	file := filepath.Join(t.TempDir(), "probe.cmd")
	if err := os.WriteFile(file, []byte(batch), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(windowsCmdPath(), "/D", "/V:OFF", "/C", file)
	cmd.Env = append(os.Environ(), "VSM_TEST_INIT="+filepath.Join(root, "vsm-cmd-init.cmd"))
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SELECTED") || !strings.Contains(string(out), "ORIGINAL=kept") {
		t.Fatalf("cmd activation failed: %v\n%s", err, out)
	}
	// cmd output uses OEM; avoid decoding Unicode bytes by asking a Unicode-aware
	// child to inspect the actual environment and serialize UTF-8.
	probe := "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);[pscustomobject]@{path=$env:PATH;home=$env:VSM_HOME}|ConvertTo-Json -Compress"
	batch = "@echo off\r\n" + command + "\r\n" + command + "\r\npowershell -NoProfile -NonInteractive -Command \"" + probe + "\"\r\n"
	// The batch text needs only ASCII: invoke its Unicode helper through an env var.
	batch = strings.ReplaceAll(batch, command, `call "%VSM_TEST_INIT%"`)
	if err := os.WriteFile(file, []byte(batch), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(windowsCmdPath(), "/D", "/V:OFF", "/C", file)
	cmd.Env = append(os.Environ(), "VSM_TEST_INIT="+filepath.Join(root, "vsm-cmd-init.cmd"))
	out, err = cmd.CombinedOutput()
	var got struct{ Path, Home string }
	if err != nil {
		t.Fatalf("cmd: %v %s", err, out)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if !strings.HasPrefix(got.Path, filepath.Join(root, "shims")+";") || strings.Count(got.Path, filepath.Join(root, "shims")) != 1 || !sameDir(got.Home, root) {
		t.Fatalf("incorrect environment: %+v", got)
	}
}

func TestCmdHookRejectsExpansionBeforeWriting(t *testing.T) {
	for _, name := range []string{"%TEMP%", "!value!"} {
		root := filepath.Join(t.TempDir(), name)
		if _, err := persistCmdHookAt(root, `Software\VSM\Tests\unused`); err == nil {
			t.Fatal("expansion path accepted")
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("created unsafe hook: %v", err)
		}
	}
}

func TestCmdActivationDiscoversAllConfiguredTools(t *testing.T) {
	a := activationFixture(t)
	if err := a.SetGlobalVersion("java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"node", "python"} {
		file := filepath.Join(a.Paths.InstallDir(tool, "1.2.3"), tool+".exe")
		if err := config.WriteFileAtomic(file, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := a.SetGlobalVersion(tool, "1.2.3"); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.WriteFileAtomic(filepath.Join(a.Paths.Root, "vsm-cmd-init.cmd"), []byte(cmdStartupScript(a.Paths.Root)), 0o644); err != nil {
		t.Fatal(err)
	}
	check, err := a.VerifyCmdActivation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !check.Passed || len(check.Tools) != 3 {
		t.Fatalf("automatic discovery failed: %+v", check)
	}
	for _, row := range check.Tools {
		if !row.Passed || !sameDir(filepath.Dir(row.Command), a.Paths.Shims()) {
			t.Fatalf("incorrect command: %+v", row)
		}
	}
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "java.exe"), []byte("local override"), 0o755); err != nil {
		t.Fatal(err)
	}
	localCheck, err := a.VerifyCmdActivation(local)
	if err != nil || localCheck.Passed {
		t.Fatalf("local command override missed: %+v %v", localCheck, err)
	}
	// A broken selection produces an explicit per-tool failure. Other tools and
	// the managed PATH are still reported, rather than a blanket success message.
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "python", "9.9.9"); err != nil {
		t.Fatal(err)
	}
	check, err = a.VerifyCmdActivation(t.TempDir())
	if err != nil || check.Passed {
		t.Fatalf("broken selection accepted: %+v %v", check, err)
	}
	for _, row := range check.Tools {
		if row.Tool == "python" && row.Error == "" {
			t.Fatal("missing explanation")
		}
	}
}
