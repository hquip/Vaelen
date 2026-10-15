package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunAutomaticallyUsesProjectVersion(t *testing.T) {
	bin := buildVsm(t)
	home, cwd := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".tool-versions"), []byte("go 1.23.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "main.go")
	code := `package main
import ("fmt";"os";"path/filepath")
func main(){exe,_:=os.Executable();fmt.Println("SELECTED="+filepath.Base(filepath.Dir(filepath.Dir(exe))));fmt.Println("OVERRIDE="+os.Getenv("VSM_GO_VERSION"));if len(os.Args)>1&&os.Args[1]=="fail"{os.Exit(42)}}`
	if err := os.WriteFile(source, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := "go"
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	target := filepath.Join(home, "installs", "go", "1.23.0", "bin", tool)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", target, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v %s", err, out)
	}
	old := filepath.Join(home, "installs", "go", "1.22.0", "bin", tool)
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSM_GO_VERSION", "1.22.0")
	status, out := runVsm(t, bin, home, cwd, "run", "--", "go")
	if status != 0 || !strings.Contains(out, "SELECTED=1.23.0") || !strings.Contains(out, "OVERRIDE=1.23.0") {
		t.Fatalf("did not select project toolchain: %d %s", status, out)
	}
	if status, out := runVsm(t, bin, home, cwd, "run", "--", "go", "fail"); status != 42 {
		t.Fatalf("child exit code lost: %d %s", status, out)
	}
	if got := os.Getenv("VSM_GO_VERSION"); got != "1.22.0" {
		t.Fatal("changed parent environment")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if status, _ := runVsm(t, bin, home, cwd, "run", "--", "go"); status == 0 {
		t.Fatal("broken project installation fell back to system Go")
	}
}
