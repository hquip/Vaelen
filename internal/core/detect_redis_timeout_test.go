package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRedisDiscoveryKeepsCompleteVersionBeforeExitTimeout(t *testing.T) {
	t.Setenv("VSM_HOME", t.TempDir())
	root := t.TempDir()
	source := filepath.Join(root, "redis.go")
	code := `package main
import("fmt";"os";"time")
func main(){
 if len(os.Args)!=2||(os.Args[1]!="--version"&&os.Args[1]!="-v"){os.Exit(90)}
 fmt.Println("Redis server v=5.0.14.1 sha=ec77f72d:0 malloc=jemalloc-5.2.1-redis bits=64 build=5627b8177c9289c")
 if os.Getenv("VSM_TEST_REDIS_NONZERO")=="1"{os.Exit(9)}
 time.Sleep(time.Minute)
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
	t.Run("discovery", func(t *testing.T) {
		version, err := probeVersion("redis", bin)
		if err != nil || version != "5.0.14.1" {
			t.Fatalf("discarded valid Redis banner before exit timeout: version=%q err=%v", version, err)
		}
	})
	t.Run("manual_path_inspection", func(t *testing.T) {
		version, err := InspectToolPath("redis", bin)
		if err != nil || version.Version != "5.0.14.1" || version.VersionError != "" {
			t.Fatalf("path inspection discarded valid banner: %+v %v", version, err)
		}
	})
	t.Run("execution_verification_stays_strict", func(t *testing.T) {
		version, err := probeVersionCommand("redis", bin, nil, "")
		if err == nil || version != "" || !strings.Contains(err.Error(), "超时") {
			t.Fatalf("execution timeout was treated as success: version=%q err=%v", version, err)
		}
	})
	t.Run("nonzero_exit_still_fails", func(t *testing.T) {
		t.Setenv("VSM_TEST_REDIS_NONZERO", "1")
		version, err := probeVersion("redis", bin)
		if err == nil || version != "" {
			t.Fatalf("explicit Redis failure accepted: version=%q err=%v", version, err)
		}
	})
}

func TestRedisTimeoutRecoveryNeedsCompleteServerBanner(t *testing.T) {
	banner := "Redis server v=5.0.14.1 sha=ec77f72d:0 malloc=jemalloc-5.2.1-redis bits=64 build=5627b8177c9289c"
	for _, tc := range []struct{ output, want string }{
		{banner + "\r\n", "5.0.14.1"}, {"\x1b[32m" + banner + "\x1b[0m\n", "5.0.14.1"},
		{banner, ""}, {"Redis server v=5.0.", ""}, {"Redis server v=5.0.14.1\n", ""},
		{"jemalloc-5.2.1 on Windows 10.0.26100\n", ""}, {"redis-cli 5.0.14.1\n", ""},
		{"System error: " + banner + "\n", ""},
	} {
		if got := completeRedisVersionBanner(tc.output); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.output, got, tc.want)
		}
	}
}
