package netcfg

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Route describes the effective proxy without exposing authentication details.
type Route struct {
	Proxy  string `json:"proxy,omitempty"`
	Source string `json:"source"`
}

type proxyCandidate struct{ address, source string }

var automaticCache struct {
	sync.Mutex
	until  time.Time
	proxy  *url.URL
	source string
}

func isLoopback(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

// These probes only contact loopback listeners. A listening TCP port alone is
// not sufficient: the listener must establish a proxy tunnel to GitHub.
func probeLocalProxy(ctx context.Context, raw string) (*url.URL, bool) {
	normalized, err := Normalize(raw)
	if err != nil || normalized == "" || normalized == "off" {
		return nil, false
	}
	u, err := url.Parse(normalized)
	if err != nil || !isLoopback(u.Hostname()) || u.User != nil || u.Port() == "" {
		return nil, false
	}
	if u.Scheme != "http" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, false
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if u.Scheme == "http" {
		if _, err := io.WriteString(conn, "CONNECT api.github.com:443 HTTP/1.1\r\nHost: api.github.com:443\r\n\r\n"); err != nil {
			return nil, false
		}
		response, err := http.ReadResponse(bufio.NewReader(io.LimitReader(conn, 8192)), &http.Request{Method: http.MethodConnect})
		if err != nil {
			return nil, false
		}
		defer response.Body.Close()
		return u, response.StatusCode == http.StatusOK
	}
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return nil, false
	}
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil || greeting != [2]byte{5, 0} {
		return nil, false
	}
	host := "api.github.com"
	request := append([]byte{5, 1, 0, 3, byte(len(host))}, host...)
	request = binary.BigEndian.AppendUint16(request, 443)
	if _, err := conn.Write(request); err != nil {
		return nil, false
	}
	var reply [4]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil || reply[0] != 5 || reply[1] != 0 {
		return nil, false
	}
	var addressBytes int
	switch reply[3] {
	case 1:
		addressBytes = 4
	case 4:
		addressBytes = 16
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return nil, false
		}
		addressBytes = int(length[0])
	default:
		return nil, false
	}
	_, err = io.CopyN(io.Discard, conn, int64(addressBytes+2))
	return u, err == nil
}

func discoverLocalProxy(ctx context.Context, system, common []proxyCandidate) (*url.URL, string) {
	// Respect the user's system proxy before guessing application ports.
	for _, candidate := range system {
		if proxy, ok := probeLocalProxy(ctx, candidate.address); ok {
			return proxy, candidate.source
		}
		if ctx.Err() != nil {
			return nil, "direct"
		}
	}
	type result struct {
		proxy  *url.URL
		source string
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, len(common))
	for _, candidate := range common {
		go func() {
			proxy, ok := probeLocalProxy(ctx, candidate.address)
			if !ok {
				proxy = nil
			}
			results <- result{proxy, candidate.source}
		}()
	}
	for range common {
		select {
		case result := <-results:
			if result.proxy != nil {
				return result.proxy, result.source
			}
		case <-ctx.Done():
			return nil, "direct"
		}
	}
	return nil, "direct"
}

func automaticProxy(ctx context.Context) (*url.URL, string) {
	automaticCache.Lock()
	defer automaticCache.Unlock()
	if time.Now().Before(automaticCache.until) {
		return automaticCache.proxy, automaticCache.source
	}
	var common []proxyCandidate
	for _, port := range []int{7897, 7890, 10809, 20171, 8080} {
		common = append(common, proxyCandidate{fmt.Sprintf("http://127.0.0.1:%d", port), "local-proxy"})
	}
	for _, port := range []int{1080, 10808, 7891, 7897, 7890} {
		common = append(common, proxyCandidate{fmt.Sprintf("socks5://127.0.0.1:%d", port), "local-proxy"})
	}
	probeCtx, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
	defer cancel()
	proxy, source := discoverLocalProxy(probeCtx, systemProxyCandidates(), common)
	if ctx.Err() == nil {
		automaticCache.proxy, automaticCache.source = proxy, source
		automaticCache.until = time.Now().Add(5 * time.Second)
	}
	return proxy, source
}

func noProxy(target *url.URL) bool {
	host := target.Hostname()
	if isLoopback(host) {
		return true
	}
	raw := os.Getenv("NO_PROXY")
	if raw == "" {
		raw = os.Getenv("no_proxy")
	}
	for _, entry := range strings.Split(strings.ToLower(raw), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "*" {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(net.ParseIP(host)) {
			return true
		}
		if h, port, err := net.SplitHostPort(entry); err == nil {
			requestPort := target.Port()
			if requestPort == "" {
				if target.Scheme == "https" {
					requestPort = "443"
				} else {
					requestPort = "80"
				}
			}
			if port != requestPort {
				continue
			}
			entry = h
		}
		subdomainsOnly := strings.HasPrefix(entry, ".") || strings.HasPrefix(entry, "*.")
		entry = strings.TrimPrefix(strings.TrimPrefix(entry, "*"), ".")
		if entry != "" && ((!subdomainsOnly && strings.EqualFold(host, entry)) || strings.HasSuffix(strings.ToLower(host), "."+entry)) {
			return true
		}
	}
	return false
}

func resolveProxy(req *http.Request) (*url.URL, string, error) {
	if err := ValidateURL(req.URL.String()); err != nil {
		return nil, "", err
	}
	stored, err := Normalize(ProxySetting())
	if err == nil && stored != "" {
		if stored == "off" {
			return nil, "direct", nil
		}
		u, err := url.Parse(stored)
		return u, "configured-proxy", err
	}
	if noProxy(req.URL) {
		return nil, "direct", nil
	}
	key := "HTTPS_PROXY"
	if req.URL.Scheme == "http" {
		key = "HTTP_PROXY"
	}
	value := os.Getenv(key)
	if value == "" {
		value = os.Getenv(strings.ToLower(key))
	}
	if value == "" {
		value = os.Getenv("ALL_PROXY")
	}
	if value == "" {
		value = os.Getenv("all_proxy")
	}
	if value != "" {
		normalized, err := Normalize(value)
		if err != nil {
			return nil, "", err
		}
		if normalized == "off" {
			return nil, "direct", nil
		}
		proxy, err := url.Parse(normalized)
		return proxy, "environment-proxy", err
	}
	proxy, source := automaticProxy(req.Context())
	return proxy, source, nil
}

func RouteForURL(ctx context.Context, raw string) (Route, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return Route{}, err
	}
	proxy, source, err := resolveProxy(req)
	if err != nil {
		return Route{}, err
	}
	if proxy == nil {
		return Route{Source: "direct"}, nil
	}
	return Route{Proxy: proxy.Scheme + "://" + proxy.Host, Source: source}, nil
}
