package netcfg

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func automaticTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("VSM_HOME", t.TempDir())
	for _, key := range []string{"VSM_PROXY", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(key, "")
	}
}

func TestDiscoverLocalProxyRequiresProtocolAndPrefersSystem(t *testing.T) {
	var probes atomic.Int32
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		if r.Method != "CONNECT" || r.Host != "api.github.com:443" {
			t.Errorf("unexpected probe: %s %s", r.Method, r.Host)
		}
		w.WriteHeader(200)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(405) }))
	defer bad.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	proxy, source := discoverLocalProxy(ctx, []proxyCandidate{{bad.URL, "invalid"}, {good.URL, "system"}}, []proxyCandidate{{good.URL, "common"}})
	if proxy == nil || proxy.String() != good.URL || source != "system" || probes.Load() != 1 {
		t.Fatalf("%v %s probes=%d", proxy, source, probes.Load())
	}
	proxy, source = discoverLocalProxy(ctx, nil, []proxyCandidate{{bad.URL, "invalid"}, {good.URL, "common"}})
	if proxy == nil || source != "common" {
		t.Fatalf("common-port fallback: %v %s", proxy, source)
	}
	if _, ok := probeLocalProxy(ctx, "http://remote.example:7897"); ok {
		t.Fatal("discovery probed a remote proxy")
	}
	if _, ok := probeLocalProxy(ctx, bad.URL); ok {
		t.Fatal("ordinary HTTP listener accepted as proxy")
	}
}

func TestDiscoverSOCKSProxyAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(time.Second))
		var greeting [3]byte
		if _, err := io.ReadFull(conn, greeting[:]); err != nil || greeting != [3]byte{5, 1, 0} {
			return
		}
		conn.Write([]byte{5, 0})
		var request [4]byte
		if _, err := io.ReadFull(conn, request[:]); err != nil || request != [4]byte{5, 1, 0, 3} {
			return
		}
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return
		}
		target := make([]byte, int(length[0])+2)
		if _, err := io.ReadFull(conn, target); err != nil || string(target[:len(target)-2]) != "api.github.com" {
			return
		}
		conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 1, 187})
	}()
	proxy, ok := probeLocalProxy(context.Background(), "socks5://"+listener.Addr().String())
	if !ok || proxy == nil {
		t.Fatal("working SOCKS5 tunnel was not detected")
	}
	<-done
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, ok := probeLocalProxy(ctx, "http://127.0.0.1:1"); ok || time.Since(start) > time.Second {
		t.Fatal("cancelled probe did not stop")
	}
}

func TestAutomaticProxyCarriesRealHTTPSRequestAndRespectsOverrides(t *testing.T) {
	automaticTestEnv(t)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "verified proxy path") }))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			http.Error(w, "CONNECT required", 405)
			return
		}
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n")
		go func() { defer upstream.Close(); io.Copy(upstream, client) }()
		go func() { defer client.Close(); io.Copy(client, upstream) }()
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	automaticCache.Lock()
	automaticCache.proxy, automaticCache.source, automaticCache.until = u, "local-proxy", time.Now().Add(time.Minute)
	automaticCache.Unlock()
	t.Cleanup(func() {
		automaticCache.Lock()
		automaticCache.until = time.Time{}
		automaticCache.proxy = nil
		automaticCache.Unlock()
	})
	tr := Transport()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // local fixture certificate
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: time.Second}
	response, err := client.Get("https://download.example.test/payload")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(data) != "verified proxy path" {
		t.Fatalf("%s %v", data, err)
	}
	for _, tc := range []struct{ setting, env, bypass, want string }{
		{"", "", "", proxy.URL}, {"off", "", "", ""}, {"http://explicit.example:8080", "", "", "http://explicit.example:8080"},
		{"", "socks5://environment.example:1080", "", "socks5://environment.example:1080"},
		{"", "", "*.example.test", ""}, {"", "", "example.test:80", proxy.URL}, {"", "", "example.test:443", ""},
	} {
		t.Setenv("VSM_PROXY", tc.setting)
		t.Setenv("HTTPS_PROXY", tc.env)
		t.Setenv("NO_PROXY", tc.bypass)
		req, _ := http.NewRequest("GET", "https://download.example.test/payload", nil)
		got, err := proxyForRequest(req)
		value := ""
		if got != nil {
			value = got.String()
		}
		if err != nil || value != tc.want {
			t.Fatalf("%+v: %q %v", tc, value, err)
		}
	}
}
