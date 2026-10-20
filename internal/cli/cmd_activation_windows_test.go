package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/core"
)

func TestCmdGlobalSelectionChangesInSameRunningSession(t *testing.T) {
	bin, home, project := buildVsm(t), t.TempDir(), t.TempDir()
	source := filepath.Join(t.TempDir(), "java.go")
	program := `package main
import("fmt";"os";"path/filepath")
func main(){exe,_:=os.Executable();fmt.Println(filepath.Base(filepath.Dir(filepath.Dir(exe))))}`
	if err := os.WriteFile(source, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"17.0.14", "21.0.1"} {
		target := filepath.Join(home, "installs", "java", version, "bin", "java.exe")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("go", "build", "-o", target, source).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, out)
		}
	}
	// No real PATH or profiles are changed. Existing cmd has the VSM shim first,
	// exactly the precondition for live GUI switching in an already open session.
	startup := "@echo off\r\n" +
		"\"%VSM_TEST_CLI%\" use -g java 17.0.14\r\n" +
		"call java -version\r\n" +
		"\"%VSM_TEST_CLI%\" use -g java 21.0.1\r\n" +
		"call java -version\r\n" +
		"\"%VSM_TEST_CLI%\" use -g java 17.0.14\r\n" +
		"call java -version\r\n"
	file := filepath.Join(t.TempDir(), "same-session.cmd")
	if err := os.WriteFile(file, []byte(startup), 0o644); err != nil {
		t.Fatal(err)
	}
	// Generate shims using the production path, then keep PATH fixed for the
	// entire test. There are no activate calls between version selections.
	if code, out := runVsm(t, bin, home, project, "use", "-g", "java", "17.0.14"); code != 0 {
		t.Fatal(out)
	}
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/D", "/V:OFF", "/C", file)
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "VSM_HOME="+home, "VSM_TEST_CLI="+bin, "VSM_PROJECT_BOUNDARY="+project, "VSM_JAVA_VERSION=", "PATH="+filepath.Join(home, "shims")+";"+filepath.Join(home, "installs", "java", "21.0.1", "bin")+";"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cmd: %v %s", err, out)
	}
	var actual []string
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if line == "17.0.14" || line == "21.0.1" {
			actual = append(actual, line)
		}
	}
	if strings.Join(actual, ",") != "17.0.14,21.0.1,17.0.14" {
		t.Fatalf("same-session switch: %s", out)
	}
	// The new visible terminal uses the same dynamic shims, not a static version
	// bin ahead of them that would pin subsequent global selections.
	paths := core.Paths{Root: home}
	reg := core.Registry{Paths: paths}
	act := core.Activator{Paths: paths, Registry: reg, Resolver: core.Resolver{Paths: paths, Registry: reg}, VsmExe: bin}
	env, err := act.ActivatedTerminalEnvironment(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), "PATH=") && !strings.HasPrefix(entry[5:], paths.Shims()+";") {
			t.Fatalf("terminal pinned static runtime: %s", entry)
		}
	}
}
