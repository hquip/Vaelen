package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/buildinfo"
)

func TestCommandSetupRegistersUnrenamedCLIWithoutLanguageActivation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home & tools")
	t.Setenv("VSM_HOME", root)
	cli := filepath.Join(t.TempDir(), "official downloads", "vsm-windows-amd64.exe")
	if out, err := exec.Command("go", "build", "-o", cli, "vsm").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, out)
	}
	key := `Software\VSM\Tests\` + filepath.Base(t.TempDir())
	t.Cleanup(func() {
		runRegistryScript(t, "[Microsoft.Win32.Registry]::CurrentUser.DeleteSubKeyTree("+quotePowerShell(key)+",$false)")
	})
	environmentKey, cmdKey := key+`\Environment`, key+`\Command Processor`
	original := `%SystemRoot%\System32;C:\kept $tools`
	runRegistryScript(t, "$k=[Microsoft.Win32.Registry]::CurrentUser.CreateSubKey("+quotePowerShell(environmentKey)+");$k.SetValue('Path',"+quotePowerShell(original)+",[Microsoft.Win32.RegistryValueKind]::ExpandString);$k.Dispose()")
	a := Activator{Paths: Paths{Root: root}, VsmExe: cli}
	for _, want := range []string{"added", "exists"} {
		result, err := a.registerWindowsCommandAt(environmentKey, cmdKey)
		if err != nil || result.Status != want || result.CLI != cli {
			t.Fatalf("register: %+v %v", result, err)
		}
	}
	var persisted struct{ Path, Kind string }
	script := "$k=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey(" + quotePowerShell(environmentKey) + ");[pscustomobject]@{path=$k.GetValue('Path',$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames);kind=$k.GetValueKind('Path').ToString()}|ConvertTo-Json -Compress;$k.Dispose()"
	if err := json.Unmarshal([]byte(runRegistryScript(t, script)), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Path != a.Paths.Shims()+";"+original || persisted.Kind != "ExpandString" {
		t.Fatalf("PATH lost existing entries, expansion, type, or gained duplicates: %+v", persisted)
	}
	// A stale parent PATH has no VSM entry. Execute exactly the registered
	// AutoRun helper, without installing a real AutoRun on the test machine.
	batch := "@echo off\r\ncall \"%VSM_TEST_INIT%\"\r\ncall \"%VSM_TEST_INIT%\"\r\ncall vsm version\r\nexit /b %errorlevel%\r\n"
	file := filepath.Join(t.TempDir(), "existing-parent.cmd")
	if err := os.WriteFile(file, []byte(batch), 0644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"OFF", "ON"} {
		cmd := exec.Command(windowsCmdPath(), "/D", "/V:"+mode, "/C", file)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "PATH="+filepath.Join(os.Getenv("SystemRoot"), "System32"), "PATHEXT=.EXE", "VSM_HOME="+t.TempDir(), "VSM_TEST_INIT="+filepath.Join(root, "vsm-cmd-init.cmd"))
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "vsm "+buildinfo.DisplayVersion() {
			t.Fatalf("cmd /V:%s cannot run unqualified vsm: %v %s", mode, err, out)
		}
	}
	files, err := os.ReadDir(a.Paths.Shims())
	if err != nil || len(files) != 1 || files[0].Name() != "vsm.cmd" {
		t.Fatalf("command setup generated language shims: %v %v", files, err)
	}
	if _, err := os.Stat(a.Paths.GlobalToolVersions()); !os.IsNotExist(err) {
		t.Fatalf("command setup changed global language versions: %v", err)
	}
}

func TestCommandSetupRejectsProjectOrMissingCLIWithoutPersisting(t *testing.T) {
	key := `Software\VSM\Tests\unused-command-setup`
	for _, mode := range []string{"local", "docker", ""} {
		root := filepath.Join(t.TempDir(), "not-created")
		a := Activator{Paths: Paths{Root: root, ProjectMode: mode}}
		if _, err := a.registerWindowsCommandAt(key, key); err == nil {
			t.Fatal("project or missing CLI accepted")
		}
		if _, err := os.Stat(filepath.Join(root, "vsm-cmd-init.cmd")); !os.IsNotExist(err) {
			t.Fatalf("failed setup wrote startup hook: %v", err)
		}
	}
}
