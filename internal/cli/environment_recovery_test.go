package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBrokenProjectStillEmitsRestoreAndAllowsExplicitMCPConfig(t *testing.T) {
	bin, home, broken, target := buildVsm(t), t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(broken, ".vsm"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".vsm", "project.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"bash", "fish", "powershell", "cmd"} {
		cmd := exec.Command(bin, "env", shell)
		cmd.Dir = broken
		cmd.Env = append(os.Environ(), "VSM_HOME="+home)
		var out, stderr strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			t.Fatal("invalid project succeeded")
		}
		if !strings.Contains(out.String(), "VSM_PATH_BASE") || !strings.Contains(out.String(), "_VSM_ENV_KEYS") || stderr.Len() == 0 {
			t.Fatalf("missing complete restoration for %s: %s / %s", shell, out.String(), stderr.String())
		}
		if shell == "powershell" && runtime.GOOS == "windows" {
			script := writeCLIFixture(t, "restore.ps1", "$env:PATH='previous-version';$env:VSM_PATH_BASE='original-path';$env:JAVA_HOME='old-java';$env:_VSM_ENV_KEYS='JAVA_HOME';$env:_VSM_HAD_JAVA_HOME='1';$env:_VSM_ORIG_JAVA_HOME='original-java'\n"+out.String()+"\nif($env:PATH -ne 'original-path' -or $env:JAVA_HOME -ne 'original-java'){exit 42}")
			if output, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-File", script).CombinedOutput(); err != nil {
				t.Fatalf("restore did not execute: %v %s", err, output)
			}
		}
	}
	if code, out := runVsm(t, bin, home, broken, "mcp", "config", "--project", target); code != 0 || !strings.Contains(out, "mcpServers") {
		t.Fatalf("unrelated cwd blocked explicit target: %d %s", code, out)
	}
}

func writeCLIFixture(t *testing.T, name, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestTemporaryShellSelectionSurvivesStartupHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real bash startup fixture runs on Unix CI")
	}
	bin, home, dir := buildVsm(t), t.TempDir(), t.TempDir()
	userHome := t.TempDir()
	for _, version := range []string{"17", "21"} {
		file := filepath.Join(home, "installs", "java", version, "bin", "java")
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("#!/bin/sh\nprintf 'JAVA_"+version+"\\n'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".tool-versions"), []byte("java 17\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rc := `eval "$("$VSM_TEST_CLI" activate bash)"
java -version
_vsm_hook
java -version
exit
`
	if err := os.WriteFile(filepath.Join(userHome, ".bashrc"), []byte(rc), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "shell", "java", "21", "bash")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VSM_HOME="+home, "HOME="+userHome, "VSM_TEST_CLI="+bin, "VSM_JAVA_VERSION=17", "VSM_PATH_BASE=/usr/bin:/bin")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.Count(string(output), "JAVA_21") != 2 || strings.Contains(string(output), "JAVA_17") {
		t.Fatalf("temporary session overwritten: %v %s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(home, ".tool-versions"))
	if err != nil || string(data) != "java 17\n" {
		t.Fatalf("temporary session changed default: %s %v", data, err)
	}
}
