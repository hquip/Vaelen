package core

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoDetectionReportsBundledVersionInsideNewerProject(t *testing.T) {
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(runtime.GOROOT(), "bin", name)
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if info.Path != "cmd/go" {
		t.Skip("compiler GOROOT does not contain a standard Go command")
	}
	want := strings.TrimPrefix(info.GoVersion, "go")
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.test/probe\n\ngo 99.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	userEnv := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(userEnv, []byte("GOTOOLCHAIN=go99.0.0+auto\nGOROOT=/does/not/exist\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	t.Setenv("GOTOOLCHAIN", "go99.0.0+auto")
	t.Setenv("GOROOT", filepath.Join(project, "not-a-toolchain"))
	t.Setenv("GOENV", userEnv)
	t.Setenv("GOWORK", filepath.Join(project, "missing.work"))
	t.Setenv("GOFLAGS", "-invalid-probe-flag")
	for name, probe := range map[string]func(string) (string, error){"metadata": probeGoVersion, "local command fallback": probeGoVersionCommand} {
		t.Run(name, func(t *testing.T) {
			got, err := probe(bin)
			if err != nil || got != want {
				t.Fatalf("got %q, %v; want bundled version %q", got, err, want)
			}
		})
	}
	if os.Getenv("GOTOOLCHAIN") != "go99.0.0+auto" || os.Getenv("GOENV") != userEnv {
		t.Fatal("probe changed the parent environment")
	}
}

func TestGoProbeEnvHasOneLocalOverride(t *testing.T) {
	input := []string{"Path=/bin", "GOROOT=/old", "goroot=/other", "GoToolChain=auto", "GOENV=/user/env", "GOWORK=/project/go.work", "GOFLAGS=-x", "HOME=/home/test"}
	got := goProbeEnv(input)
	want := []string{"Path=/bin", "HOME=/home/test", "GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected isolated environment: %v", got)
	}
	if input[1] != "GOROOT=/old" {
		t.Fatal("input slice was mutated")
	}
}

func TestGoProbeDoesNotParseFailuresAsVersions(t *testing.T) {
	dir := t.TempDir()
	source := `package main
import("fmt";"os")
func main(){
switch os.Getenv("VSM_TEST_PROBE_MODE") {
case "failure":fmt.Println("go: downloading go1.99.0; requires go >= 1.99.0");os.Exit(1)
case "diagnostic":fmt.Println("warning: requires go1.99.0")
case "wrong-command":fmt.Println("other tool 1.99.0")
case "prerelease":fmt.Println("go version go1.27rc1 linux/amd64")
default:fmt.Println("go version go1.22.3 linux/amd64")
}}
`
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "go-probe")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v %s", err, out)
	}
	for _, mode := range []string{"failure", "diagnostic", "wrong-command", "prerelease", "stable"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("VSM_TEST_PROBE_MODE", mode)
			got, err := probeGoVersion(bin)
			want := ""
			if mode == "stable" {
				want = "1.22.3"
			}
			if mode == "prerelease" {
				want = "1.27rc1"
			}
			if got != want || (err == nil) != (want != "") {
				t.Fatalf("mode %s returned %q, %v", mode, got, err)
			}
		})
	}
}
