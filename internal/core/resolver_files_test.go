package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlainVer(t *testing.T) {
	cases := map[string]string{
		"v18.17.0\n":          "18.17.0",
		"20\n":                "20",
		"# comment\n3.12.1\n": "3.12.1",
		"lts/*\n":             "",
		"  v16  \n":           "16",
	}
	for in, want := range cases {
		if got := plainVer(in); got != want {
			t.Errorf("plainVer(%q)=%q want %q", in, got, want)
		}
	}
}

func TestGoModVer(t *testing.T) {
	if got := goModVer("module x\n\ngo 1.22\n\nrequire ()\n"); got != "1.22" {
		t.Errorf("goModVer=%q want 1.22", got)
	}
	if got := goModVer("module x\ngo 1.26.1\n"); got != "1.26.1" {
		t.Errorf("goModVer=%q want 1.26.1", got)
	}
}

func TestPkgJSONNode(t *testing.T) {
	if got := pkgJSONNode(`{"name":"x","engines":{"node":">=18.0.0","npm":">=9"}}`); got != "18.0.0" {
		t.Errorf("pkgJSONNode=%q want 18.0.0", got)
	}
	if got := pkgJSONNode(`{"dependencies":{"node":"^1.0"}}`); got != "" {
		t.Errorf("应忽略依赖里的 node，got %q", got)
	}
}

func TestPyRequires(t *testing.T) {
	if got := pyRequires("[project]\nrequires-python = \">=3.10\"\n"); got != "3.10" {
		t.Errorf("pyRequires=%q want 3.10", got)
	}
}

func TestGemfileRuby(t *testing.T) {
	if got := gemfileRuby("source 'x'\nruby \"3.2.3\"\n"); got != "3.2.3" {
		t.Errorf("gemfileRuby=%q want 3.2.3", got)
	}
}

func TestProjectVersionIntegration(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\ngo 1.21\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte("18.17.0\n"), 0o644)
	if v, _, ok := projectVersion("go", dir); !ok || v != "1.21" {
		t.Errorf("go=%q ok=%v want 1.21", v, ok)
	}
	if v, _, ok := projectVersion("node", dir); !ok || v != "18.17.0" {
		t.Errorf("node=%q ok=%v want 18.17.0", v, ok)
	}
	if _, _, ok := projectVersion("python", dir); ok {
		t.Error("python 不应被识别（无对应文件）")
	}
}
