package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

// 镜像规则：按上游 URL 前缀整体替换为国内镜像前缀。下载与元数据 JSON 都会经过重写。
//
// - go.dev/dl/      → golang.google.cn/dl/      （官方中国镜像，元数据与二进制都覆盖）
// - nodejs.org/dist → npmmirror.com/mirrors/node（index.json 与发行包都覆盖）
// - github.com/     → ghproxy 加速（deno/bun/python 等基于 GitHub Release 的下载）
//
// 注意：api.github.com、api.adoptium.net 等“API 端点”不在此重写（结构不一致），
// 仅二进制下载与可整体替换的元数据走镜像。
var mirrorRules = map[string][][2]string{
	"cn": {
		{"https://go.dev/dl/", "https://golang.google.cn/dl/"},
		{"https://nodejs.org/dist/", "https://npmmirror.com/mirrors/node/"},
		{"https://github.com/", "https://ghproxy.com/https://github.com/"},
	},
}

// mirrorMode 读取当前镜像模式：优先环境变量 VSM_MIRROR，其次 <VSM_HOME>/mirror 文件。
// 返回小写模式名（如 "cn"），未设置返回 ""。每次读取以便 GUI 运行时切换即时生效。
func mirrorMode() string {
	if v := strings.TrimSpace(os.Getenv("VSM_MIRROR")); v != "" {
		return strings.ToLower(v)
	}
	root := os.Getenv("VSM_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".vsm")
	}
	b, err := os.ReadFile(filepath.Join(root, "mirror"))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(b)))
}

// MirrorURL 按当前镜像设置重写下载 / 元数据 URL；未启用或无匹配规则时原样返回。
func MirrorURL(url string) string {
	mode := mirrorMode()
	if mode == "" || mode == "off" {
		return url
	}
	rules, ok := mirrorRules[mode]
	if !ok {
		return url
	}
	for _, r := range rules {
		if strings.HasPrefix(url, r[0]) {
			return r[1] + strings.TrimPrefix(url, r[0])
		}
	}
	return url
}
