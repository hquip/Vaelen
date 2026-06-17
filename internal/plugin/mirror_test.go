package plugin

import "testing"

func TestMirrorURLCN(t *testing.T) {
	t.Setenv("VSM_MIRROR", "cn")
	cases := map[string]string{
		"https://go.dev/dl/go1.22.0.windows-amd64.zip":              "https://golang.google.cn/dl/go1.22.0.windows-amd64.zip",
		"https://nodejs.org/dist/index.json":                       "https://npmmirror.com/mirrors/node/index.json",
		"https://github.com/astral-sh/x/releases/download/a.tar.gz": "https://ghproxy.com/https://github.com/astral-sh/x/releases/download/a.tar.gz",
		"https://api.github.com/repos/x/y/releases":                "https://api.github.com/repos/x/y/releases", // 不重写
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
