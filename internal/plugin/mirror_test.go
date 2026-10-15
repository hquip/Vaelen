package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCustomMirrorsArePerSourceAndOffOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("VSM_HOME", home)
	t.Setenv("VSM_MIRROR", "custom")
	if err := os.WriteFile(filepath.Join(home, "mirror-sources.json"), []byte(`{"github":"https://mirror.example","github-api":"https://api.example/","node":"https://node.example/dist","npm":"https://npm.example/"}`), 0644); err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string]string{
		"https://github.com/a/b/releases/download/v/file":      "https://mirror.example/https://github.com/a/b/releases/download/v/file",
		"https://api.github.com/repos/a/b/releases?per_page=5": "https://api.example/repos/a/b/releases?per_page=5",
		"https://nodejs.org/dist/index.json":                   "https://node.example/dist/index.json",
		"https://registry.npmjs.org/pkg":                       "https://npm.example/pkg",
	} {
		if got := MirrorURL(input); got != want {
			t.Fatalf("%s -> %s, want %s", input, got, want)
		}
	}
	t.Setenv("VSM_MIRROR", "off")
	input := "https://github.com/a/b/file"
	if MirrorURL(input) != input {
		t.Fatal("off did not disable custom mirror")
	}
	for _, bad := range []string{"http://remote.example/", "https://user:pass@mirror.example", "https://mirror.example/?token=x"} {
		if ValidateMirror("github", bad) == nil {
			t.Fatalf("invalid mirror accepted: %s", bad)
		}
	}
}

func TestMirrorURLCN(t *testing.T) {
	t.Setenv("VSM_MIRROR", "cn")
	cases := map[string]string{
		"https://go.dev/dl/go1.22.0.windows-amd64.zip":              "https://golang.google.cn/dl/go1.22.0.windows-amd64.zip",
		"https://nodejs.org/dist/index.json":                        "https://npmmirror.com/mirrors/node/index.json",
		"https://github.com/astral-sh/x/releases/download/a.tar.gz": "https://ghproxy.com/https://github.com/astral-sh/x/releases/download/a.tar.gz",
		"https://api.github.com/repos/x/y/releases":                 "https://api.github.com/repos/x/y/releases", // 不重写
	}
	for in, want := range cases {
		if got := MirrorURL(in); got != want {
			t.Errorf("MirrorURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMirrorURLOff(t *testing.T) {
	t.Setenv("VSM_MIRROR", "off")
	in := "https://go.dev/dl/go1.22.0.linux-amd64.tar.gz"
	if got := MirrorURL(in); got != in {
		t.Errorf("off 不应重写，got %q", got)
	}
}
