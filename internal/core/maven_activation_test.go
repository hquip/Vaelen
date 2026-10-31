package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

func TestMavenRuntimeEnvironmentUsesSelectedJava(t *testing.T) {
	root := Paths{Root: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	javaDir := root.InstallDir("java", "17.0.14")
	mavenDir := root.InstallDir("maven", "3.9.9")
	javaBin := filepath.Join(javaDir, "bin")
	mavenBin := filepath.Join(mavenDir, "bin")
	if err := os.MkdirAll(javaBin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mavenBin, 0755); err != nil {
		t.Fatal(err)
	}
	javaName, mavenName := "java", "mvn"
	if runtime.GOOS == "windows" {
		javaName += ".exe"
		mavenName += ".cmd"
	}
	for _, file := range []string{filepath.Join(javaBin, javaName), filepath.Join(mavenBin, mavenName)} {
		if err := os.WriteFile(file, []byte("fixture"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetIn(root.GlobalToolVersions(), "java", "17.0.14"); err != nil {
		t.Fatal(err)
	}
	reg := Registry{Paths: root}
	resolver := Resolver{Paths: root, Registry: reg}
	act := Activator{Paths: root, Registry: reg, Resolver: resolver}
	maven, _ := plugin.Get("maven")
	dirs, env, err := act.runtimeEnvironment("maven", t.TempDir(), mavenDir, maven, maven.BinPaths(mavenDir))
	if err != nil {
		t.Fatal(err)
	}
	if env["JAVA_HOME"] != javaDir {
		t.Fatalf("Maven did not receive selected JAVA_HOME: %+v", env)
	}
	if len(dirs) < 2 || !sameDir(dirs[0], javaBin) || !sameDir(dirs[len(dirs)-1], mavenBin) {
		t.Fatalf("Maven PATH does not put selected Java first: %v", dirs)
	}
}

func TestNormalizeProbeTextDecodesWindowsUTF16(t *testing.T) {
	utf16LE := []byte{'b', 0, 'a', 0, 's', 0, 'h', 0, ' ', 0, 'v', 0, '1', 0, '\r', 0, '\n', 0}
	if got := normalizeProbeText(string(utf16LE)); got != "bash v1\r\n" {
		t.Fatalf("UTF-16 probe output was not decoded: %q", got)
	}
	if got := normalizeProbeText("plain output\n"); got != "plain output\n" {
		t.Fatalf("plain probe output changed: %q", got)
	}
}
