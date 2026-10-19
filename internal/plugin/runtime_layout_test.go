package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCondaPythonEnvironmentLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "conda-meta"), 0755); err != nil {
		t.Fatal(err)
	}
	p, _ := Get("python")
	env := p.ExecEnv(root)
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if env["CONDA_PREFIX"] != physical || env["CONDA_DEFAULT_ENV"] != filepath.Base(physical) || env["CONDA_SHLVL"] != "1" || env["PYTHONHOME"] != "" {
		t.Fatalf("%+v", env)
	}
	if runtime.GOOS == "windows" {
		paths := p.BinPaths(root)
		for _, wanted := range []string{root, filepath.Join(root, "Scripts"), filepath.Join(root, "Library", "bin")} {
			found := false
			for _, path := range paths {
				if path == wanted {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing Conda executable/DLL directory %s: %v", wanted, paths)
			}
		}
	}
}

func TestMavenLegacyBinDirectoryRemainsUsable(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	name := "mvn"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	p, _ := Get("maven")
	paths := p.BinPaths(bin)
	if paths[len(paths)-1] != bin {
		t.Fatalf("legacy bin layout no longer resolves: %v", paths)
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.ExecEnv(bin)["MAVEN_HOME"] != physical {
		t.Fatalf("wrong Maven home: %+v", p.ExecEnv(bin))
	}
}
