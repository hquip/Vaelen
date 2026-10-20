package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRedisProbeUsesOnlyVersionFlagsAndKeepsFailureDetails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VSM_HOME", filepath.Join(root, "vsm"))
	source := filepath.Join(root, "probe.go")
	code := `package main
import("fmt";"os")
func main(){
 if len(os.Args)!=2 || (os.Args[1]!="--version" && os.Args[1]!="-v") {os.WriteFile(os.Getenv("VSM_REDIS_UNSAFE"),[]byte("unsafe"),0600);os.Exit(90)}
 mode:=os.Getenv("VSM_REDIS_MODE")
 if mode=="failed" {fmt.Fprintln(os.Stderr,os.Args[1]+" startup failure");os.Exit(1)}
 if mode=="unrelated" {fmt.Println("jemalloc-5.2.1 on Windows 10.0.26100");return}
 if mode=="fallback" && os.Args[1]=="--version" {fmt.Fprintln(os.Stderr,"unknown argument: version");os.Exit(1)}
 if mode=="stderr" {fmt.Fprintln(os.Stderr,"Redis server v=5.0.14.1 sha=abc malloc=jemalloc-5.2.1 bits=64");return}
 fmt.Println("Redis server v=3.0.504 sha=0 malloc=jemalloc-3.6.0 bits=64")
}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "redis-server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	marker := filepath.Join(root, "unsafe")
	t.Setenv("VSM_REDIS_UNSAFE", marker)
	for _, tc := range []struct{ mode, want string }{
		{"normal", "3.0.504"}, {"fallback", "3.0.504"}, {"stderr", "5.0.14.1"},
		{"failed", ""}, {"unrelated", ""},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("VSM_REDIS_MODE", tc.mode)
			version, err := probeVersion("redis", bin)
			if version != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("version=%q err=%v", version, err)
			}
			if tc.want == "" && (!strings.Contains(err.Error(), "--version") || !strings.Contains(err.Error(), "-v")) {
				t.Fatalf("probe lost an attempt's failure: %v", err)
			}
			if tc.mode == "failed" && !strings.Contains(err.Error(), "startup failure") {
				t.Fatalf("probe lost the server's diagnostic: %v", err)
			}
		})
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("probe tried an argument that could start/configure the server")
	}
}

// CI supplies real portable Windows ports. No server is started, no database
// is initialized, and nothing is installed into the user's environment.
func TestRedisPortableDetection(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	for _, tc := range []struct{ env, version string }{
		{"VSM_TEST_REDIS_3", "3.0.504"}, {"VSM_TEST_REDIS_5", "5.0.14.1"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			bin := os.Getenv(tc.env)
			if bin == "" {
				t.Skip(tc.env + " not configured")
			}
			for _, path := range []string{bin, filepath.Dir(bin)} {
				v, err := InspectToolPath("redis", path)
				if err != nil || v.Version != tc.version {
					t.Fatalf("%s: %+v %v", path, v, err)
				}
			}
			t.Setenv("PATH", filepath.Dir(bin))
			found := DetectSystem(map[string][]string{"redis": {"redis-server"}}, "")
			if len(found) != 1 || found[0].Version != tc.version || found[0].VersionError != "" {
				t.Fatalf("PATH detection: %+v", found)
			}
		})
	}
}
