package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"vsm/internal/plugin"
)

func TestDatabaseProbesOnlyUseReadOnlyVersionArguments(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "probe.go")
	code := `package main
import("fmt";"os")
func main(){tool:=os.Getenv("VSM_DATABASE_EXPECT_TOOL");valid:=len(os.Args)==2&&os.Args[1]=="--version";if tool=="mysql"||tool=="mariadb"{valid=len(os.Args)==3&&os.Args[1]=="--no-defaults"&&os.Args[2]=="--version"};if valid{fmt.Println("database server 17.9");return};os.WriteFile(os.Getenv("VSM_DATABASE_MUTATION_MARKER"),[]byte("unsafe arguments"),0600);os.Exit(1)}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "probe")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	marker := filepath.Join(root, "mutated")
	t.Setenv("VSM_DATABASE_MUTATION_MARKER", marker)
	for _, tool := range []string{"mysql", "mariadb", "postgres", "redis", "valkey"} {
		t.Setenv("VSM_DATABASE_EXPECT_TOOL", tool)
		version, err := probeVersion(tool, bin)
		if err != nil || version != "17.9" {
			t.Fatalf("%s: %s %v", tool, version, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("probe invoked an unsafe database command")
	}
}

func TestDatabaseRegistrationDoesNotShadowSystemClients(t *testing.T) {
	paths := Paths{Root: t.TempDir()}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"mysql", "node"} {
		if err := os.MkdirAll(paths.InstallDir(tool, "8.4.1"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	name := func(command string) string {
		if runtime.GOOS == "windows" {
			return command + ".cmd"
		}
		return command
	}
	old := []byte("#!/bin/sh\nexec old-cli exec mysql \"$@\"\n")
	if runtime.GOOS == "windows" {
		old = []byte("@echo off\r\nold-cli exec mysql %*\r\n")
	}
	if err := os.WriteFile(filepath.Join(paths.Shims(), name("mysql")), old, 0755); err != nil {
		t.Fatal(err)
	}
	reg := Registry{Paths: paths}
	act := Activator{Paths: paths, Registry: reg, VsmExe: "new-vsm"}
	if err := act.GenerateShims(); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"mysql", "mysqld", "mysqladmin"} {
		if _, err := os.Stat(filepath.Join(paths.Shims(), name(command))); !os.IsNotExist(err) {
			t.Fatalf("database registration introduced a system command proxy: %s", command)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.Shims(), name("node"))); err != nil {
		t.Fatal("ordinary tool shim missing")
	}
	commands := plugin.DetectCommands()["mysql"]
	client := false
	for _, name := range commands {
		if name == "mysql" {
			client = true
		}
	}
	if !client {
		t.Fatal("project execution lost the database client alias")
	}
	probes := plugin.ProbeCommands()["mysql"]
	if len(probes) != 1 || probes[0] != "mysqld" {
		t.Fatalf("database scan includes administration utilities: %v", probes)
	}
}
