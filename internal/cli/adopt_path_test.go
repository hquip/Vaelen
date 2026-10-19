package cli

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIAdoptsNamedPathOutsidePATH(t *testing.T) {
	bin, home, cwd := buildVsm(t), t.TempDir(), t.TempDir()
	root := filepath.Join(t.TempDir(), "Maven outside PATH")
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
	if err := os.WriteFile(filepath.Join(root, "bin", name), []byte("not executable; metadata must be used"), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root, "lib", "maven-core-3.9.6.jar"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	entry, err := z.Create("META-INF/maven/org.apache.maven/maven-core/pom.properties")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte("version=3.9.6\n")); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"adopt", "maven", "--path", root, "--name", "office-maven"}, {"use", "-g", "maven", "office-maven"}} {
		if code, out := runVsm(t, bin, home, cwd, args...); code != 0 {
			t.Fatal(out)
		}
	}
	if code, out := runVsm(t, bin, home, cwd, "which", "maven"); code != 0 || !strings.Contains(out, filepath.Join(home, "installs", "maven", "office-maven", "bin", name)) {
		t.Fatalf("wrong Maven layout: %s", out)
	}
	for _, args := range [][]string{{"adopt", "--all", "extra"}, {"adopt", "maven", "--path"}, {"adopt", "maven", "--bad", "value"}, {"adopt", "maven", "--path", root, "--name", "../escape"}} {
		if code, out := runVsm(t, bin, home, cwd, args...); code == 0 {
			t.Fatalf("invalid args accepted: %v %s", args, out)
		}
	}
	if code, out := runVsm(t, bin, home, cwd, "uninstall", "maven", "office-maven"); code != 0 {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "maven-core-3.9.6.jar")); err != nil {
		t.Fatal("external Maven was deleted")
	}
}
