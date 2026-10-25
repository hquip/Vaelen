package plugin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func officialFixture(t *testing.T, serve http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	previous := officialHTTPClient
	t.Cleanup(func() { officialHTTPClient = previous })
	client := server.Client()
	transport := client.Transport
	client.Transport = metadataTransport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = "http"
		u.Host = strings.TrimPrefix(server.URL, "http://")
		copy.URL = &u
		return transport.RoundTrip(copy)
	})
	officialHTTPClient = client
}

func TestDotnetOfficialSDKUsesPublisherSHA512ForSixTargets(t *testing.T) {
	bad := false
	officialFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "releases-index.json") {
			fmt.Fprint(w, `{"releases-index":[{"channel-version":"11.0","latest-sdk":"11.0.100-preview.1","support-phase":"preview"},{"channel-version":"8.0","latest-sdk":"8.0.408","support-phase":"active"}]}`)
			return
		}
		if r.URL.Path != "/dotnet/release-metadata/8.0/releases.json" {
			t.Errorf("unexpected metadata path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"releases":[{"sdk":{"version":"8.0.408","files":[`)
		for i, rid := range []string{"win-x64", "win-arm64", "linux-x64", "linux-arm64", "osx-x64", "osx-arm64"} {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			ext := "tar.gz"
			if strings.HasPrefix(rid, "win") {
				ext = "zip"
			}
			sum := strings.Repeat("a", 128)
			if bad {
				sum = "invalid"
			}
			fmt.Fprintf(w, `{"rid":%q,"url":%q,"hash":%q}`, rid, "https://builds.dotnet.microsoft.com/dotnet/Sdk/8.0.408/dotnet-sdk-8.0.408-"+rid+"."+ext, sum)
		}
		fmt.Fprint(w, `]}}]}`)
	})
	p, _ := Get("dotnet")
	if v, err := p.LatestStable(); err != nil || v != "8.0.408" {
		t.Fatalf("stable: %s %v", v, err)
	}
	if vs, err := p.ListAll(); err != nil || len(vs) != 1 || vs[0] != "8.0.408" {
		t.Fatalf("SDK versions: %v %v", vs, err)
	}
	for _, osName := range []string{"windows", "linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			a, err := p.(ArtifactProvider).Artifact("8.0.408", Target{osName, arch})
			if err != nil || len(a.SHA512) != 128 || a.SHA256 != "" {
				t.Fatalf("artifact %s/%s: %+v %v", osName, arch, a, err)
			}
		}
	}
	bad = true
	if _, err := p.(ArtifactProvider).Artifact("8.0.408", Target{"windows", "amd64"}); err == nil {
		t.Fatal("invalid publisher hash accepted")
	}
}

func TestOfficialDartJuliaFlutterSelectExactPlatformAndDigest(t *testing.T) {
	officialFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "latest/VERSION"):
			fmt.Fprint(w, `{"version":"3.8.1"}`)
		case strings.HasSuffix(r.URL.Path, ".sha256sum"):
			fmt.Fprintln(w, strings.Repeat("b", 64)+"  sdk.zip")
		case strings.HasSuffix(r.URL.Path, "versions.json"):
			fmt.Fprintf(w, `{"1.11.5":{"stable":true,"files":[{"url":"https://julialang-s3.julialang.org/bin/linux/x64/1.11/julia.tar.gz","os":"linux","arch":"x86_64","extension":"tar.gz","triplet":"x86_64-linux-gnu","sha256":%q,"size":123}]}}`, strings.Repeat("c", 64))
		case strings.HasSuffix(r.URL.Path, "releases_macos.json"):
			fmt.Fprintf(w, `{"releases":[{"version":"3.32.0","channel":"stable","dart_sdk_arch":"arm64","archive":"stable/macos/flutter_macos_arm64_3.32.0-stable.zip","sha256":%q},{"version":"3.33.0","channel":"beta","dart_sdk_arch":"arm64","archive":"beta.zip","sha256":%q}]}`, strings.Repeat("d", 64), strings.Repeat("e", 64))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	for _, test := range []struct{ name, version, os, arch, suffix string }{{"dart", "3.8.1", "windows", "amd64", "dartsdk-windows-x64-release.zip"}, {"julia", "1.11.5", "linux", "amd64", "julia.tar.gz"}, {"flutter", "3.32.0", "darwin", "arm64", "flutter_macos_arm64_3.32.0-stable.zip"}} {
		p, _ := Get(test.name)
		a, err := p.(ArtifactProvider).Artifact(test.version, Target{test.os, test.arch})
		if err != nil || !strings.HasSuffix(a.URL, test.suffix) || len(a.SHA256) != 64 {
			t.Fatalf("%s artifact %+v %v", test.name, a, err)
		}
	}
	p, _ := Get("flutter")
	if _, err := p.(ArtifactProvider).Artifact("3.32.0", Target{"darwin", "amd64"}); err == nil {
		t.Fatal("arm64 archive selected for x64")
	}
}

func TestLanguageCatalogContainsRequestedFamiliesAndRealCommands(t *testing.T) {
	for query, want := range map[string]string{"C#": "dotnet", "VB.NET": "dotnet", "C++": "gcc", "Objective-C": "clang", "Fortran": "gfortran", "Ada": "gnat", "COBOL": "cobol", "PowerShell": "powershell", "Assembly": "nasm", "VHDL": "ghdl", "TypeScript": "typescript"} {
		found := false
		for _, row := range Catalog(query) {
			if row.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s search missing %s", query, want)
		}
	}
	commands := DetectCommands()
	for tool, want := range map[string]string{"gcc": "gcc", "clang": "clang", "msvc": "cl", "dotnet": "dotnet", "purescript": "purs", "solidity": "solc", "erlang": "erl", "ldc": "ldc2"} {
		found := false
		for _, cmd := range commands[tool] {
			if cmd == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing command %s", tool, want)
		}
	}
	if s := Support("msvc", Target{"linux", "amd64"}); s.Mode != "unavailable" {
		t.Fatal("MSVC reported usable on Linux")
	}
	if s := Support("gcc", Target{"windows", "amd64"}); s.Mode != "adopt" {
		t.Fatal("source-only GCC reported downloadable")
	}
}
