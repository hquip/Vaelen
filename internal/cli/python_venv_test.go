package cli

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRealIDEVenvKeepsPythonAndPipAfterShellRefresh(t *testing.T) {
	python := os.Getenv("VSM_TEST_PYTHON")
	if python == "" {
		t.Skip("set VSM_TEST_PYTHON to exercise real venv and pip isolation (enabled on CI)")
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "_VSM_") || strings.HasPrefix(key, "VSM_PYTHON_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("PYTHONHOME", "")
	t.Setenv("PYTHONPATH", "")
	t.Setenv("VSM_PATH_BASE", "")
	query := exec.Command(python, "-c", "import json,sys; print(json.dumps({'executable':sys.executable,'root':sys.base_prefix,'version':'.'.join(map(str,sys.version_info[:3]))}))")
	data, err := query.Output()
	if err != nil {
		t.Fatal(err)
	}
	var base struct{ Executable, Root, Version string }
	if err := json.Unmarshal(data, &base); err != nil {
		t.Fatal(err)
	}
	bin, home, dir := buildVsm(t), t.TempDir(), t.TempDir()
	// macOS exposes TempDir through /var -> /private/var. Project boundaries
	// use canonical paths, so the fixture must use the same actual directory.
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSM_PROJECT_BOUNDARY", dir)
	for _, args := range [][]string{{"adopt", "python", "--path", base.Executable}, {"use", "-g", "python", base.Version}, {"reshim"}} {
		if code, out := runVsm(t, bin, home, dir, args...); code != 0 {
			t.Fatalf("prepare: %v code=%d %s", args, code, out)
		}
	}
	wheel := filepath.Join(dir, "vsm_venv_probe-0.0.0-py3-none-any.whl")
	writeVenvProbeWheel(t, wheel)
	probe := filepath.Join(dir, "probe.py")
	if err := os.WriteFile(probe, []byte(`import importlib.util,json,pathlib,sys,sysconfig
pathlib.Path(sys.argv[1]).write_text(json.dumps({'prefix':sys.prefix,'purelib':sysconfig.get_path('purelib'),'package':importlib.util.find_spec('vsm_venv_probe') is not None}),encoding='utf-8')
`), 0644); err != nil {
		t.Fatal(err)
	}
	shells := []string{"bash"}
	if runtime.GOOS == "windows" {
		shells = []string{"powershell", "cmd"}
	} else {
		for _, sh := range []string{"zsh", "fish"} {
			if _, err := exec.LookPath(sh); err == nil {
				shells = append(shells, sh)
			}
		}
	}
	for _, sh := range shells {
		t.Run(sh, func(t *testing.T) {
			venv := filepath.Join(dir, sh+" IDE & project", ".venv")
			if sh == "cmd" {
				// CPython 3.10's activate.bat itself does not quote VIRTUAL_ENV.
				// Test VSM with spaces without triggering that upstream '&' bug.
				venv = filepath.Join(dir, sh+" IDE project", ".venv")
			}
			if output, err := exec.Command(base.Executable, "-m", "venv", venv).CombinedOutput(); err != nil {
				t.Fatalf("create venv: %v %s", err, output)
			}
			active, shim, after := filepath.Join(dir, sh+"-active.json"), filepath.Join(dir, sh+"-shim.json"), filepath.Join(dir, sh+"-after.json")
			var command *exec.Cmd
			switch sh {
			case "powershell":
				q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
				script := "$ErrorActionPreference='Stop'\nfunction Refresh-Vsm { & " + q(bin) + " env powershell | Out-String | Invoke-Expression }\nRefresh-Vsm\n& " + q(filepath.Join(venv, "Scripts", "Activate.ps1")) + "\nRefresh-Vsm\n"
				script += "pip install --no-index --no-deps --no-compile --disable-pip-version-check " + q(wheel) + "\nif($LASTEXITCODE){exit $LASTEXITCODE}\npython " + q(probe) + " " + q(active) + "\n"
				script += "& " + q(bin) + " exec python -- python " + q(probe) + " " + q(shim) + "\nif($LASTEXITCODE){exit $LASTEXITCODE}\nRefresh-Vsm\ndeactivate\nRefresh-Vsm\nif($env:VIRTUAL_ENV){throw 'VSM resurrected the venv'}\npython " + q(probe) + " " + q(after) + "\n"
				file := writeCLIFixture(t, "venv.ps1", script)
				command = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", file)
			case "cmd":
				q := func(s string) string { return "\"" + strings.ReplaceAll(s, "%", "%%") + "\"" }
				envScript := filepath.Join(dir, "refresh.cmd")
				refresh := q(bin) + " env cmd > " + q(envScript) + "\r\nif errorlevel 1 exit /b 21\r\ncall " + q(envScript) + "\r\n"
				script := "@echo off\r\n" + refresh + "call " + q(filepath.Join(venv, "Scripts", "activate.bat")) + "\r\n" + refresh
				script += "call pip install --no-index --no-deps --no-compile --disable-pip-version-check " + q(wheel) + "\r\nif errorlevel 1 exit /b 22\r\ncall python " + q(probe) + " " + q(active) + "\r\n"
				script += q(bin) + " exec python -- python " + q(probe) + " " + q(shim) + "\r\nif errorlevel 1 exit /b 23\r\n" + refresh + "call " + q(filepath.Join(venv, "Scripts", "deactivate.bat")) + "\r\n" + refresh + "if defined VIRTUAL_ENV exit /b 24\r\ncall python " + q(probe) + " " + q(after) + "\r\n"
				command = exec.Command("cmd", "/D", "/V:OFF", "/c", writeCLIFixture(t, "venv.cmd", script))
			case "bash", "zsh":
				q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
				refresh := "eval \"$(" + q(bin) + " env " + sh + ")\"\n"
				script := "set -e\nunset VSM_PATH_BASE\n" + refresh + ". " + q(filepath.Join(venv, "bin", "activate")) + "\n" + refresh
				script += "pip install --no-index --no-deps --no-compile --disable-pip-version-check " + q(wheel) + "\npython " + q(probe) + " " + q(active) + "\n"
				script += q(bin) + " exec python -- python " + q(probe) + " " + q(shim) + "\n" + refresh + "deactivate\n" + refresh + "test -z \"${VIRTUAL_ENV:-}\"\npython " + q(probe) + " " + q(after) + "\n"
				if sh == "bash" {
					command = exec.Command("bash", "--noprofile", "--norc", "-c", script)
				} else {
					command = exec.Command("zsh", "-f", "-c", script)
				}
			case "fish":
				q := func(s string) string {
					return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
				}
				refresh := q(bin) + " env fish | source\n"
				script := "set -e VSM_PATH_BASE\n" + refresh + "source " + q(filepath.Join(venv, "bin", "activate.fish")) + "\n" + refresh
				script += "pip install --no-index --no-deps --no-compile --disable-pip-version-check " + q(wheel) + "; or exit 21\npython " + q(probe) + " " + q(active) + "; or exit 22\n"
				script += q(bin) + " exec python -- python " + q(probe) + " " + q(shim) + "; or exit 23\n" + refresh + "deactivate\n" + refresh + "test -z \"$VIRTUAL_ENV\"; or exit 24\npython " + q(probe) + " " + q(after) + "; or exit 25\n"
				command = exec.Command("fish", "-c", script)
			}
			command.Dir = dir
			command.Env = append(os.Environ(), "VSM_HOME="+home)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("venv activation/refresh/install/deactivation: %v\n%s", err, output)
			}
			for file, want := range map[string]struct {
				root           string
				packagePresent bool
			}{active: {venv, true}, shim: {venv, true}, after: {base.Root, false}} {
				var result struct {
					Prefix, Purelib string
					Package         bool
				}
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				actual, errA := os.Stat(result.Prefix)
				expected, errB := os.Stat(want.root)
				if errA != nil || errB != nil || !os.SameFile(actual, expected) || result.Package != want.packagePresent {
					t.Fatalf("Python/pip wrote outside the selected environment: %s %s expected %s package=%v", file, data, want.root, want.packagePresent)
				}
			}
		})
	}
	t.Run("missing-pip-must-not-use-global-pip", func(t *testing.T) {
		venv := filepath.Join(dir, "without-pip")
		if output, err := exec.Command(base.Executable, "-m", "venv", "--without-pip", venv).CombinedOutput(); err != nil {
			t.Fatalf("create venv without pip: %v %s", err, output)
		}
		t.Setenv("VIRTUAL_ENV", venv)
		for _, command := range []string{"pip", "pip3"} {
			code, out := runVsm(t, bin, home, dir, "exec", "python", "--", command, "--version")
			if code == 0 || !strings.Contains(out, "No module named pip") {
				t.Fatalf("%s escaped the venv: code=%d %s", command, code, out)
			}
		}
		if code, out := runVsm(t, bin, home, dir, "exec", "python", "--", "python", "-c", "import sys;sys.exit(42)"); code != 42 {
			t.Fatalf("Python exit code was lost: %d %s", code, out)
		}
	})
}

func writeVenvProbeWheel(t *testing.T, file string) {
	t.Helper()
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, text := range map[string]string{
		"vsm_venv_probe/__init__.py":              "VALUE = 'venv-only'\n",
		"vsm_venv_probe-0.0.0.dist-info/METADATA": "Metadata-Version: 2.1\nName: vsm-venv-probe\nVersion: 0.0.0\n",
		"vsm_venv_probe-0.0.0.dist-info/WHEEL":    "Wheel-Version: 1.0\nGenerator: vsm-test\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
		"vsm_venv_probe-0.0.0.dist-info/RECORD":   "",
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprint(w, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
