package netcfg

import (
	"strings"
	"syscall"
	"unsafe"
)

func systemProxyCandidates() []proxyCandidate {
	type proxyConfig struct {
		autoDetect             int32
		autoURL, proxy, bypass *uint16
	}
	var config proxyConfig
	get := syscall.NewLazyDLL("winhttp.dll").NewProc("WinHttpGetIEProxyConfigForCurrentUser")
	ok, _, _ := get.Call(uintptr(unsafe.Pointer(&config)))
	if ok == 0 {
		return nil
	}
	free := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalFree")
	defer func() {
		for _, p := range []*uint16{config.autoURL, config.proxy, config.bypass} {
			if p != nil {
				free.Call(uintptr(unsafe.Pointer(p)))
			}
		}
	}()
	if config.proxy == nil {
		return nil
	}
	var chars []uint16
	for i := 0; i < 32768; i++ {
		value := *(*uint16)(unsafe.Add(unsafe.Pointer(config.proxy), i*2))
		if value == 0 {
			break
		}
		chars = append(chars, value)
	}
	return parseSystemProxyList(syscall.UTF16ToString(chars))
}

func parseSystemProxyList(raw string) []proxyCandidate {
	var result []proxyCandidate
	for _, protocol := range []string{"https", "http", "socks", ""} {
		for _, field := range strings.Split(raw, ";") {
			field = strings.TrimSpace(field)
			key, address, qualified := strings.Cut(field, "=")
			if !qualified {
				key, address = "", field
			}
			if !strings.EqualFold(key, protocol) || address == "" {
				continue
			}
			if !strings.Contains(address, "://") {
				if protocol == "socks" {
					address = "socks5://" + address
				} else {
					address = "http://" + address
				}
			}
			result = append(result, proxyCandidate{address, "local-proxy"})
		}
	}
	return result
}
