package netcfg

import "testing"

func TestWindowsSystemProxyParsing(t *testing.T) {
	got := parseSystemProxyList("http=127.0.0.1:7890;https=127.0.0.1:7897;socks=[::1]:1080")
	if len(got) != 3 || got[0].address != "http://127.0.0.1:7897" || got[1].address != "http://127.0.0.1:7890" || got[2].address != "socks5://[::1]:1080" {
		t.Fatalf("%+v", got)
	}
	if got := parseSystemProxyList("127.0.0.1:7897"); len(got) != 1 || got[0].address != "http://127.0.0.1:7897" {
		t.Fatalf("%+v", got)
	}
}
