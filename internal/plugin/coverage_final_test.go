package plugin

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostGuidesNeverBecomeExecutablePlugins(t *testing.T) {
	seen := map[string]bool{}
	for _, guide := range hostGuides {
		if seen[guide.ID] {
			t.Fatalf("duplicate guide %s", guide.ID)
		}
		seen[guide.ID] = true
		if _, exists := Get(guide.ID); exists {
			t.Fatalf("guide registered as executable tool: %s", guide.ID)
		}
		u, err := url.Parse(guide.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(guide.Steps) == 0 || guide.Limit == "" {
			t.Fatalf("incomplete guide %s", guide.ID)
		}
		rows := Catalog(guide.ID)
		found := false
		for _, row := range rows {
			if row.Name == guide.ID {
				found = true
				if row.Installable || len(row.Commands) != 0 || row.Support.Guide == nil {
					t.Fatalf("guide offers a managed runtime: %+v", row)
				}
			}
		}
		if !found {
			t.Fatalf("guide absent from catalog: %s", guide.ID)
		}
	}
	for _, query := range []string{"VBA", "ABAP", "Verse", "梯形图", "WolframScript", "Viya", "Red/System", "Vale", "Kdb+"} {
		if len(Catalog(query)) == 0 {
			t.Errorf("guide lookup missing %s", query)
		}
	}
	if _, ok := Get("matlab"); !ok {
		t.Fatal("MATLAB SDK adapter missing")
	}
	if _, ok := Get("netlogo"); !ok {
		t.Fatal("NetLogo SDK adapter missing")
	}
}

func TestSDKMetadataReadersValidateSchemaAndDoNotGuessFromOtherFiles(t *testing.T) {
	for _, test := range []struct{ tool, file, body, want string }{
		{"matlab", "VersionInfo.xml", `<MathWorks_version_info><version>25.1.0.123456</version><release>R2025a</release></MathWorks_version_info>`, "25.1.0.123456"},
		{"netlogo", "app/NetLogo.cfg", "[Application]\napp.name=NetLogo\napp.version=7.0.4\n", "7.0.4"},
	} {
		t.Run(test.tool, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, filepath.FromSlash(test.file))
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(test.body), 0600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "bin", test.tool)
			gotRoot, v, err := ReadSDKMetadata(test.tool, bin)
			if err != nil || v != test.want || gotRoot != root {
				t.Fatalf("SDK metadata root=%s version=%s: %v", gotRoot, v, err)
			}
			if err := os.WriteFile(file, []byte("<different><version>99.0</version></different>"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ReadSDKMetadata(test.tool, bin); err == nil {
				t.Fatal("foreign or damaged SDK metadata accepted")
			}
			if err := os.WriteFile(file, []byte(strings.Repeat("a", (1<<20)+1)), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ReadSDKMetadata(test.tool, bin); err == nil {
				t.Fatal("unbounded metadata accepted")
			}
		})
	}
}

func TestAptosUsesCLITagsAndPublisherDigestForExactPlatform(t *testing.T) {
	officialFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "matching-refs") {
			fmt.Fprint(w, `[{"ref":"refs/tags/aptos-cli-v9.6.0"},{"ref":"refs/tags/aptos-cli-v9.5.1"},{"ref":"refs/tags/aptos-cli-v99.0.0-rc.1"},{"ref":"refs/tags/aptos-node-v99.0.0"}]`)
			return
		}
		fmt.Fprint(w, `{"tag_name":"aptos-cli-v9.6.0","assets":[`)
		for i, target := range []string{"Windows-x86_64", "Linux-x86_64", "Linux-aarch64", "macOS-x86_64", "macOS-arm64"} {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			name := "aptos-cli-9.6.0-" + target + ".zip"
			fmt.Fprintf(w, `{"name":%q,"browser_download_url":%q,"size":123,"digest":%q}`, name, "https://github.com/aptos-labs/aptos-core/releases/download/aptos-cli-v9.6.0/"+name, "sha256:"+strings.Repeat("c", 64))
		}
		fmt.Fprint(w, `]}`)
	})
	p, _ := Get("aptos")
	if vs, err := p.ListAll(); err != nil || strings.Join(vs, ",") != "9.6.0,9.5.1" {
		t.Fatalf("CLI versions %v %v", vs, err)
	}
	for _, target := range []Target{{"windows", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		if a, err := p.(ArtifactProvider).Artifact("9.6.0", target); err != nil || len(a.SHA256) != 64 || a.Format != "zip" {
			t.Fatalf("Aptos %v: %+v %v", target, a, err)
		}
	}
	if _, err := p.(ArtifactProvider).Artifact("9.6.0", Target{"windows", "arm64"}); err == nil {
		t.Fatal("Aptos mismatched architecture accepted")
	}
	for _, query := range []string{"Cairo", "Move", "J", "BQN", "Rebol", "Hare", "Hylo", "Eiffel", "Pharo", "Squeak", "MATLAB", "Delphi", "NetLogo"} {
		if len(Catalog(query)) == 0 {
			t.Errorf("managed language missing %s", query)
		}
	}
	for _, name := range []string{"red", "vale", "kona", "ucblogo"} {
		if _, ok := Get(name); !ok || VersionMode(name) != "label" || Support(name, Target{"linux", "amd64"}).Mode != "adopt" {
			t.Errorf("manual version adapter missing %s", name)
		}
	}
}
