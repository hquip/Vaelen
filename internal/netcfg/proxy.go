// Package netcfg 提供前端（CLI/GUI）与语言插件共享的网络配置：HTTP/HTTPS/SOCKS5 代理。
//
// 它单独成包且只依赖标准库，因此 core 与 plugin 都能复用而不引入循环导入
// （注意：core 依赖 plugin，故二者共享的代理逻辑不能落在任一方包内）。
//
// 代理与镜像（mirror）是正交的两层：镜像重写下载 URL，代理作用于传输层。
// 这里把代理统一注入到所有 http.Client，使 install / list-all 等全部网络请求都生效。
package netcfg

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// root 解析 VSM 数据根目录：优先环境变量 VSM_HOME，否则 ~/.vsm。
// 与 core.DefaultPaths 保持一致，但此处自带实现以避免依赖 core 包。
func root() string {
	if env := strings.TrimSpace(os.Getenv("VSM_HOME")); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".vsm")
}

// ProxyFile 返回代理配置文件路径（<VSM_HOME>/proxy）。无法解析根目录时返回 ""。
func ProxyFile() string {
	r := root()
	if r == "" {
		return ""
	}
	return filepath.Join(r, "proxy")
}

// ProxySetting 读取当前代理配置的原始字符串。
// 优先级：环境变量 VSM_PROXY > <VSM_HOME>/proxy 文件。
// 每次调用都重新读取，使 GUI 等长驻进程在运行时切换后即时生效。
// 返回 "" 表示未配置（届时回退到系统环境变量 HTTP(S)_PROXY/NO_PROXY）。
func ProxySetting() string {
	if v := strings.TrimSpace(os.Getenv("VSM_PROXY")); v != "" {
		return v
	}
	p := ProxyFile()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Normalize 把用户输入规范化为“可落盘的代理配置值”，供 CLI 与 GUI 复用同一套校验。
//
//	"" / system / default / - / env / follow -> ""    （清空配置，回退系统环境变量）
//	off / none / direct / no                 -> "off" （强制直连，忽略系统环境变量）
//	其余                                       -> 规范化后的代理 URL（缺省 scheme 按 http 处理）
//
// 非法地址（解析失败或缺少 host）返回错误。
func Normalize(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "system", "default", "-", "env", "follow":
		return "", nil
	case "off", "none", "direct", "no":
		return "off", nil
	}
	raw := s
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("非法代理地址 %q（示例 http://127.0.0.1:7890 或 socks5://127.0.0.1:1080）", s)
	}
	return u.String(), nil
}

// proxyForRequest 实现 http.Transport.Proxy 的签名，按 vsm 配置决定每个请求的代理。
// 每请求读取配置，因此运行时切换即时生效；配置非法时回退系统环境变量而非直接断网。
func proxyForRequest(req *http.Request) (*url.URL, error) {
	stored, err := Normalize(ProxySetting())
	if err != nil || stored == "" {
		return http.ProxyFromEnvironment(req)
	}
	if stored == "off" {
		return nil, nil
	}
	return url.Parse(stored)
}

// Transport 返回带 vsm 代理配置的 http.Transport，克隆标准库默认值以保留连接池等行为。
func Transport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = proxyForRequest
	return tr
}

// Client 返回带 vsm 代理配置与给定超时的 *http.Client。
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport()}
}

// Describe 返回适合展示的 (生效值, 来源) 二元组，供 CLI/GUI 呈现当前代理状态。
// 生效值为空表示“未配置（跟随系统环境变量）”。
func Describe() (value, source string) {
	if v := strings.TrimSpace(os.Getenv("VSM_PROXY")); v != "" {
		return v, "环境变量 VSM_PROXY"
	}
	p := ProxyFile()
	if p != "" {
		if b, err := os.ReadFile(p); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v, "配置文件 " + p
			}
		}
	}
	return "", ""
}

// SystemProxyEnv 返回系统环境变量里设置的代理（按常见优先级），用于在“未配置”时给用户提示。
func SystemProxyEnv() string {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
