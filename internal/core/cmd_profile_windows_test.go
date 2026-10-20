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

func executingActivationFixture(t *testing.T) (Activator, []byte) {
	t.Helper()
	a := activationFixture(t)
	source := filepath.Join(t.TempDir(), "runtime.go")
	program := `package main
import("fmt";"os";"path/filepath";"strings")
func main(){exe,_:=os.Executable();name:=strings.ToLower(filepath.Base(exe));version:=filepath.Base(filepath.Dir(exe));if name=="java.exe"{version=filepath.Base(filepath.Dir(filepath.Dir(exe)));fmt.Printf("java version %q\n",version)}else if name=="python.exe"{fmt.Println("Python "+version)}else{fmt.Println("v"+version)}}`
	if err := os.WriteFile(source, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(t.TempDir(), "fixture.exe")
	if out, err := exec.Command("go", "build", "-o", native, source).CombinedOutput(); err != nil {
		t.Fatalf("runtime fixture: %v %s", err, out)
	}
	data, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"17.0.12", "21.0.1"} {
		if err := os.WriteFile(filepath.Join(a.Paths.InstallDir("java", version), "bin", "java.exe"), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a.VsmExe = filepath.Join(t.TempDir(), "vsm.exe")
	if out, err := exec.Command("go", "build", "-o", a.VsmExe, "vsm").CombinedOutput(); err != nil {
		t.Fatalf("CLI fixture: %v %s", err, out)
	}
	return a, data
}

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

func TestCmdStartupRepairsPATHEXTOnce(t *testing.T) {
	root := t.TempDir()
	helper := filepath.Join(root, "vsm-cmd-init.cmd")
	if err := config.WriteFileAtomic(helper, []byte(cmdStartupScript(root)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(filepath.Join(root, "shims", "vsm-fixture.cmd"), []byte("@echo SHIM_OK\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, before, after string }{
		{"missing", ".EXE;.PY", ".EXE;.PY;.CMD"},
		{"lowercase", ".EXE;.cmd;.PY", ".EXE;.cmd;.PY"},
		{"unset", "", ".COM;.EXE;.BAT;.CMD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// cmd can populate an absent PATHEXT at process startup. Clear it
			// inside the test session to also exercise our own unset fallback.
			batch := "@echo off\r\nset \"PATHEXT=%VSM_TEST_EXTS%\"\r\ncall \"%VSM_TEST_INIT%\"\r\ncall \"%VSM_TEST_INIT%\"\r\nset PATHEXT\r\ncall vsm-fixture\r\n"
			file := filepath.Join(t.TempDir(), "extensions.cmd")
			if err := os.WriteFile(file, []byte(batch), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(windowsCmdPath(), "/D", "/V:OFF", "/C", file)
			// Already-prefixed PATH exercises the helper's early-return branch.
			cmd.Env = append(os.Environ(), "VSM_TEST_INIT="+helper, "PATH="+filepath.Join(root, "shims")+";"+os.Getenv("PATH"), "VSM_TEST_EXTS="+tc.before)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "PATHEXT="+tc.after+"\r\n") || !strings.Contains(string(out), "SHIM_OK") {
				t.Fatalf("PATHEXT repair must preserve extensions and be idempotent: %v\n%s", err, out)
			}
		})
	}
}

func TestCmdActivationDiscoversAllConfiguredTools(t *testing.T) {
	a, data := executingActivationFixture(t)
	if err := a.SetGlobalVersion("java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"node", "python"} {
		file := filepath.Join(a.Paths.InstallDir(tool, "1.2.3"), tool+".exe")
		if err := config.WriteFileAtomic(file, data, 0o755); err != nil {
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
	for _, row := range localCheck.Tools {
		if row.Tool == "java" && (row.Code != "current-directory-override" || row.Action == "" || !sameCommandFile(row.Command, filepath.Join(local, "java.exe"))) {
			t.Fatalf("missing actionable local override diagnosis: %+v", row)
		}
	}
	if data, err := os.ReadFile(filepath.Join(local, "java.exe")); err != nil || string(data) != "local override" {
		t.Fatalf("local program modified: %q %v", data, err)
	}
	// A real executable inside shims wins over java.cmd. Its location alone
	// must not be accepted as evidence of a managed dispatcher.
	shadow := filepath.Join(a.Paths.Shims(), "java.exe")
	if err := os.WriteFile(shadow, data, 0o755); err != nil {
		t.Fatal(err)
	}
	shadowCheck, err := a.VerifyCmdActivation(t.TempDir())
	if err != nil || shadowCheck.Passed {
		t.Fatalf("shadowing executable accepted as managed dispatcher: %+v %v", shadowCheck, err)
	}
	for _, row := range shadowCheck.Tools {
		if row.Tool == "java" && (row.Code != "shim-shadowed" || row.Action == "" || !sameCommandFile(row.Command, shadow)) {
			t.Fatalf("missing actionable shim override diagnosis: %+v", row)
		}
	}
	if _, err := os.Stat(shadow); err != nil {
		t.Fatalf("shadowing program removed: %v", err)
	}
	if err := os.Remove(shadow); err != nil {
		t.Fatal(err)
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

func TestCmdActivationRejectsWrongRuntimeBehindShim(t *testing.T) {
	a, _ := executingActivationFixture(t)
	if err := a.SetGlobalVersion("java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(filepath.Join(a.Paths.Root, "vsm-cmd-init.cmd"), []byte(cmdStartupScript(a.Paths.Root)), 0o644); err != nil {
		t.Fatal(err)
	}
	// The path is a perfectly valid VSM shim, but its dispatcher returns the
	// wrong runtime. Checking only its location must not count as success.
	if err := os.WriteFile(filepath.Join(a.Paths.Shims(), "java.cmd"), []byte("@echo off\r\n@echo java version \"21.0.1\"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check, err := a.VerifyCmdActivation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if check.Passed {
		t.Fatalf("false success for Java 17 selection executing Java 21: %+v", check)
	}
	if len(check.Tools) != 1 || !strings.Contains(check.Tools[0].Error, "21.0.1") || !strings.Contains(check.Tools[0].Error, "17.0.12") {
		t.Fatalf("did not compare actual versions: %+v", check)
	}
}

func TestCmdActivationRepairsMissingCMDExtension(t *testing.T) {
	a, _ := executingActivationFixture(t)
	if err := a.SetGlobalVersion("java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(filepath.Join(a.Paths.Root, "vsm-cmd-init.cmd"), []byte(cmdStartupScript(a.Paths.Root)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATHEXT", ".EXE")
	t.Setenv("PATH", filepath.Join(a.Paths.InstallDir("java", "21.0.1"), "bin")+";"+os.Getenv("PATH"))
	check, err := a.VerifyCmdActivation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !check.Passed || !check.PathExtRepaired || len(check.Tools) != 1 || check.Tools[0].ActualVersion != "17.0.12" || !sameDir(filepath.Dir(check.Tools[0].Command), a.Paths.Shims()) {
		t.Fatalf("activation did not repair .CMD before executing Java: %+v", check)
	}
	// Keep one cmd process alive through 17 -> 21 -> 17, loading the activation
	// helper only once. No user PATH, profile or actual installation is changed.
	dir := t.TempDir()
	batch := "@echo off\r\ncall \"%VSM_TEST_INIT%\"\r\ncall java -version\r\n\"%VSM_TEST_CLI%\" use -g java 21.0.1\r\ncall java -version\r\n\"%VSM_TEST_CLI%\" use -g java 17.0.12\r\ncall java -version\r\n"
	file := filepath.Join(dir, "same-session.cmd")
	if err := os.WriteFile(file, []byte(batch), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(windowsCmdPath(), "/D", "/V:OFF", "/C", file)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VSM_HOME="+a.Paths.Root, "VSM_TEST_INIT="+filepath.Join(a.Paths.Root, "vsm-cmd-init.cmd"), "VSM_TEST_CLI="+a.VsmExe, "VSM_PROJECT_BOUNDARY="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("same-session switch: %v %s", err, out)
	}
	var versions []string
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "java version ") {
			versions = append(versions, strings.TrimPrefix(line, "java version "))
		}
	}
	if strings.Join(versions, ",") != `"17.0.12","21.0.1","17.0.12"` {
		t.Fatalf("versions did not change in the same cmd after repairing PATHEXT: %s", out)
	}
}

func TestCmdExistingSessionWithSystemJavaBeforeShims(t *testing.T) {
	a, data := executingActivationFixture(t)
	if err := a.SetGlobalVersion("java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(a.Paths.Root, "vsm-cmd-init.cmd")
	if err := config.WriteFileAtomic(helper, []byte(cmdStartupScript(a.Paths.Root)), 0o644); err != nil {
		t.Fatal(err)
	}
	oracle := filepath.Join(t.TempDir(), "Oracle javapath")
	if err := config.WriteFileAtomic(filepath.Join(oracle, "java.exe"), data, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Match the customer's screenshot: system Java 21, Oracle javapath, then
	// VSM shims. Keep the very same cmd alive before and after loading once.
	batch := "@echo off\r\ncall java -version\r\ncall \"%VSM_TEST_INIT%\"\r\ncall java -version\r\n\"%VSM_TEST_CLI%\" use -g java 21.0.1\r\ncall java -version\r\n\"%VSM_TEST_CLI%\" use -g java 17.0.12\r\ncall java -version\r\n"
	file := filepath.Join(dir, "customer-session.cmd")
	if err := os.WriteFile(file, []byte(batch), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(windowsCmdPath(), "/D", "/V:OFF", "/C", file)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VSM_HOME="+a.Paths.Root, "VSM_TEST_INIT="+helper, "VSM_TEST_CLI="+a.VsmExe, "VSM_PROJECT_BOUNDARY="+dir,
		"PATH="+filepath.Join(a.Paths.InstallDir("java", "21.0.1"), "bin")+";"+oracle+";"+a.Paths.Shims()+";"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("customer cmd fixture: %v %s", err, out)
	}
	var versions []string
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "java version ") {
			versions = append(versions, strings.TrimPrefix(line, "java version "))
		}
	}
	if strings.Join(versions, ",") != `"21.0.1","17.0.12","21.0.1","17.0.12"` {
		t.Fatalf("same cmd did not recover after a single initialization: %s", out)
	}
	// The copied command must target the same helper, including custom homes.
	status := a.ActivationStatus("java", dir)
	command, err := CmdActivationCommand(a.Paths.Root)
	if err != nil || status.CurrentCmd != command {
		t.Fatalf("copied command uses the wrong home: %+v %v", status, err)
	}
}
