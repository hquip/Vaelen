package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testFish(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("native fish tests run on Unix")
	}
	bin, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish unavailable")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return bin
}

func TestFishNestedShellKeepsPathListsAndRestoresVariables(t *testing.T) {
	bin := testFish(t)
	base := filepath.Join(t.TempDir(), "base space ' $PATH")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	enter, _ := EnvScript("fish", []string{filepath.Join(t.TempDir(), "java bin")}, map[string]string{"GOROOT": "managed-go", "JAVA_HOME": "managed-java"})
	leave, _ := EnvScript("fish", nil, nil)
	enterFile, leaveFile := writeTemp(t, "enter.fish", enter), writeTemp(t, "leave.fish", leave)
	child := writeTemp(t, "child.fish", `source `+quoteFish(leaveFile)+`
test (count $PATH) -eq 3; or exit 11
test "$PATH[1]" = `+quoteFish(base)+`; or exit 12
command ls -d / >/dev/null; or exit 13
test "$GOROOT" = original-go; or exit 14
test "$JAVA_HOME" = original-java; or exit 15
printf 'CHILD_OK\n'
`)
	script := `set -gx PATH ` + quoteFish(base) + ` /usr/bin /bin
set -e VSM_PATH_BASE
set -e _VSM_ENV_KEYS
set -gx GOROOT original-go
set -gx JAVA_HOME original-java
source ` + quoteFish(enterFile) + `
` + quoteFish(bin) + ` ` + quoteFish(child) + `; or exit $status
test "$GOROOT" = managed-go; or exit 16
test "$JAVA_HOME" = managed-java; or exit 17
source ` + quoteFish(leaveFile) + `
test (count $PATH) -eq 3; or exit 18
printf 'PARENT_OK\n'
`
	out, stderr := runCmd(t, bin, "-c", script)
	if !strings.Contains(out, "CHILD_OK") || !strings.Contains(out, "PARENT_OK") || strings.TrimSpace(stderr) != "" {
		t.Fatalf("nested fish failed: %s %s", out, stderr)
	}
}

func TestFishPathBaseRepairsColonSerializedInheritance(t *testing.T) {
	bin := testFish(t)
	leave, _ := EnvScript("fish", nil, nil)
	script := `set -gx --unpath VSM_PATH_BASE '/usr/bin:/bin'
` + leave + `
test (count $VSM_PATH_BASE) -eq 2; or exit 10
test (count $PATH) -eq 2; or exit 11
test "$PATH[1]" = /usr/bin; or exit 12
test "$PATH[2]" = /bin; or exit 13
command ls -d / >/dev/null; or exit 14
`
	runCmd(t, bin, "-c", script)
}

func TestFishNestedActivationKeepsCommandsAndSeparateEnvironment(t *testing.T) {
	bin := testFish(t)
	home := t.TempDir()
	shims := filepath.Join(home, "shims with space")
	if err := os.Mkdir(shims, 0755); err != nil {
		t.Fatal(err)
	}
	enter, _ := EnvScript("fish", []string{filepath.Join(home, "tool bin")}, map[string]string{"JAVA_HOME": "managed-java"})
	state := writeTemp(t, "env.fish", enter)
	if err := os.WriteFile(filepath.Join(shims, "vsm"), []byte("#!/bin/sh\n/bin/cat "+quotePOSIX(state)+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	hook, _ := HookScript("fish", shims, "vsm")
	hookFile := writeTemp(t, "activate.fish", hook)
	leave, _ := EnvScript("fish", nil, nil)
	leaveFile := writeTemp(t, "leave.fish", leave)
	child := writeTemp(t, "child.fish", `source `+quoteFish(hookFile)+`
test (count $PATH) -eq 4; or exit 11
test "$JAVA_HOME" = managed-java; or exit 12
command vsm env fish >/dev/null; or exit 13
command ls -d / >/dev/null; or exit 14
source `+quoteFish(leaveFile)+`
test "$JAVA_HOME" = original-java; or exit 15
test (count $PATH) -eq 3; or exit 16
`)
	script := `set -gx PATH /usr/bin /bin
set -e VSM_PATH_BASE
set -e _VSM_ENV_KEYS
set -gx JAVA_HOME original-java
source ` + quoteFish(hookFile) + `
` + quoteFish(bin) + ` ` + quoteFish(child) + `; or exit $status
test "$JAVA_HOME" = managed-java; or exit 17
test (count $PATH) -eq 4; or exit 18
source ` + quoteFish(leaveFile) + `
test "$JAVA_HOME" = original-java; or exit 19
`
	_, stderr := runCmd(t, bin, "-c", script)
	if strings.TrimSpace(stderr) != "" {
		t.Fatal(stderr)
	}
}

func TestFishHookDeduplicatesDirectoryAndPromptRefresh(t *testing.T) {
	bin := testFish(t)
	home := t.TempDir()
	counter := filepath.Join(home, "calls")
	state, _ := EnvScript("fish", nil, nil)
	stateFile := writeTemp(t, "state.fish", state)
	fake := filepath.Join(home, "fake cli")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'call\\n' >> "+quotePOSIX(counter)+"\n/bin/cat "+quotePOSIX(stateFile)+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	shims := filepath.Join(home, "shims")
	if err := os.Mkdir(shims, 0755); err != nil {
		t.Fatal(err)
	}
	hook, _ := HookScript("fish", shims, fake)
	hookFile := writeTemp(t, "hook.fish", hook)
	next := t.TempDir()
	script := `set -gx PATH /usr/bin ` + quoteFish(shims) + ` /bin ` + quoteFish(shims) + `
set -l _test_user_paths "$fish_user_paths"
set -gx --path VSM_PATH_BASE $PATH
source ` + quoteFish(hookFile) + `
test "$PATH[1]" = ` + quoteFish(shims) + `; or exit 11
test (count (string match -- ` + quoteFish(shims) + ` $PATH)) -eq 1; or exit 12
emit fish_prompt
test (count (string match call < ` + quoteFish(counter) + `)) -eq 1; or exit 13
cd ` + quoteFish(next) + `
emit fish_prompt
test (count (string match call < ` + quoteFish(counter) + `)) -eq 2; or exit 14
emit fish_prompt
test (count (string match call < ` + quoteFish(counter) + `)) -eq 3; or exit 15
source ` + quoteFish(hookFile) + `
emit fish_prompt
test (count (string match call < ` + quoteFish(counter) + `)) -eq 4; or exit 16
cd ` + quoteFish(home) + `
emit fish_prompt
test (count (string match call < ` + quoteFish(counter) + `)) -eq 5; or exit 17
test "$fish_user_paths" = "$_test_user_paths"; or exit 18
`
	_, stderr := runCmd(t, bin, "-c", script)
	if strings.TrimSpace(stderr) != "" {
		t.Fatal(stderr)
	}
}
