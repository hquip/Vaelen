package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRejectsHostInstallationBeforeWrites(t *testing.T) {
	p, err := InitProject(t.TempDir(), "docker", "golang:1.25.11")
	if err != nil {
		t.Fatal(err)
	}
	paths := p.Paths(Paths{})
	in := Installer{Paths: paths}
	for _, target := range []string{"", t.TempDir()} {
		if err := in.Install(integrityPlugin{}, "1.0", target, nil); !errors.Is(err, ErrDockerHostInstall) {
			t.Fatalf("Docker allowed host installation: %v", err)
		}
	}
	if err := in.Adopt(integrityPlugin{}, "1.0", "/outside/go"); !errors.Is(err, ErrDockerHostInstall) {
		t.Fatalf("Docker allowed adopting host binaries: %v", err)
	}
	if _, err := os.Stat(paths.Root); !os.IsNotExist(err) {
		t.Fatalf("rejected host operations wrote an environment directory: %v", err)
	}
}

func TestNPMMirrorStaysInProjectChildEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("VSM_HOME", home)
	t.Setenv("VSM_MIRROR", "custom")
	if err := os.WriteFile(filepath.Join(home, "mirror-sources.json"), []byte(`{"npm":"https://registry.example/"}`), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := InitProject(t.TempDir(), "local", "")
	if err != nil {
		t.Fatal(err)
	}
	paths := p.Paths(Paths{})
	reg := Registry{Paths: paths}
	a := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
	plan, err := a.PlanProjectEnvironment(p.Root)
	if err != nil || plan.Variables["npm_config_registry"] != "https://registry.example/" {
		t.Fatalf("npm registry missing: %v %v", plan, err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, ".npmrc")); !os.IsNotExist(err) {
		t.Fatal("project .npmrc changed")
	}
	p.Mode = "docker"
	p.Image = "node:22"
	args, err := p.DockerArgs(p.Root, "sha256:test", "node", nil)
	if err != nil || !strings.Contains(strings.Join(args, " "), "npm_config_registry=https://registry.example/") {
		t.Fatalf("Docker npm registry missing: %v %v", args, err)
	}
}

func TestProjectsHaveIndependentInstallsAndRequirements(t *testing.T) {
	global := Paths{Root: t.TempDir()}
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, ".tool-versions"), []byte("node 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var roots []Paths
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(parent, name)
		os.Mkdir(dir, 0o755)
		p, err := InitProject(dir, "local", "")
		if err != nil {
			t.Fatal(err)
		}
		paths := p.Paths(global)
		roots = append(roots, paths)
		if spec, _, err := ProjectRequirement("node", dir); err != nil || spec != "" {
			t.Fatalf("inherited parent configuration: %q %v", spec, err)
		}
		if err := paths.EnsureDirs(); err != nil {
			t.Fatal(err)
		}
		if !pathWithin(p.Root, paths.Installs()) {
			t.Fatal("install root is outside project")
		}
	}
	if roots[0].Installs() == roots[1].Installs() {
		t.Fatal("projects share installs")
	}
	if err := os.MkdirAll(roots[0].InstallDir("go", "1.22.3"), 0o755); err != nil {
		t.Fatal(err)
	}
	if versions, err := (Registry{Paths: roots[1]}).Installed("go"); err != nil || len(versions) != 0 {
		t.Fatal("project B sees project A's tools")
	}
}

func TestProjectInitIsIdempotentAndModeIsExplicit(t *testing.T) {
	root := t.TempDir()
	p, err := InitProject(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != "local" {
		t.Fatal("default is not local")
	}
	if _, err := InitProject(root, "local", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := InitProject(root, "versions", ""); err == nil {
		t.Fatal("silently changed existing environment")
	}
	if _, err := InitProject(t.TempDir(), "docker", ""); err == nil {
		t.Fatal("Docker image is required")
	}
	versions, err := InitProject(t.TempDir(), "versions", "")
	if err != nil {
		t.Fatal(err)
	}
	global := Paths{Root: t.TempDir()}
	if versions.Paths(global).Installs() != global.Installs() {
		t.Fatal("versions mode should share tools")
	}
}

func TestLocalProjectRejectsExternalStateAndLinks(t *testing.T) {
	p, err := InitProject(t.TempDir(), "local", "")
	if err != nil {
		t.Fatal(err)
	}
	paths := p.Paths(Paths{})
	in := Installer{Paths: paths}
	if err := in.Adopt(integrityPlugin{}, "1.0", "/outside/go"); err == nil {
		t.Fatal("local environment adopted shared binaries")
	}
	if err := in.Install(integrityPlugin{}, "1.0", t.TempDir(), nil); err == nil {
		t.Fatal("local environment installed outside project")
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := linkDir(outside, filepath.Join(root, ".vsm")); err != nil {
		t.Fatal(err)
	}
	if _, err := InitProject(root, "local", ""); err == nil {
		t.Fatal("environment followed a .vsm junction")
	}
}

func TestDockerArgumentsDoNotMountOtherProjects(t *testing.T) {
	p, err := InitProject(t.TempDir(), "docker", "busybox:1.37.0")
	if err != nil {
		t.Fatal(err)
	}
	args, err := p.DockerArgs(p.Root, "sha256:test", "sh", []string{"-c", "printf hello"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, required := range []string{"--cap-drop=ALL", "--read-only", "--security-opt=no-new-privileges", "dst=/workspace", "GOTOOLCHAIN=local"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing Docker boundary %s", required)
		}
	}
	if _, err := p.DockerArgs(t.TempDir(), "sha256:test", "sh", nil); err == nil {
		t.Fatal("accepted outside working directory")
	}
	if strings.Contains(joined, "docker.sock") || strings.Contains(joined, "--privileged") {
		t.Fatal("unsafe Docker privileges")
	}
}

func TestLocalProjectDoesNotFallBackForExecutableSuffix(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("Windows executable suffix")
	}
	p, err := InitProject(t.TempDir(), "local", "")
	if err != nil {
		t.Fatal(err)
	}
	paths := p.Paths(Paths{})
	reg := Registry{Paths: paths}
	a := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
	if err := a.RunProject(p.Root, "go.exe", []string{"version"}); err == nil || !strings.Contains(err.Error(), "未配置") {
		t.Fatalf("go.exe bypassed project version requirement: %v", err)
	}
}
