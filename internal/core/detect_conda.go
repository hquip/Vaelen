package core

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

func binaryIdentity(path string) string {
	path = realPath(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func describeRuntime(tool, command, path, version string) SystemVersion {
	s := SystemVersion{Tool: tool, Command: command, Path: path, Version: version}
	if tool != "python" {
		return s
	}
	root := filepath.Dir(path)
	if filepath.Base(root) == "bin" || strings.EqualFold(filepath.Base(root), "Scripts") {
		root = filepath.Dir(root)
	}
	if prefix := plugin.CondaPrefix(root); prefix != "" {
		name := filepath.Base(prefix)
		var slug strings.Builder
		for _, c := range name {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
				slug.WriteRune(c)
			}
		}
		label := slug.String()
		if label == "" {
			label = "env"
		}
		if len(label) > 32 {
			label = label[:32]
		}
		digest := sha256.Sum256([]byte(binaryIdentity(prefix)))
		s.Environment = "Conda: " + name
		s.ManagedVersion = fmt.Sprintf("conda-%s-%x", label, digest[:4])
	}
	return s
}

// Discover existing Conda environments without running conda or traversing disks.
// environments.txt is Conda's per-user registry; envs folders supplement it.
func detectCondaPython(commands map[string][]string) []SystemVersion {
	if _, wanted := commands["python"]; !wanted {
		return nil
	}
	var roots []string
	add := func(path string) {
		if path != "" {
			roots = append(roots, path)
		}
	}
	add(os.Getenv("CONDA_PREFIX"))
	if exe := os.Getenv("CONDA_EXE"); exe != "" {
		root := filepath.Dir(exe)
		if name := strings.ToLower(filepath.Base(root)); name == "scripts" || name == "bin" || name == "condabin" {
			root = filepath.Dir(root)
		}
		add(root)
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		if data, err := config.ReadBounded(filepath.Join(home, ".conda", "environments.txt"), 1<<20); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if filepath.IsAbs(line) {
					add(line)
				}
			}
		}
		for _, name := range []string{"miniconda3", "anaconda3", "miniforge3", "mambaforge", ".conda"} {
			add(filepath.Join(home, name))
		}
	}
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramData")} {
			if base != "" {
				for _, name := range []string{"miniconda3", "anaconda3", "miniforge3"} {
					add(filepath.Join(base, name))
				}
			}
		}
	}
	// Only inspect immediate environment directories, never a recursive search.
	for _, root := range append([]string{}, roots...) {
		if entries, err := os.ReadDir(filepath.Join(root, "envs")); err == nil {
			for i, e := range entries {
				if i >= 256 {
					break
				}
				if e.IsDir() {
					add(filepath.Join(root, "envs", e.Name()))
				}
			}
		}
	}
	seen := map[string]bool{}
	var out []SystemVersion
	python, _ := plugin.Get("python")
	for _, root := range roots {
		if len(seen) >= 256 {
			break
		}
		prefix := plugin.CondaPrefix(root)
		if prefix == "" || seen[binaryIdentity(prefix)] {
			continue
		}
		seen[binaryIdentity(prefix)] = true
		bin := findExecutable(python.BinPaths(prefix), "python")
		if bin == "" {
			continue
		}
		version, err := probeVersion("python", bin)
		s := describeRuntime("python", filepath.Base(bin), bin, version)
		if err != nil {
			s.VersionError = err.Error()
		}
		out = append(out, s)
	}
	return out
}
