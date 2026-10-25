package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestToolchainVersionProbesUseReadOnlyArguments(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	code := `package main
import("fmt";"os";"strings";"io")
func main(){
 switch os.Getenv("VSM_TEST_TOOLCHAIN_PROBE"){
 case "dotnet":if len(os.Args)!=2||os.Args[1]!="--list-sdks"{os.Exit(41)};fmt.Println("8.0.408 [/sdk]");fmt.Println("9.0.100 [/sdk]")
 case "tcl":if len(os.Args)!=1{os.Exit(42)};data,_:=io.ReadAll(os.Stdin);if !strings.Contains(string(data),"info patchlevel"){os.Exit(43)};fmt.Println("8.6.14")
 case "erlang":if len(os.Args)!=7||os.Args[1]!="-noshell"||os.Args[4]!="no_dot_erlang"||os.Args[5]!="-eval"||!strings.Contains(os.Args[6],"system_info"){os.Exit(44)};fmt.Println("27")
 case "fortran":if len(os.Args)!=2||os.Args[1]!="--version"{os.Exit(45)};fmt.Println("GNU Fortran 14.2.0")
 case "lua":if len(os.Args)!=2||os.Args[1]!="-v"{os.Exit(46)};fmt.Println("Lua 5.4.7")
 case "msvc":if len(os.Args)!=1||os.Getenv("CL")!=""||os.Getenv("_CL_")!=""{os.Exit(47)};fmt.Fprintln(os.Stderr,"Microsoft (R) C/C++ Optimizing Compiler Version 19.40.33811 for x64");os.Exit(2)
 case "godot":if len(os.Args)!=3||os.Args[1]!="--headless"||os.Args[2]!="--version"{os.Exit(48)};fmt.Println("4.7.2.stable.official.abcdef")
 case "ballerina":if len(os.Args)!=2||os.Args[1]!="version"{os.Exit(49)};fmt.Println("Ballerina 2201.14.1 (Swan Lake Update 14)")
 case "factor":if len(os.Args)!=2||os.Args[1]!="-version"{os.Exit(50)};fmt.Println("Factor 0.102 x86.64 (123)")
 case "coreutils":fmt.Println("factor (GNU coreutils) 9.7")
 case "regina":if len(os.Args)!=2||os.Args[1]!="-v"{os.Exit(51)};fmt.Println("REXX-Regina_3.9.5 5.00 31 Dec 2019")
 case "gnu-apl":if len(os.Args)!=2||os.Args[1]!="--version"{os.Exit(52)};fmt.Println("BUILDTAG:\n---------\n    Project:        GNU APL\n    Version / SVN:  1.9 / 1800\n    Build Date:     2026-01-01")
 }
}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v %s", err, out)
	}
	for _, test := range []struct{ tool, mode, want string }{{"dotnet", "dotnet", "9.0.100"}, {"tcl", "tcl", "8.6.14"}, {"erlang", "erlang", "27"}, {"gfortran", "fortran", "14.2.0"}, {"lua", "lua", "5.4.7"}, {"msvc", "msvc", "19.40.33811"}, {"godot", "godot", "4.7.2"}, {"ballerina", "ballerina", "2201.14.1"}, {"factor", "factor", "0.102"}, {"regina", "regina", "3.9.5"}, {"gnu-apl", "gnu-apl", "1.9"}} {
		t.Run(test.tool, func(t *testing.T) {
			t.Setenv("VSM_TEST_TOOLCHAIN_PROBE", test.mode)
			t.Setenv("CL", "unexpected.c")
			t.Setenv("_CL_", "unexpected.cpp")
			v, err := probeVersion(test.tool, bin)
			if err != nil || v != test.want {
				t.Fatalf("version %s %v, want %s", v, err, test.want)
			}
		})
	}
	t.Setenv("VSM_TEST_TOOLCHAIN_PROBE", "coreutils")
	if version, err := probeVersion("factor", bin); err == nil || version != "" {
		t.Fatalf("coreutils factor misidentified as language runtime: %s %v", version, err)
	}
}
