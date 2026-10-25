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
	"vsm/internal/plugin"
)

func TestLabeledToolSelectsRealInstallationsWithoutPretendingToProbeVersion(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	code := `package main
import("fmt";"os";"path/filepath")
func main(){if len(os.Args)>1&&(os.Args[1]=="--version"||os.Args[1]=="-version"){os.WriteFile(os.Getenv("VSM_LABEL_PROBE_MARKER"),[]byte("started"),0600);os.Exit(79)};exe,_:=os.Executable();data,_:=os.ReadFile(filepath.Join(filepath.Dir(exe),"identity.txt"));fmt.Print(string(data))}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	compiled := filepath.Join(dir, "runtime")
	if runtime.GOOS == "windows" {
		compiled += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", compiled, source).CombinedOutput(); err != nil {
		t.Fatalf("build fixture %v %s", err, out)
	}
	data, err := os.ReadFile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	paths := Paths{Root: t.TempDir()}
	t.Setenv("VSM_HOME", paths.Root)
	marker := filepath.Join(t.TempDir(), "probe-started")
	t.Setenv("VSM_LABEL_PROBE_MARKER", marker)
	project := t.TempDir()
	reg := Registry{Paths: paths}
	a := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
	for index, label := range []string{"0.6.5", "0.6.6"} {
		sdk := t.TempDir()
		command := "red"
		if runtime.GOOS == "windows" {
			command += ".exe"
		}
		if err := os.WriteFile(filepath.Join(sdk, command), data, 0755); err != nil {
			t.Fatal(err)
		}
		identity := []string{"SDK_A", "SDK_B"}[index]
		if err := os.WriteFile(filepath.Join(sdk, "identity.txt"), []byte(identity), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := (Installer{Paths: paths}).AdoptPath("red", sdk, ""); err == nil {
			t.Fatal("unlabeled installation accepted")
		}
		info, err := (Installer{Paths: paths}).AdoptPath("red", sdk, label)
		if err != nil || info.VersionOrigin != "label" || info.Version != label {
			t.Fatalf("labeled adoption %+v %v", info, err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("manual adoption started an automatic version probe")
		}
		if err := config.SetIn(filepath.Join(project, ".tool-versions"), "red", label); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := a.RunProjectContext(context.Background(), project, "red", nil, nil, &out, &out); err != nil || !strings.Contains(out.String(), identity) {
			t.Fatalf("selected runtime wrong: %v %s", err, out.String())
		}
	}
	if _, ok := plugin.ProbeCommands()["red"]; ok {
		t.Fatal("labeled-only tool appears in automatic scanning")
	}
}

func TestMetadataSDKAdoptionAndProjectSelectionNeverStartTheHost(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "matlab")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Deliberately invalid executable bytes: all operations here must read
	// metadata and inspect paths, without trying to start or license MATLAB.
	if err := os.WriteFile(bin, []byte("MUST_NOT_EXECUTE"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "VersionInfo.xml"), []byte(`<MathWorks_version_info><version>25.1.0.12345</version></MathWorks_version_info>`), 0600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("VSM_HOME", home)
	paths := Paths{Root: home}
	selected, err := (Installer{Paths: paths}).AdoptPath("matlab", root, "")
	if err != nil || selected.Version != "25.1.0.12345" {
		t.Fatalf("metadata adoption %+v %v", selected, err)
	}
	project := t.TempDir()
	if err := config.SetIn(filepath.Join(project, ".tool-versions"), "matlab", selected.Version); err != nil {
		t.Fatal(err)
	}
	reg := Registry{Paths: paths}
	a := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
	exe, err := a.Which("matlab", project)
	if err != nil || !sameCommandFile(exe, bin) {
		t.Fatalf("SDK selection %s %v", exe, err)
	}
	if _, err := os.Stat(filepath.Join(paths.InstallDir("matlab", selected.Version), "VersionInfo.xml")); err != nil {
		t.Fatal("adoption did not keep the full SDK root")
	}
	plan, err := a.PlanProjectEnvironment(project)
	if err != nil || len(plan.Versions) != 1 {
		t.Fatalf("SDK project plan %+v %v", plan, err)
	}
}

func TestJConsoleWithoutEngineLibraryIsNotStarted(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	marker := filepath.Join(dir, "started")
	code := "package main\nimport \"os\"\nfunc main(){os.WriteFile(os.Getenv(\"VSM_J_TEST_MARKER\"),[]byte(\"bad\"),0600)}\n"
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "jconsole")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); err != nil {
		t.Fatalf("build fake jconsole: %v %s", err, out)
	}
	t.Setenv("VSM_J_TEST_MARKER", marker)
	if _, err := probeVersion("j", bin); err == nil {
		t.Fatal("JDK-like jconsole accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("foreign jconsole was started")
	}
	for _, command := range plugin.ProbeCommands()["j"] {
		if command == "jconsole" {
			t.Fatal("ambiguous JDK command included in automatic discovery")
		}
	}
}

func TestDockerDoesNotStartMetadataOrLabeledSDKsToGuessTheirVersion(t *testing.T) {
	for _, tool := range []string{"matlab", "netlogo", "red"} {
		t.Run(tool, func(t *testing.T) {
			dir := t.TempDir()
			project, err := InitProject(dir, "docker", "busybox:1.37.0")
			if err != nil {
				t.Fatal(err)
			}
			if err := config.SetIn(filepath.Join(dir, ".tool-versions"), tool, "1.0"); err != nil {
				t.Fatal(err)
			}
			err = project.RunDocker(context.Background(), dir, "echo", []string{"must-not-run"})
			if err == nil || !strings.Contains(err.Error(), "不能通过 Docker") {
				t.Fatalf("unsupported SDK was probed in Docker: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".vsm", "docker-cache")); !os.IsNotExist(err) {
				t.Fatal("Docker rejection created runtime state")
			}
		})
	}
}

func TestEiffelAdoptionKeepsTheSDKRootAndPlatformEnvironment(t *testing.T) {
	p, _ := plugin.Get("eiffel")
	root := t.TempDir()
	bin := p.BinPaths(root)[0]
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	name := "ec"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(bin, name)
	if err := os.WriteFile(executable, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := inferInstallRoot(p, executable)
	if err != nil || !sameDir(got, root) {
		t.Fatalf("Eiffel SDK root %s %v", got, err)
	}
	env := p.ExecEnv(root)
	if env["ISE_EIFFEL"] != root || env["ISE_PLATFORM"] == "" {
		t.Fatalf("Eiffel environment %+v", env)
	}
}
