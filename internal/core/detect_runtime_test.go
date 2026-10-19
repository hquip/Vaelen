package core

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

func mavenFixture(t *testing.T, version string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Maven & tools")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	name := "mvn"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	bin := filepath.Join(root, "bin", name)
	if err := os.WriteFile(bin, []byte("not a runnable launcher"), 0755); err != nil {
		t.Fatal(err)
	}
	jar, err := os.Create(filepath.Join(root, "lib", "maven-core-"+version+".jar"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(jar)
	f, err := z.Create("META-INF/maven/org.apache.maven/maven-core/pom.properties")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("# Maven\ngroupId=org.apache.maven\nartifactId=maven-core\nversion=" + version + "\n")); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = jar.Close(); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestMavenDetectionDoesNotDependOnJavaOrProject(t *testing.T) {
	bin := mavenFixture(t, "3.9.6")
	t.Setenv("JAVA_HOME", filepath.Join(t.TempDir(), "missing-java"))
	t.Chdir(t.TempDir())
	version, err := probeVersion("maven", bin)
	if err != nil || version != "3.9.6" {
		t.Fatalf("%s %v", version, err)
	}
	for _, path := range []string{bin, filepath.Dir(filepath.Dir(bin))} {
		s, err := InspectToolPath("maven", path)
		if err != nil || s.Version != "3.9.6" {
			t.Fatalf("%+v %v", s, err)
		}
	}
	p, _ := plugin.Get("maven")
	root, err := inferInstallRoot(p, bin)
	if err != nil || binaryIdentity(root) != binaryIdentity(filepath.Dir(filepath.Dir(bin))) {
		t.Fatalf("wrong Maven root: %s %v", root, err)
	}
	t.Setenv("MAVEN_HOME", "")
	t.Setenv("M2_HOME", "")
	t.Setenv("PATH", filepath.Dir(bin))
	found := DetectSystem(map[string][]string{"maven": plugin.DetectCommands()["maven"]}, "")
	if len(found) != 1 || found[0].Version != "3.9.6" {
		t.Fatalf("Maven command mapping failed: %+v", found)
	}
}

func TestSingleToolDetectionNeverIncludesOtherEnvironmentTools(t *testing.T) {
	bin := mavenFixture(t, "3.9.6")
	t.Setenv("MAVEN_HOME", filepath.Dir(filepath.Dir(bin)))
	t.Setenv("PATH", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", t.TempDir())
	}
	for _, tool := range []string{"node", "go", "python"} {
		for _, s := range DetectSystem(map[string][]string{tool: {tool}}, "") {
			if s.Tool != tool {
				t.Fatalf("%s contains %s", tool, s.Tool)
			}
		}
	}
	system := DetectSystem(map[string][]string{"maven": {"mvn"}}, "")
	if len(system) != 1 || system[0].Version != "3.9.6" {
		t.Fatalf("Maven env discovery failed: %+v", system)
	}
}

func TestInspectToolPathRejectsWrongLanguageAndInvalidAlias(t *testing.T) {
	bin := mavenFixture(t, "3.9.6")
	if _, err := InspectToolPath("python", bin); err == nil {
		t.Fatal("Maven accepted as Python")
	}
	in := Installer{Paths: Paths{Root: t.TempDir()}}
	if _, err := in.AdoptPath("maven", bin, "../escape"); err == nil {
		t.Fatal("invalid alias accepted")
	}
	if _, err := os.Stat(in.Paths.Installs()); !os.IsNotExist(err) {
		t.Fatalf("invalid input wrote installation state: %v", err)
	}
	if _, err := in.AdoptPath("maven", bin, "office-maven"); err != nil {
		t.Fatal(err)
	}
	if !isLink(in.Paths.InstallDir("maven", "office-maven")) {
		t.Fatal("manual adoption did not create a link")
	}
	if err := in.Uninstall("maven", "office-maven"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatal("uninstall deleted external Maven")
	}
}

func TestCondaEnvironmentsKeepSeparateIdentitiesAndDependencies(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "main.go")
	code := `package main
import("fmt";"os")
func main(){if os.Getenv("PYTHONHOME")!=""{os.Exit(42)};if len(os.Args)>1&&os.Args[1]=="--version"{fmt.Println("Python 3.12.8");return};fmt.Printf("prefix=%s\n",os.Getenv("CONDA_PREFIX"))}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "fixture")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exe, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	for _, name := range []string{"science", "web"} {
		root := filepath.Join(home, "miniconda3", "envs", name)
		if err := os.MkdirAll(filepath.Join(root, "conda-meta"), 0755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(root, "bin", "python")
		if runtime.GOOS == "windows" {
			bin = filepath.Join(root, "python.exe")
		}
		if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, data, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "dependencies.txt"), []byte(name+" packages"), 0644); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
	}
	t.Setenv("CONDA_PREFIX", roots[0])
	t.Setenv("CONDA_EXE", "")
	t.Setenv("PYTHONHOME", "invalid-inherited-home")
	if err := os.MkdirAll(filepath.Join(home, ".conda"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".conda", "environments.txt"), []byte(strings.Join(roots, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	list := detectCondaPython(map[string][]string{"python": {"python"}})
	if len(list) != 2 || list[0].Version != "3.12.8" || list[0].RegistryVersion() == list[1].RegistryVersion() {
		t.Fatalf("Conda environments collapsed: %+v", list)
	}
	in := Installer{Paths: Paths{Root: t.TempDir()}}
	p, _ := plugin.Get("python")
	for _, s := range list {
		if err := in.Adopt(p, s.RegistryVersion(), s.Path); err != nil {
			t.Fatal(err)
		}
		root := in.Paths.InstallDir("python", s.RegistryVersion())
		if p.ExecEnv(root)["CONDA_PREFIX"] == "" {
			t.Fatal("Conda prefix not preserved through link")
		}
		if err := config.SetIn(in.Paths.GlobalToolVersions(), "python", s.RegistryVersion()); err != nil {
			t.Fatal(err)
		}
		reg := Registry{Paths: in.Paths}
		act := Activator{Paths: in.Paths, Registry: reg, Resolver: Resolver{Paths: in.Paths, Registry: reg}}
		plan, err := act.PlanEnvironment(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		process := exec.Command(findExecutable(p.BinPaths(root), "python"))
		process.Env = buildEnv(plan.Paths, plan.Variables)
		out, err := process.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "prefix="+p.ExecEnv(root)["CONDA_PREFIX"]) {
			t.Fatalf("wrong Conda execution environment: %s %v", out, err)
		}
		if err := in.Uninstall("python", s.RegistryVersion()); err != nil {
			t.Fatal(err)
		}
		original := filepath.Dir(s.Path)
		if runtime.GOOS != "windows" {
			original = filepath.Dir(original)
		}
		if _, err := os.Stat(filepath.Join(original, "dependencies.txt")); err != nil {
			t.Fatal("external dependencies deleted")
		}
	}
	if err := in.Adopt(p, "science", list[0].Path); err != nil {
		t.Fatal(err)
	}
	reg := Registry{Paths: in.Paths}
	res := Resolver{Paths: in.Paths, Registry: reg}
	if r := res.match("python", "latest", "test"); r.Version != "" {
		t.Fatalf("Conda environment implicitly selected: %+v", r)
	}
	if r := res.match("python", "science", "test"); r.Version != "science" {
		t.Fatalf("explicit Conda alias rejected: %+v", r)
	}
}
