package plugin

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVyperExecutableFormatRejectsWrongArchitectureBeforeExecution(t *testing.T) {
	for _, target := range []Target{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		t.Run(target.OS+"/"+target.Arch, func(t *testing.T) {
			data := make([]byte, 128)
			switch target.OS {
			case "windows":
				copy(data, "MZ")
				binary.LittleEndian.PutUint32(data[60:64], 64)
				copy(data[64:], "PE\x00\x00")
				machine := uint16(0x8664)
				if target.Arch == "arm64" {
					machine = 0xaa64
				}
				binary.LittleEndian.PutUint16(data[68:70], machine)
			case "linux":
				copy(data, "\x7fELF")
				data[4], data[5] = 2, 1
				machine := uint16(62)
				if target.Arch == "arm64" {
					machine = 183
				}
				binary.LittleEndian.PutUint16(data[18:20], machine)
			case "darwin":
				binary.LittleEndian.PutUint32(data[:4], 0xfeedfacf)
				machine := uint32(0x1000007)
				if target.Arch == "arm64" {
					machine = 0x100000c
				}
				binary.LittleEndian.PutUint32(data[4:8], machine)
			}
			file := filepath.Join(t.TempDir(), "binary")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateVyperExecutable(file, target); err != nil {
				t.Fatal(err)
			}
			wrong := target
			wrong.Arch = "arm64"
			if target.Arch == "arm64" {
				wrong.Arch = "amd64"
			}
			if err := validateVyperExecutable(file, wrong); err == nil {
				t.Fatal("wrong binary architecture accepted")
			}
			if err := os.WriteFile(file, []byte("#!/bin/sh\necho fake\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateVyperExecutable(file, target); err == nil {
				t.Fatal("non-native publisher executable accepted")
			}
		})
	}
}

func TestGodotAndVyperPinExactPublisherAssets(t *testing.T) {
	bad := ""
	officialFixture(t, func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		repo, tag := parts[1]+"/"+parts[2], parts[len(parts)-1]
		names := []string{"Godot_v4.7.2-stable_win64.zip", "Godot_v4.7.2-stable_windows_arm64.zip", "Godot_v4.7.2-stable_linux.x86_64.zip", "Godot_v4.7.2-stable_linux.arm64.zip", "Godot_v4.7.2-stable_macos.universal.zip", "Godot_v4.7.2-stable_mono_win64.zip", "godot-4.7.2-stable.tar.xz"}
		if repo == "vyperlang/vyper" {
			names = []string{"vyper.0.4.3+commit.bff19ea2.windows.exe", "vyper.0.4.3+commit.bff19ea2.linux", "vyper.0.4.3+commit.bff19ea2.darwin"}
		}
		if bad == "duplicate" {
			names = append(names, names[0])
		}
		gotTag := tag
		if bad == "tag" {
			gotTag = "another-version"
		}
		fmt.Fprintf(w, `{"tag_name":%q,"draft":%t,"prerelease":%t,"assets":[`, gotTag, bad == "draft", bad == "prerelease")
		for i, name := range names {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			digest := "sha256:" + strings.Repeat("a", 64)
			if bad == "digest" {
				digest = ""
			}
			assetURL := "https://github.com/" + repo + "/releases/download/" + tag + "/" + name
			if bad == "url" {
				assetURL = "https://example.com/" + name
			}
			fmt.Fprintf(w, `{"name":%q,"browser_download_url":%q,"size":123,"digest":%q}`, name, assetURL, digest)
		}
		fmt.Fprint(w, `]} `)
	})
	for _, osName := range []string{"windows", "linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			p, _ := Get("godot")
			a, err := p.(ArtifactProvider).Artifact("4.7.2", Target{osName, arch})
			if err != nil || a.Format != "zip" || len(a.SHA256) != 64 || strings.Contains(a.URL, "mono") {
				t.Fatalf("Godot %s/%s: %+v %v", osName, arch, a, err)
			}
			v, _ := Get("vyper")
			a, err = v.(ArtifactProvider).Artifact("0.4.3", Target{osName, arch})
			if arch == "arm64" {
				if err == nil {
					t.Fatal("Vyper amd64 package used for ARM")
				}
				continue
			}
			if err != nil || a.Format != "binary" || len(a.SHA256) != 64 {
				t.Fatalf("Vyper %s: %+v %v", osName, a, err)
			}
		}
	}
	for _, mode := range []string{"digest", "url", "tag", "draft", "prerelease", "duplicate"} {
		bad = mode
		p, _ := Get("godot")
		if _, err := p.(ArtifactProvider).Artifact("4.7.2", Target{"windows", "amd64"}); err == nil {
			t.Fatalf("invalid publisher metadata accepted: %s", mode)
		}
	}
}

func TestNimUsesOfficialStableTableAndExactPlatformSHA256(t *testing.T) {
	bad := false
	officialFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/install.html" {
			for _, name := range []string{"nim-2.2.12-windows_x64.zip", "nim-2.2.12-linux_x64.tar.xz", "nim-2.2.12-linux_arm64.tar.xz", "nim-2.2.12-macosx_x64.tar.xz", "nim-2.2.12-macosx_arm64.tar.xz", "nim-2.3.1-linux_x64.tar.xz"} {
				fmt.Fprintf(w, `<a href="https://github.com/nim-lang/nightlies/releases/download/stable-tag/%s">bin</a>`, name)
			}
			fmt.Fprint(w, `<a href="https://evil.test/nim-99.0.0-linux_x64.tar.xz">wrong publisher</a><a href="https://github.com/someone/else/releases/download/v1/nim-99.0.0-linux_x64.tar.xz">wrong repo</a>`)
			return
		}
		sum := strings.Repeat("c", 64)
		if bad {
			sum = "invalid"
		}
		parts := strings.Split(r.URL.Path, "/")
		fmt.Fprintln(w, sum+"  "+strings.TrimSuffix(parts[len(parts)-1], ".sha256"))
	})
	p, _ := Get("nim")
	if vs, err := p.ListAll(); err != nil || len(vs) != 1 || vs[0] != "2.2.12" {
		t.Fatalf("stable table %v %v", vs, err)
	}
	for _, target := range []Target{{"windows", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		a, err := p.(ArtifactProvider).Artifact("2.2.12", target)
		if err != nil || len(a.SHA256) != 64 {
			t.Fatalf("Nim %v: %+v %v", target, a, err)
		}
	}
	for _, target := range []Target{{"windows", "arm64"}, {"linux", "386"}} {
		if _, err := p.(ArtifactProvider).Artifact("2.2.12", target); err == nil {
			t.Fatalf("wrong architecture accepted %v", target)
		}
	}
	bad = true
	if _, err := p.(ArtifactProvider).Artifact("2.2.12", Target{"linux", "amd64"}); err == nil {
		t.Fatal("invalid SHA256 accepted")
	}
}

func TestGodotArchiveLayoutUsesStableCommandsAndPreservesBundle(t *testing.T) {
	p, _ := Get("godot")
	prepare := p.(InstallPreparer)
	for _, target := range []Target{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}} {
		t.Run(target.OS+"/"+target.Arch, func(t *testing.T) {
			if target.OS == "darwin" && runtime.GOOS == "windows" {
				t.Skip("macOS bundle symlinks are verified on Unix")
			}
			root := t.TempDir()
			platform := map[string]string{"windows/amd64": "win64", "windows/arm64": "windows_arm64", "linux/amd64": "linux.x86_64", "linux/arm64": "linux.arm64"}[target.OS+"/"+target.Arch]
			files := []string{"Godot_v4.7.2-stable_" + platform}
			if target.OS == "windows" {
				files = []string{files[0] + ".exe", files[0] + "_console.exe"}
			}
			if target.OS == "darwin" {
				files = []string{"Godot.app/Contents/MacOS/Godot", "Godot.app/Contents/_CodeSignature/CodeResources"}
			}
			for _, name := range files {
				file := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(name), 0755); err != nil {
					t.Fatal(err)
				}
			}
			result, err := prepare.PrepareInstall(root, "4.7.2", target)
			if err != nil || result != root {
				t.Fatalf("prepare %s %v", result, err)
			}
			command := "godot"
			if target.OS == "windows" {
				command += ".exe"
			}
			if _, err := os.Stat(filepath.Join(root, command)); err != nil {
				t.Fatal(err)
			}
			if target.OS == "darwin" {
				data, err := os.ReadFile(filepath.Join(root, files[1]))
				if err != nil || string(data) != files[1] {
					t.Fatal("bundle signature resources changed")
				}
			}
		})
	}
	root := t.TempDir()
	if _, err := prepare.PrepareInstall(root, "../bad", Target{"linux", "amd64"}); err == nil {
		t.Fatal("unsafe version accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "Godot_v4.7.2-stable_linux.x86_64"), []byte("source"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "godot"), []byte("preserve"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare.PrepareInstall(root, "4.7.2", Target{"linux", "amd64"}); err == nil {
		t.Fatal("existing command overwritten")
	}
}

func TestExtendedLanguagesExposeRealCapabilitiesAndCommands(t *testing.T) {
	for query, want := range map[string]string{"GDScript": "godot", "Vyper": "vyper", "Arturo": "arturo", "Smalltalk": "smalltalk", "Forth": "gforth", "REXX": "regina", "APL": "gnu-apl", "Ballerina": "ballerina", "Factor": "factor"} {
		found := false
		for _, row := range Catalog(query) {
			if row.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing %s", query, want)
		}
	}
	if _, ok := Get("nim"); !ok {
		t.Fatal("Nim missing")
	}
	for _, name := range []string{"haxe", "factor", "arturo", "nim", "godot", "vyper"} {
		if s := Support(name, Target{"windows", "amd64"}); s.Mode != "install" {
			t.Errorf("%s not installable: %+v", name, s)
		}
	}
	for _, name := range []string{"vyper", "factor", "haxe", "nim"} {
		if s := Support(name, Target{"windows", "arm64"}); s.Mode != "adopt" {
			t.Errorf("%s wrongly offers ARM archive: %+v", name, s)
		}
	}
	for _, name := range []string{"smalltalk", "gforth", "regina", "ballerina"} {
		if s := Support(name, Target{"linux", "amd64"}); s.Mode != "adopt" {
			t.Errorf("%s overstates capability", name)
		}
	}
}
