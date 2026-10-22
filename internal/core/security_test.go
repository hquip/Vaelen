package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testTar(t *testing.T, names ...string) string {
	return testTarMode(t, 0644, names...)
}

func testTarMode(t *testing.T, mode int64, names ...string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "test.tar.gz")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: mode, Size: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestExtractionRejectsPortableTraversal(t *testing.T) {
	for _, name := range []string{"../victim", "/absolute", `C:\victim`, `a\..\victim`, "a:stream", "NUL", "COM1.txt", "trailing. ", "a/../../victim"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			if err := Extract(testTar(t, name), t.TempDir()); err == nil {
				t.Fatalf("unsafe path accepted: %q", name)
			}
		})
	}
}

func TestExtractionCannotTraverseExistingDirectoryLink(t *testing.T) {
	out, outside := t.TempDir(), t.TempDir()
	if err := linkDir(outside, filepath.Join(out, "redirect")); err != nil {
		t.Fatal(err)
	}
	if err := Extract(testTar(t, "redirect/victim"), out); err == nil {
		t.Fatal("followed an out-of-root link")
	}
	if pathExists(filepath.Join(outside, "victim")) {
		t.Fatal("wrote outside extraction root")
	}
}

func TestExtractionRejectsDuplicatesAndExpansionLimits(t *testing.T) {
	if err := Extract(testTar(t, "a", "a"), t.TempDir()); err == nil {
		t.Fatal("duplicate overwrote an existing file")
	}
	if err := ExtractWithLimits(testTar(t, "a", "b"), t.TempDir(), ArchiveLimits{1, 100, 100}); err == nil {
		t.Fatal("file count limit not enforced")
	}
	if err := ExtractWithLimits(testTar(t, "a", "b"), t.TempDir(), ArchiveLimits{10, 100, 1}); err == nil {
		t.Fatal("total expansion limit not enforced")
	}
	if err := Extract(testTar(t, ".config"), t.TempDir()); err != nil {
		t.Fatalf("legitimate dotfile rejected: %v", err)
	}
}

func TestZipSymlinkEscapeRejected(t *testing.T) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "escape"}
	h.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("../outside"))
	zw.Close()
	src := filepath.Join(t.TempDir(), "test.zip")
	os.WriteFile(src, b.Bytes(), 0o644)
	if err := Extract(src, t.TempDir()); err == nil {
		t.Fatal("unsafe zip symlink accepted")
	}
}

func TestUninstallRejectsPathTraversalAndToolJunction(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.ToolInstalls("go"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkTree(t, filepath.Join(p.Root, "victim"), map[string]string{"keep": "safe"})
	if err := (Installer{Paths: p}).Uninstall("go", "../../victim"); err == nil {
		t.Fatal("accepted traversal to an existing outside directory")
	}
	if !fileExists(filepath.Join(p.Root, "victim", "keep")) {
		t.Fatal("deleted existing outside target")
	}
	if err := os.Remove(p.ToolInstalls("go")); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "keep")
	os.WriteFile(sentinel, []byte("keep"), 0o644)
	for _, version := range []string{"..", "../../outside", `..\outside`, "C:", "NUL"} {
		if err := (Installer{Paths: p}).Uninstall("go", version); err == nil {
			t.Fatalf("accepted %q", version)
		}
	}
	outside := t.TempDir()
	mkTree(t, outside, map[string]string{"1.0/keep": "keep"})
	os.MkdirAll(p.Installs(), 0o755)
	if err := linkDir(outside, p.ToolInstalls("go")); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{Paths: p}).Uninstall("go", "1.0"); err == nil {
		t.Fatal("accepted tool-level junction")
	}
	if !fileExists(filepath.Join(outside, "1.0", "keep")) {
		t.Fatal("deleted outside data")
	}
}

func TestForgedMarkerDoesNotAuthorizeUninstall(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	target := t.TempDir()
	mkTree(t, target, map[string]string{installMarker: "vsm\n", "keep": "safe"})
	dest := p.InstallDir("go", "1.0")
	os.MkdirAll(filepath.Dir(dest), 0o755)
	if err := linkDir(target, dest); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{Paths: p}).Uninstall("go", "1.0"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(target, "keep")) {
		t.Fatal("marker alone caused external deletion")
	}
}

func TestLinkDirKeepsShellMetacharactersLiteral(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "sdk %TEMP% & (test) ^", "real")
	mkTree(t, target, map[string]string{"keep": "safe"})
	link := filepath.Join(base, "link %TEMP% & (test) ^")
	if err := linkDir(target, link); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(link, "keep")) {
		t.Fatal("junction path was interpreted by a shell")
	}
}

func TestInstallRootCannotAliasShims(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := linkDir(p.Shims(), alias); err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateInstallRoot(alias); err == nil {
		a, _ := canonicalPath(alias)
		r, _ := canonicalPath(p.Root)
		t.Fatalf("symlink alias bypassed installation isolation: alias=%q root=%q", a, r)
	}
}

func TestDownloadRefusesHTMLAndLeavesNoPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>not an archive</html>")) }))
	defer srv.Close()
	dir := t.TempDir()
	if _, err := downloadOnce(srv.URL+"/payload.zip", dir, "test", func(string) {}, downloadIntegrity{}); err == nil {
		t.Fatal("HTML accepted as a tool archive")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("failed download leaked partial files: %v", entries)
	}
}

func TestRangeDownloadValidatesHeaderAndLength(t *testing.T) {
	for _, mode := range []string{"wrong-range", "short", "long", "valid"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-Match") != "\"stable\"" {
					t.Error("missing entity guard")
				}
				cr := "bytes 0-3/4"
				if mode == "wrong-range" {
					cr = "bytes 1-4/4"
				}
				w.Header().Set("Content-Range", cr)
				w.WriteHeader(http.StatusPartialContent)
				data := "data"
				if mode == "short" {
					data = "d"
				}
				if mode == "long" {
					data = "dataEXTRA"
				}
				w.Write([]byte(data))
			}))
			defer srv.Close()
			dest := filepath.Join(t.TempDir(), "part")
			os.WriteFile(dest, []byte(strings.Repeat("_", 4)), 0o644)
			err := downloadChunk(srv.URL, dest, 0, 3, 4, &countingWriter{progress: func(string) {}}, "\"stable\"")
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			data, _ := os.ReadFile(dest)
			if len(data) > 4 {
				t.Fatal("chunk overwrote adjacent ranges")
			}
		})
	}
}
