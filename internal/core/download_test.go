package core

import "testing"

func TestArchiveExt(t *testing.T) {
	cases := map[string]string{
		"https://x/go1.22.0.windows-amd64.zip": ".zip",
		"https://x/node-v20.tar.gz":            ".tar.gz",
		"https://x/a.tgz":                      ".tgz",
		"https://x/py.tar.xz":                  ".tar.xz",
		"https://x/file.zip?token=1":           ".zip",
	}
	for in, want := range cases {
		if got := archiveExt(in); got != want {
			t.Errorf("archiveExt(%q)=%q want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:     "512B",
		1024:    "1.0KB",
		1536:    "1.5KB",
		1048576: "1.0MB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d)=%q want %q", in, got, want)
		}
	}
}
