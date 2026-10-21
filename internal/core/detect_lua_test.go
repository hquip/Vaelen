package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLuaProbeUsesReadOnlyVersionFlag(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	dir := t.TempDir()
	source := `package main
import("fmt";"os")
func main(){
 if len(os.Args)!=2||os.Args[1]!="-v"{fmt.Fprintln(os.Stderr,"unrecognized option");os.Exit(1)}
 switch os.Getenv("VSM_TEST_LUA_PROBE") {
 case "luajit": fmt.Fprintln(os.Stderr,"LuaJIT 2.1.0-beta3 -- Copyright (C) 2005-2025 Mike Pall")
 case "unrelated": fmt.Println("linked library 5.4.7")
 case "failed": fmt.Fprintln(os.Stderr,"Lua 5.4.7");os.Exit(1)
 default: fmt.Fprintln(os.Stderr,"Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio")
 }
}`
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "lua")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("build Lua fixture: %v %s", err, out)
	}
	for _, mode := range []string{"standard", "luajit", "unrelated", "failed"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("VSM_TEST_LUA_PROBE", mode)
			want := map[string]string{"standard": "5.4.7", "luajit": "2.1.0-beta3"}[mode]
			for _, probe := range []func(string, string) (string, error){probeVersion, func(tool, bin string) (string, error) { return probeVersionCommand(tool, bin, nil, "") }} {
				got, err := probe("lua", bin)
				if got != want || (err == nil) != (want != "") {
					t.Fatalf("got %q, %v; want %q", got, err, want)
				}
			}
		})
	}
}

func TestNativeLuaRuntimeDiscovery(t *testing.T) {
	requested := os.Getenv("VSM_TEST_LUA")
	if requested == "" {
		t.Skip("set VSM_TEST_LUA to verify an installed Lua interpreter")
	}
	bin, err := exec.LookPath(requested)
	if err != nil {
		t.Fatal(err)
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSM_HOME", t.TempDir())
	want, err := probeVersion("lua", bin)
	if err != nil || want == "" {
		t.Fatalf("native Lua version: %q %v", want, err)
	}
	t.Setenv("PATH", filepath.Dir(bin))
	name := filepath.Base(bin)
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	versions := DetectSystem(map[string][]string{"lua": {name}}, "")
	if len(versions) != 1 || versions[0].Version != want || versions[0].VersionError != "" {
		t.Fatalf("native Lua discovery: %+v", versions)
	}
}
