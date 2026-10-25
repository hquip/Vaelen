package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"vsm/internal/config"
	"vsm/internal/plugin"
)

// Redirect only fixture downloads; retain the real plugin's archive layout and
// model a publisher's missing hash without bypassing the lockfile policy.
type compilerFixtureProvider struct {
	plugin.Plugin
	artifact plugin.Artifact
}

func (p compilerFixtureProvider) Artifact(string, plugin.Target) (plugin.Artifact, error) {
	return p.artifact, nil
}

// Opt-in real publisher packages, downloaded separately so ordinary unit tests
// stay offline. All installations, caches and source files belong to TempDir.
func TestRealAdditionalLanguages(t *testing.T) {
	fixture := os.Getenv("VSM_TEST_LANGUAGE_ARTIFACTS")
	if fixture == "" {
		t.Skip("set VSM_TEST_LANGUAGE_ARTIFACTS to the publisher fixture manifest")
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var packages []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Archive string `json:"archive"`
		SHA256  string `json:"sha256"`
		Size    int64  `json:"size"`
		Format  string `json:"format"`
		Adopt   bool   `json:"adopt"`
	}
	if err := json.Unmarshal(data, &packages); err != nil {
		t.Fatal(err)
	}
	if len(packages) == 0 {
		t.Fatal("no real compiler packages supplied")
	}
	for _, pkg := range packages {
		t.Run(pkg.Name, func(t *testing.T) {
			home, project := t.TempDir(), t.TempDir()
			t.Setenv("VSM_HOME", home)
			t.Setenv("VSM_PROJECT_BOUNDARY", "")
			t.Setenv("VSM_MIRROR", "off")
			t.Setenv("VSM_REQUIRE_CHECKSUM", "0")
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			if pkg.SHA256 == "" && pkg.Name != "haxe" {
				t.Fatal("fixture requires a publisher checksum")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, pkg.Archive) }))
			t.Cleanup(server.Close)
			p, ok := plugin.Get(pkg.Name)
			if !ok {
				t.Fatal("unknown fixture tool")
			}
			artifact := plugin.Artifact{URL: server.URL + "/publisher." + pkg.Format, SHA256: pkg.SHA256, Size: pkg.Size, Format: pkg.Format}
			paths := Paths{Root: home}
			if pkg.Adopt {
				data, err := os.ReadFile(pkg.Archive)
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(data)
				if hex.EncodeToString(hash[:]) != pkg.SHA256 {
					t.Fatal("adoption fixture publisher checksum mismatch")
				}
				sdk := t.TempDir()
				if pkg.Format == "zip" {
					if err := Extract(pkg.Archive, sdk); err != nil {
						t.Fatal(err)
					}
					sdk = stripSingleDir(sdk)
				} else {
					if pkg.Format == "gzip" {
						r, err := gzip.NewReader(bytes.NewReader(data))
						if err != nil {
							t.Fatal(err)
						}
						data, err = io.ReadAll(io.LimitReader(r, (64<<20)+1))
						r.Close()
						if err != nil || len(data) > 64<<20 {
							t.Fatal("invalid adoption fixture gzip")
						}
					}
					command := plugin.DetectCommands()[pkg.Name][0]
					if runtime.GOOS == "windows" {
						command += ".exe"
					}
					if err := os.WriteFile(filepath.Join(sdk, command), data, 0755); err != nil {
						t.Fatal(err)
					}
				}
				selected, err := (Installer{Paths: paths}).AdoptPath(pkg.Name, sdk, "")
				if err != nil {
					t.Fatal(err)
				}
				if pkg.Version != "" && pkg.Version != selected.Version {
					t.Fatalf("adopted version %s, want %s", selected.Version, pkg.Version)
				}
				pkg.Version = selected.Version
			} else {
				installer := Installer{Paths: paths, Pinned: &artifact, RequireChecksum: true}
				if pkg.SHA256 == "" {
					p = compilerFixtureProvider{Plugin: p, artifact: artifact}
					installer.Pinned = nil
					if err := installer.Install(p, pkg.Version, "", nil); err == nil {
						t.Fatal("strict install accepted a publisher without a hash")
					}
					installer.RequireChecksum = false
				}
				if err := installer.Install(p, pkg.Version, "", func(message string) { t.Log(message) }); err != nil {
					t.Fatal(err)
				}
			}
			bin := ""
			for _, name := range plugin.DetectCommands()[pkg.Name] {
				bin = findExecutable(p.BinPaths(paths.InstallDir(pkg.Name, pkg.Version)), name)
				if bin != "" {
					break
				}
			}
			if bin == "" {
				t.Fatal("installed compiler command missing")
			}
			version, err := probeVersion(pkg.Name, bin)
			if err != nil || version != pkg.Version {
				t.Fatalf("installed compiler version: %q, want %s: %v", version, pkg.Version, err)
			}
			if err := config.SetIn(filepath.Join(project, ".tool-versions"), pkg.Name, pkg.Version); err != nil {
				t.Fatal(err)
			}
			reg := Registry{Paths: paths}
			act := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
			var file, source, want string
			var args []string
			var input io.Reader
			switch pkg.Name {
			case "godot":
				file, source, want = "hello.gd", "extends SceneTree\nfunc _init():\n\tprint(\"GDSCRIPT_OK\")\n\tquit(0)\n", "GDSCRIPT_OK"
				if err := os.WriteFile(filepath.Join(project, "project.godot"), []byte("config_version=5\n[application]\nconfig/name=\"VSM isolated test\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				args = []string{"--headless", "--path", project, "--log-file", filepath.Join(project, "godot.log"), "--script", filepath.Join(project, file)}
			case "vyper":
				file, source, want = "answer.vy", "# pragma version "+pkg.Version+"\n@external\n@pure\ndef answer() -> uint256:\n    return 42\n", "0x"
				args = []string{filepath.Join(project, file)}
			case "arturo":
				file, source, want = "hello.art", "print \"ARTURO_OK\"\n", "ARTURO_OK"
				args = []string{filepath.Join(project, file)}
			case "factor":
				file, source, want = "hello.factor", "USING: io ;\n\"FACTOR_OK\" print\n", "FACTOR_OK"
				args = []string{"-no-user-init", filepath.Join(project, file)}
			case "haxe":
				file, source, want = "Main.hx", "class Main { static function main() { Sys.println(\"HAXE_OK\"); } }\n", "HAXE_OK"
				args = []string{"-cp", project, "-main", "Main", "--interp"}
			case "nim":
				file, source, want = "hello.nim", "echo \"NIM_OK\"\n", "NIM_OK"
				out := filepath.Join(project, "hello")
				cc := "gcc"
				if runtime.GOOS == "darwin" {
					cc = "clang"
				}
				if _, err := exec.LookPath(cc); err != nil {
					t.Fatalf("Nim native backend compiler required: %s", cc)
				}
				if runtime.GOOS == "windows" {
					out += ".exe"
				}
				args = []string{"c", "-r", "--hints:off", "--cc:" + cc, "--nimcache:" + filepath.Join(project, "nimcache"), "--out:" + out, filepath.Join(project, file)}
			case "scarb":
				file, source, want = "src/lib.cairo", "pub fn answer() -> u32 { 42 }\n", "Finished"
				if err := os.WriteFile(filepath.Join(project, "Scarb.toml"), []byte("[package]\nname=\"vsm_hello\"\nversion=\"0.1.0\"\nedition=\"2024_07\"\n[lib]\n"), 0600); err != nil {
					t.Fatal(err)
				}
				args = []string{"--offline", "build"}
			case "aptos":
				file, source, want = "sources/hello.move", "module vsm::hello { public fun answer(): u64 { 42 } }\n", "hello"
				if err := os.WriteFile(filepath.Join(project, "Move.toml"), []byte("[package]\nname=\"vsm_hello\"\nversion=\"0.0.0\"\n[addresses]\nvsm=\"0x42\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("APTOS_DISABLE_TELEMETRY", "true")
				args = []string{"move", "compile", "--package-dir", project, "--skip-fetch-latest-git-deps"}
			case "j":
				file, source, want = "hello.ijs", "1!:2&2 'J_OK'\n2!:55''\n", "J_OK"
				args = []string{"-jprofile"}
				input = strings.NewReader(source)
			case "rebol":
				t.Setenv("REBOL_HOME", t.TempDir())
				file, source, want = "hello.reb", "Rebol []\nprint \"REBOL_OK\"\nquit\n", "REBOL_OK"
				args = []string{"-w", "-q", filepath.Join(project, file)}
			default:
				t.Fatal("unexpected language fixture")
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(project, file)), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, file), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			var log bytes.Buffer
			if err := act.RunProjectContext(ctx, project, filepath.Base(bin), args, input, &log, &log); err != nil {
				t.Fatalf("run project: %v\n%s", err, log.String())
			}
			if !strings.Contains(log.String(), want) {
				t.Fatalf("expected %s output, got: %s", want, log.String())
			}
			if pkg.Name == "aptos" && !fileExists(filepath.Join(project, "build", "vsm_hello", "bytecode_modules", "hello.mv")) {
				t.Fatal("Move compiler did not produce the expected module")
			}
			if pkg.Name == "vyper" && !regexp.MustCompile(`(?m)^0x[0-9a-f]+\r?$`).MatchString(log.String()) {
				t.Fatalf("Vyper did not produce bytecode: %s", log.String())
			}
			if pkg.Name == "godot" && runtime.GOOS == "darwin" {
				bundle := filepath.Join(paths.InstallDir(pkg.Name, pkg.Version), "Godot.app")
				if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", bundle).CombinedOutput(); err != nil {
					t.Fatalf("Godot bundle signature changed: %v %s", err, out)
				}
			}
		})
	}
}

func TestRealAdditionalLocalInterpreters(t *testing.T) {
	for _, test := range []struct{ tool, variable, file, source, want string }{
		{"gforth", "VSM_TEST_FORTH", "hello.fs", ".\" FORTH_OK\" cr bye\n", "FORTH_OK"},
		{"regina", "VSM_TEST_REXX", "hello.rexx", "say 'REXX_OK'\n", "REXX_OK"},
		{"gnu-apl", "VSM_TEST_APL", "hello.apl", "⎕←'APL_OK'\n)OFF\n", "APL_OK"},
		{"bqn", "VSM_TEST_BQN", "hello.bqn", "•Out \"BQN_OK\"\n", "BQN_OK"},
		{"hare", "VSM_TEST_HARE", "hello.ha", "use fmt;\nexport fn main() void = { fmt::println(\"HARE_OK\")!; };\n", "HARE_OK"},
	} {
		t.Run(test.tool, func(t *testing.T) {
			requested := os.Getenv(test.variable)
			if requested == "" {
				t.Skip("set " + test.variable + " to the installed interpreter")
			}
			bin, err := exec.LookPath(requested)
			if err != nil {
				t.Fatal(err)
			}
			bin, err = filepath.Abs(bin)
			if err != nil {
				t.Fatal(err)
			}
			home, project := t.TempDir(), t.TempDir()
			t.Setenv("VSM_HOME", home)
			t.Setenv("VSM_PROJECT_BOUNDARY", "")
			paths := Paths{Root: home}
			selected, err := (Installer{Paths: paths}).AdoptPath(test.tool, bin, "")
			if err != nil {
				t.Fatal(err)
			}
			if selected.Version == "" {
				t.Fatal("interpreter version missing")
			}
			if err := config.SetIn(filepath.Join(project, ".tool-versions"), test.tool, selected.Version); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(project, test.file)
			if err := os.WriteFile(file, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			reg := Registry{Paths: paths}
			act := Activator{Paths: paths, Registry: reg, Resolver: Resolver{Paths: paths, Registry: reg}}
			args := []string{file}
			if test.tool == "gnu-apl" {
				args = []string{"--script", "-f", file}
			}
			if test.tool == "hare" {
				args = []string{"run", file}
			}
			var log bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := act.RunProjectContext(ctx, project, filepath.Base(bin), args, nil, &log, &log); err != nil {
				t.Fatalf("run interpreter: %v %s", err, log.String())
			}
			if !strings.Contains(log.String(), test.want) {
				t.Fatalf("expected %s, got %s", test.want, log.String())
			}
		})
	}
}
