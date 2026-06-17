package netcfg

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{"system", "", false},
		{"default", "", false},
		{"-", "", false},
		{"env", "", false},
		{"off", "off", false},
		{"OFF", "off", false},
		{"none", "off", false},
		{"direct", "off", false},
		{"127.0.0.1:7890", "http://127.0.0.1:7890", false},
		{"http://127.0.0.1:7890", "http://127.0.0.1:7890", false},
		{"socks5://127.0.0.1:1080", "socks5://127.0.0.1:1080", false},
		{"http://user:pass@host:3128", "http://user:pass@host:3128", false},
		{"http://", "", true},
		{"://nohost", "", true},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("Normalize(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("Normalize(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestProxySettingEnvWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VSM_HOME", dir)
	if err := writeProxyFile(dir, "http://file-proxy:1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VSM_PROXY", "http://env-proxy:2")
	if got := ProxySetting(); got != "http://env-proxy:2" {
		t.Errorf("环境变量应优先，got %q", got)
	}
}

func TestProxySettingFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VSM_HOME", dir)
	t.Setenv("VSM_PROXY", "")
	if err := writeProxyFile(dir, "socks5://127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}
	if got := ProxySetting(); got != "socks5://127.0.0.1:1080" {
		t.Errorf("应读取文件值，got %q", got)
	}
}

func TestProxyForRequest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VSM_HOME", dir)
	t.Setenv("VSM_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

	// 显式 URL：走该代理
	if err := writeProxyFile(dir, "http://127.0.0.1:7890"); err != nil {
		t.Fatal(err)
	}
	u, err := proxyForRequest(req)
	if err != nil || u == nil || u.Host != "127.0.0.1:7890" {
		t.Fatalf("显式代理解析失败: u=%v err=%v", u, err)
	}

	// off：强制直连
	if err := writeProxyFile(dir, "off"); err != nil {
		t.Fatal(err)
	}
	u, err = proxyForRequest(req)
	if err != nil || u != nil {
		t.Fatalf("off 应直连: u=%v err=%v", u, err)
	}
}

func writeProxyFile(dir, content string) error {
	return os.WriteFile(filepath.Join(dir, "proxy"), []byte(content+"\n"), 0o644)
}
