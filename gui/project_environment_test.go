package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vsm/internal/core"
)

func TestProjectEnvironmentSwitchAndInit(t *testing.T) {
	a := newTestApp(t)
	if err := a.SetProjectDir(""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.InitProjectEnvironment("local", ""); !errors.Is(err, errNoProjectDir) {
		t.Fatalf("initialization without a project: %v", err)
	}
	if _, err := a.MCPConfig(); !errors.Is(err, errNoProjectDir) {
		t.Fatalf("MCP without a project: %v", err)
	}
	for _, mode := range []string{"local", "versions", "docker"} {
		dir := t.TempDir()
		if err := a.SetProjectDir(dir); err != nil {
			t.Fatal(err)
		}
		if p, err := a.ProjectEnvironment(); err != nil || p != nil {
			t.Fatalf("new project inherited the previous environment: %+v, %v", p, err)
		}
		image := ""
		if mode == "docker" {
			image = "golang:1.25.11"
		}
		p, err := a.InitProjectEnvironment(mode, image)
		if err != nil {
			t.Fatal(err)
		}
		root, err := core.ProjectDirectory(dir)
		if err != nil {
			t.Fatal(err)
		}
		if p.Root != root || p.Mode != mode || p.Image != image {
			t.Fatalf("wrong initialized project: %+v", p)
		}
		status, err := a.ProjectEnvironment()
		if err != nil || status == nil || status.Schema != p.Schema || status.Mode != p.Mode || status.Image != p.Image {
			t.Fatalf("status does not reflect initialization: %+v, %v", status, err)
		}
		statusRoot, err := core.ProjectDirectory(status.Root)
		if err != nil || statusRoot != root {
			t.Fatalf("status refers to another project: %q, want %q; %v", statusRoot, root, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".vsm", "project.json")); err != nil {
			t.Fatal(err)
		}
		if mode == "local" && !samePath(a.GetInstallRoot(), filepath.Join(status.Root, ".vsm", "env", "installs")) {
			t.Fatalf("GUI still uses the shared installation directory: %s", a.GetInstallRoot())
		}
	}
}

func TestDockerGUIUsesOnlyImageTools(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	if err := a.SetProjectDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := a.InitProjectEnvironment("docker", "golang:1.25.11"); err != nil {
		t.Fatal(err)
	}
	for _, install := range []func() error{
		func() error { return a.Install("go", "latest", "") },
		func() error { return a.InstallVerified("go", "latest", t.TempDir(), true) },
		func() error { return a.AdoptTool("go", "1.25.11", "/outside/go") },
	} {
		if err := install(); !errors.Is(err, core.ErrDockerHostInstall) {
			t.Fatalf("GUI allowed host tool management: %v", err)
		}
	}
	if _, err := os.Stat(a.c().paths.Root); !os.IsNotExist(err) {
		t.Fatalf("GUI created host environment data before refusing installation: %v", err)
	}
	// Older builds may have installed host files in this project. Do not show
	// these as the container's installed or active toolchain.
	if err := os.MkdirAll(a.c().paths.InstallDir("go", "1.25.11"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := a.SetLocal("go", "1.25.11"); err != nil {
		t.Fatal(err)
	}
	status, err := a.Status()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range status {
		if tool.Tool == "go" && (tool.Spec != "1.25.11" || tool.Current != "" || len(tool.Installed) != 0) {
			t.Fatalf("GUI confused host files with image tools: %+v", tool)
		}
	}
}

func TestMCPConfigRejectsIncompatibleCLI(t *testing.T) {
	for _, mode := range []string{"valid", "legacy", "invalid-json", "missing-server"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMCPConfigOutputHelper$")
			cmd.Env = append(os.Environ(), "VSM_GUI_TEST_MCP_OUTPUT="+mode)
			config, err := mcpConfigFromCLI(cmd)
			if mode == "valid" {
				if err != nil || config["mcpServers"] == nil {
					t.Fatalf("valid CLI configuration rejected: %+v, %v", config, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "CLI") || config != nil {
				t.Fatalf("incompatible CLI produced a usable-looking configuration: %+v, %v", config, err)
			}
		})
	}
}

func TestMCPConfigOutputHelper(t *testing.T) {
	switch os.Getenv("VSM_GUI_TEST_MCP_OUTPUT") {
	case "":
		return
	case "valid":
		fmt.Println(`{"mcpServers":{"vsm":{"command":"/tools/vsm","args":["mcp","serve","--project","/work/app"]}}}`)
	case "legacy":
		fmt.Fprintln(os.Stderr, "unknown command: mcp")
		os.Exit(1)
	case "invalid-json":
		fmt.Println("vsm 0.1.0")
	case "missing-server":
		fmt.Println(`{"mcpServers":{}}`)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
