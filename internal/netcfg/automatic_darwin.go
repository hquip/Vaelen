package netcfg

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"time"
)

func systemProxyCandidates() []proxyCandidate {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return nil
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, " : ")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	var result []proxyCandidate
	for _, protocol := range []string{"HTTPS", "HTTP", "SOCKS"} {
		if values[protocol+"Enable"] != "1" {
			continue
		}
		scheme := "http"
		if protocol == "SOCKS" {
			scheme = "socks5"
		}
		result = append(result, proxyCandidate{scheme + "://" + net.JoinHostPort(values[protocol+"Proxy"], values[protocol+"Port"]), "local-proxy"})
	}
	return result
}
