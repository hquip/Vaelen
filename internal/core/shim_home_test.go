package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/config"
)

func TestShimsUseTheirOwningHomeInAnExistingTerminal(t *testing.T) {
	a := activationFixture(t)
	a.Paths.Root = filepath.Join(a.Paths.Root, "owner's home & tools")
	a.Registry.Paths = a.Paths
	a.Resolver.Paths, a.Resolver.Registry = a.Paths, a.Registry
	t.Setenv("VSM_HOME", a.Paths.Root)
	if err := a.Paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	other := Paths{Root: t.TempDir()}
	if err := other.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "java.go")
	program := "package main\nimport(\"fmt\";\"os\";\"path/filepath\";\"strings\")\nfunc main(){if len(os.Args)>1&&os.Args[1]==\"exit42\"{os.Exit(42)};p,_:=os.Executable();fmt.Printf(\"%s version %q\\n\",strings.TrimSuffix(filepath.Base(p),\".exe\"),filepath.Base(filepath.Dir(filepath.Dir(p))))}\n"
	if err := os.WriteFile(source, []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, item := range []struct {
		paths   Paths
		version string
	}{{a.Paths, "17.0.12"}, {a.Paths, "21.0.1"}, {other, "21.0.1"}} {
		destination := filepath.Join(item.paths.InstallDir("java", item.version), "bin", name)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if out, err := exec.Command("go", "build", "-o", destination, source).CombinedOutput(); err != nil {
			t.Fatalf("runtime fixture: %v %s", err, out)
		}
		if err := config.SetIn(item.paths.GlobalToolVersions(), "java", item.version); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetIn(a.Paths.GlobalToolVersions(), "java", "17.0.12"); err != nil {
		t.Fatal(err)
	}
	a.VsmExe = filepath.Join(t.TempDir(), "cli !cache!", "vsm")
	if runtime.GOOS == "windows" {
		a.VsmExe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", a.VsmExe, "vsm").CombinedOutput(); err != nil {
		t.Fatalf("CLI fixture: %v %s", err, out)
	}
	if err := a.GenerateShims(); err != nil {
		t.Fatal(err)
	}
	var shell, script string
	if runtime.GOOS == "windows" {
		shell = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		script = "@echo off\r\ncall java -version\r\ncall vsm which java\r\ncall vsm use -g java 21.0.1\r\ncall java -version\r\ncall vsm use -g java 17.0.12\r\ncall java -version\r\ncall java exit42\r\necho FAILURE_CODE=%errorlevel%\r\necho ORIGINAL_HOME=%VSM_HOME%\r\n"
	} else {
		var err error
		shell, err = exec.LookPath("sh")
		if err != nil {
			t.Skip("POSIX shell unavailable")
		}
		script = "java -version\nvsm which java\nvsm use -g java 21.0.1\njava -version\nvsm use -g java 17.0.12\njava -version\njava exit42\nprintf 'FAILURE_CODE=%s\\n' \"$?\"\nprintf 'ORIGINAL_HOME=%s\\n' \"$VSM_HOME\"\n"
	}
	file := filepath.Join(t.TempDir(), "existing-session.cmd")
	if err := os.WriteFile(file, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	modes := []string{"POSIX"}
	if runtime.GOOS == "windows" {
		modes = []string{"OFF", "ON"}
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.Command(shell, "/D", "/V:"+mode, "/C", file)
			} else {
				cmd = exec.Command(shell, file)
			}
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "VSM_HOME="+other.Root, "VSM_JAVA_VERSION=", "VSM_PROJECT_BOUNDARY="+cmd.Dir, "PATH="+a.Paths.Shims()+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("existing terminal: %v %s", err, out)
			}
			var versions []string
			for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
				if strings.HasPrefix(line, "java version ") {
					versions = append(versions, strings.TrimPrefix(line, "java version "))
				}
			}
			if strings.Join(versions, ",") != `"17.0.12","21.0.1","17.0.12"` || !strings.Contains(string(out), a.Paths.InstallDir("java", "17.0.12")) || !strings.Contains(string(out), "ORIGINAL_HOME="+other.Root) {
				t.Fatalf("owned shim followed a different home from the existing terminal: %s", out)
			}
			if !strings.Contains(string(out), "FAILURE_CODE=42") {
				t.Fatalf("exit code changed: %s", out)
			}
			global, err := config.Load(other.GlobalToolVersions())
			if err != nil {
				t.Fatal(err)
			}
			if version, _ := global.Get("java"); version != "21.0.1" {
				t.Fatalf("unrelated home was modified: %s", version)
			}
		})
	}
}
