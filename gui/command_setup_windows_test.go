package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"vsm/internal/core"
)

func TestAutomaticCommandUsesGlobalHomeForEveryProjectMode(t *testing.T) {
	cli := filepath.Join(t.TempDir(), "vsm-windows-amd64.exe")
	if out, err := exec.Command("go", "build", "-o", cli, "vsm").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, out)
	}
	for _, mode := range []string{"", "versions", "local", "docker", "broken"} {
		t.Run(mode, func(t *testing.T) {
			a := newTestApp(t)
			globalHome := os.Getenv("VSM_HOME")
			c := *a.c()
			c.paths.ProjectMode = mode
			if mode == "local" || mode == "docker" {
				c.paths.Root = filepath.Join(t.TempDir(), ".vsm")
			}
			if mode == "broken" {
				c.initErr = errors.New("invalid project configuration")
			}
			c.act.VsmExe = cli
			a.st.Store(&c)
			called := false
			a.commandRegistrar = func(act core.Activator) (core.CommandSetup, error) {
				called = true
				if !samePath(act.Paths.Root, globalHome) || act.Paths.ProjectMode != "" || act.VsmExe != cli {
					t.Fatalf("registered project rather than global CLI: %+v", act)
				}
				return core.CommandSetup{Status: "added", CLI: cli}, nil
			}
			if result, err := a.EnsureCommand(); err != nil || !called || result.CLI != cli {
				t.Fatalf("automatic registration: %+v %v called=%v", result, err, called)
			}
			if a.c().paths.Root != c.paths.Root || a.c().paths.ProjectMode != mode {
				t.Fatal("automatic registration replaced the GUI's selected project")
			}
			registrationErr := errors.New("fixture registration denied")
			a.commandRegistrar = func(core.Activator) (core.CommandSetup, error) {
				return core.CommandSetup{}, registrationErr
			}
			if _, err := a.EnsureCommand(); !errors.Is(err, registrationErr) {
				t.Fatalf("registration error hidden: %v", err)
			}
			a.mu.Lock()
			_, err := a.EnsureCommand()
			a.mu.Unlock()
			if !errors.Is(err, errBusy) {
				t.Fatalf("registration ignored concurrent operation: %v", err)
			}
		})
	}
	// Download failure cannot leave a dispatcher pointing at a missing CLI.
	a := newTestApp(t)
	c := *a.c()
	c.act.VsmExe = ""
	a.st.Store(&c)
	t.Setenv("PATH", t.TempDir())
	missingErr := errors.New("fixture download unavailable")
	a.cliInstaller = func(context.Context, string, string) (string, error) { return "", missingErr }
	a.commandRegistrar = func(core.Activator) (core.CommandSetup, error) {
		t.Fatal("registered a command before obtaining a CLI")
		return core.CommandSetup{}, nil
	}
	if _, err := a.EnsureCommand(); !errors.Is(err, missingErr) {
		t.Fatalf("preparation error hidden: %v", err)
	}
}
