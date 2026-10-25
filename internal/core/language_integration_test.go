package core

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/config"
)

func TestRealCAndCXXToolchainBuildsPrograms(t *testing.T) {
	cc, cxx := os.Getenv("VSM_TEST_CC"), os.Getenv("VSM_TEST_CXX")
	if cc == "" || cxx == "" {
		t.Skip("set VSM_TEST_CC and VSM_TEST_CXX to installed compilers")
	}
	for _, test := range []struct{ tool, command, source, code string }{{"gcc", cc, "hello.c", "#include <stdio.h>\nint main(void){puts(\"C_OK\");return 0;}\n"}, {"gcc", cxx, "hello.cpp", "#include <iostream>\nint main(){std::cout<<\"CXX_OK\\n\";}\n"}} {
		t.Run(test.source, func(t *testing.T) {
			home, project := t.TempDir(), t.TempDir()
			t.Setenv("VSM_HOME", home)
			t.Setenv("VSM_PROJECT_BOUNDARY", "")
			bin, err := exec.LookPath(test.command)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := (Installer{Paths: Paths{Root: home}}).AdoptPath(test.tool, bin, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := config.SetIn(filepath.Join(project, ".tool-versions"), test.tool, selected.Version); err != nil {
				t.Fatal(err)
			}
			paths := Paths{Root: home}
			reg := Registry{Paths: paths}
			act := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
			file := filepath.Join(project, test.source)
			if err := os.WriteFile(file, []byte(test.code), 0600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(project, "hello")
			if runtime.GOOS == "windows" {
				out += ".exe"
			}
			var log bytes.Buffer
			if err := act.RunProjectContext(context.Background(), project, filepath.Base(bin), []string{file, "-o", out}, nil, &log, &log); err != nil {
				t.Fatalf("compile: %v %s", err, log.String())
			}
			text, err := exec.Command(out).CombinedOutput()
			if err != nil || !strings.Contains(string(text), "_OK") {
				t.Fatalf("compiled program: %v %s", err, text)
			}
		})
	}
}

func TestRealDotnetSDKBuildsCSharpFSharpAndVisualBasic(t *testing.T) {
	requested := os.Getenv("VSM_TEST_DOTNET")
	if requested == "" {
		t.Skip("set VSM_TEST_DOTNET to an installed .NET SDK host")
	}
	bin, err := exec.LookPath(requested)
	if err != nil {
		t.Fatal(err)
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTNET_CLI_HOME", t.TempDir())
	t.Setenv("DOTNET_SKIP_FIRST_TIME_EXPERIENCE", "1")
	t.Setenv("DOTNET_CLI_TELEMETRY_OPTOUT", "1")
	t.Setenv("NUGET_PACKAGES", t.TempDir())
	t.Setenv("VSM_PROJECT_BOUNDARY", "")
	home := t.TempDir()
	t.Setenv("VSM_HOME", home)
	selected, err := (Installer{Paths: Paths{Root: home}}).AdoptPath("dotnet", bin, "")
	if err != nil {
		t.Fatal(err)
	}
	paths := Paths{Root: home}
	reg := Registry{Paths: paths}
	act := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
	realBin := realPath(bin)
	libraryPacks := filepath.Join(filepath.Dir(realBin), "sdk", selected.Version, "FSharp", "library-packs")
	for _, language := range []string{"C#", "F#", "VB"} {
		t.Run(language, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "global.json"), []byte(`{"sdk":{"version":"`+selected.Version+`"}}`), 0644); err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			run := func(args ...string) {
				t.Helper()
				log.Reset()
				if err := act.RunProjectContext(context.Background(), dir, "dotnet", args, nil, &log, &log); err != nil {
					t.Fatalf("dotnet %v: %v %s", args, err, log.String())
				}
			}
			run("new", "console", "--language", language, "--name", "CoverageHello", "--no-restore")
			// FSharp.Core ships with the SDK. Restore without third-party sources.
			if err := os.WriteFile(filepath.Join(dir, "CoverageHello", "NuGet.Config"), []byte(`<configuration><packageSources><clear/><add key="sdk" value="`+libraryPacks+`"/></packageSources><fallbackPackageFolders><clear/></fallbackPackageFolders></configuration>`), 0600); err != nil {
				t.Fatal(err)
			}
			run("build", filepath.Join("CoverageHello", "CoverageHello."+map[string]string{"C#": "csproj", "F#": "fsproj", "VB": "vbproj"}[language]), "--nologo")
		})
	}
}
