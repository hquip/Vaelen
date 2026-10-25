package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"vsm/internal/config"
)

type Project struct {
	Schema int    `json:"schema_version"`
	Mode   string `json:"mode"`
	Image  string `json:"image,omitempty"`
	Root   string `json:"root,omitempty"`
}

func ProjectDirectory(dir string) (string, error) {
	abs, err := canonicalPath(dir)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("project must be a directory")
	}
	return abs, nil
}

func InitProject(dir, mode, image string) (Project, error) {
	abs, err := canonicalPath(dir)
	if err != nil {
		return Project{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return Project{}, err
	}
	if !fi.IsDir() {
		return Project{}, fmt.Errorf("project must be a directory")
	}
	if mode == "" {
		mode = "local"
	}
	p := Project{Schema: 1, Mode: mode, Image: image, Root: abs}
	if err := p.Validate(); err != nil {
		return p, err
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return p, err
	}
	defer r.Close()
	if err := r.Mkdir(".vsm", 0o755); err != nil && !os.IsExist(err) {
		return p, err
	}
	if _, err := r.Readlink(".vsm"); err == nil {
		return p, fmt.Errorf("project .vsm directory cannot be a link")
	}
	d, err := r.OpenRoot(".vsm")
	if err != nil {
		return p, err
	}
	defer d.Close()
	if data, err := d.ReadFile("project.json"); err == nil {
		var existing Project
		if err := json.Unmarshal(data, &existing); err != nil {
			return p, err
		}
		if existing.Mode != mode || existing.Image != image {
			return p, fmt.Errorf("project already uses %s; refusing to replace its environment", existing.Mode)
		}
		if err := existing.Validate(); err != nil {
			return p, err
		}
		return p, nil
	} else if !os.IsNotExist(err) {
		return p, err
	}
	stored := p
	stored.Root = ""
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return p, err
	}
	if err := config.WriteFileAtomic(filepath.Join(abs, ".vsm", "project.json"), append(data, '\n'), 0o644); err != nil {
		return p, err
	}
	// Keep runtime state out of repositories without altering the project's gitignore.
	ignore, err := d.OpenFile(".gitignore", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		_, writeErr := ignore.Write([]byte("*\n!.gitignore\n!project.json\n"))
		closeErr := ignore.Close()
		if writeErr != nil {
			return p, writeErr
		}
		if closeErr != nil {
			return p, closeErr
		}
	} else if !os.IsExist(err) {
		return p, err
	}
	return p, nil
}

func (p Project) Validate() error {
	if p.Schema != 1 {
		return fmt.Errorf("unsupported project environment schema")
	}
	switch p.Mode {
	case "versions", "local":
		if p.Image != "" {
			return fmt.Errorf("image is only valid for docker mode")
		}
	case "docker":
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]*$`).MatchString(p.Image) {
			return fmt.Errorf("docker mode needs an explicit --image (for example golang:1.25.11)")
		}
	default:
		return fmt.Errorf("unknown project mode %q (versions/local/docker)", p.Mode)
	}
	return nil
}

func FindProject(start string) (*Project, error) {
	return findProjectBounded(start, os.Getenv("VSM_PROJECT_BOUNDARY"))
}

func findProjectBounded(start, boundary string) (*Project, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	if boundary != "" && !pathWithin(boundary, dir) {
		return nil, fmt.Errorf("directory outside bound project")
	}
	for {
		f, err := os.OpenInRoot(dir, filepath.Join(".vsm", "project.json"))
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(f, 64*1024+1))
			f.Close()
			if readErr != nil {
				return nil, readErr
			}
			if len(data) > 64*1024 {
				return nil, fmt.Errorf("project config too large")
			}
			var p Project
			if err := json.Unmarshal(data, &p); err != nil {
				return nil, err
			}
			if err := p.Validate(); err != nil {
				return nil, err
			}
			p.Root = dir
			return &p, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		if boundary != "" && sameDir(boundary, dir) {
			return nil, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

func (p Project) Paths(global Paths) Paths {
	if p.Mode == "versions" {
		global.ProjectRoot = p.Root
		global.ProjectMode = p.Mode
		return global
	}
	return Paths{Root: filepath.Join(p.Root, ".vsm", "env"), ProjectRoot: p.Root, ProjectMode: p.Mode}
}

func PathsForDirectory(global Paths, dir string) (Paths, error) {
	p, err := FindProject(dir)
	if err != nil {
		return Paths{}, err
	}
	if p == nil {
		return global, nil
	}
	if err := p.ValidateStateRoot(); err != nil {
		return Paths{}, err
	}
	return p.Paths(global), nil
}

func (p Project) ValidateStateRoot() error {
	r, err := os.OpenRoot(p.Root)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, name := range []string{filepath.Join(".vsm", "env"), filepath.Join(".vsm", "docker-cache")} {
		fi, err := r.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("project environment state cannot be a link: %s", name)
		}
		if _, err := r.Readlink(name); err == nil {
			return fmt.Errorf("project environment state cannot be a junction: %s", name)
		}
	}
	return nil
}

// ProjectVariables contains only project-owned runtime state, never system config.
func (p Project) Variables() map[string]string {
	if p.Mode != "local" {
		return nil
	}
	base := filepath.Join(p.Root, ".vsm", "env")
	return map[string]string{
		"VSM_PROJECT_ROOT": p.Root, "GOTOOLCHAIN": "local",
		"GOPATH": filepath.Join(base, "go"), "GOMODCACHE": filepath.Join(base, "cache", "go-mod"), "GOCACHE": filepath.Join(base, "cache", "go-build"),
		"GOBIN":            filepath.Join(base, "go", "bin"),
		"npm_config_cache": filepath.Join(base, "cache", "npm"), "npm_config_prefix": filepath.Join(base, "node"), "npm_config_userconfig": filepath.Join(base, "npmrc"),
		"PIP_CACHE_DIR": filepath.Join(base, "cache", "pip"), "PIP_REQUIRE_VIRTUALENV": "true", "PYTHONNOUSERSITE": "1",
		"PYTHONHOME": "", "PYTHONPATH": "", "VIRTUAL_ENV": "", "NODE_PATH": "",
		"CARGO_HOME": filepath.Join(base, "cargo"), "GRADLE_USER_HOME": filepath.Join(base, "gradle"),
		"DOTNET_CLI_HOME": filepath.Join(base, "dotnet-home"), "NUGET_PACKAGES": filepath.Join(base, "cache", "nuget"),
		"PUB_CACHE": filepath.Join(base, "cache", "pub"), "JULIA_DEPOT_PATH": filepath.Join(base, "cache", "julia"),
		"SCARB_CACHE": filepath.Join(base, "cache", "scarb"),
	}
}

func (p Project) PrepareVariables() error {
	if err := p.ValidateStateRoot(); err != nil {
		return err
	}
	r, err := os.OpenRoot(p.Root)
	if err != nil {
		return err
	}
	defer r.Close()
	for key, value := range p.Variables() {
		if key == "VSM_HOME" || key == "VSM_PROJECT_ROOT" || key == "npm_config_userconfig" || !strings.ContainsAny(value, "/\\") {
			continue
		}
		rel, err := filepath.Rel(p.Root, value)
		if err != nil {
			return err
		}
		if err := r.MkdirAll(rel, 0o755); err != nil {
			return err
		}
	}
	return nil
}
