package cli

import (
	"fmt"
	"io"
	"time"

	"vsm/internal/updater"
)

func updateProgressPrinter(out io.Writer) func(updater.Progress) {
	lastKey := ""
	var lastPrint time.Time
	return func(p updater.Progress) {
		key := fmt.Sprintf("%s/%s/%d", p.Stage, p.Asset, p.Attempt)
		if key == lastKey && time.Since(lastPrint) < time.Second && p.Downloaded < p.Total {
			return
		}
		lastKey, lastPrint = key, time.Now()
		switch p.Stage {
		case "checking":
			fmt.Fprintln(out, "正在检查官方 GitHub Release…")
		case "routing":
			fmt.Fprintln(out, "检测本机代理；没有可用代理时尝试国内下载源…")
		case "official-fallback":
			fmt.Fprintln(out, "国内源 SHA256 校验失败，回退官方文件…")
		case "connecting":
			fmt.Fprintln(out, "连接更新源:", p.Asset, updateRouteLabel(p), "（连续 15 秒无数据将重试）")
		case "retrying":
			route := updateRouteLabel(p)
			fmt.Fprintf(out, "重试 %s · 第 %d/3 次 · 已收 %s · %s\n", p.Asset, p.Attempt, humanSize(p.Downloaded), route)
		case "parallel":
			fmt.Fprintf(out, "启用 %d 连接分块下载: %s\n", p.Connections, p.Asset)
		case "fallback":
			fmt.Fprintln(out, "源站未正确支持分块，改为单连接:", p.Asset)
		case "downloading":
			percent := float64(0)
			if p.OverallTotal > 0 {
				percent = min(100, 100*float64(p.OverallDownloaded)/float64(p.OverallTotal))
			}
			fmt.Fprintf(out, "下载 %s · %.1f%% · %s/%s · %s/s · 第 %d 次尝试 · %s\n", p.Asset, percent, humanSize(p.OverallDownloaded), humanSize(p.OverallTotal), humanSize(int64(p.BytesPerSecond)), p.Attempt, updateRouteLabel(p))
		case "verifying":
			fmt.Fprintln(out, "校验 SHA256:", p.Asset)
		case "extracting":
			fmt.Fprintln(out, "正在解压 macOS 应用…")
		case "validating":
			fmt.Fprintln(out, "验证程序版本、平台或签名:", p.Asset)
		case "replacing":
			fmt.Fprintln(out, "正在备份并替换程序…")
		}
	}
}

func updateRouteLabel(p updater.Progress) string {
	network := map[string]string{"local-proxy": "本机代理", "configured-proxy": "已配置代理", "environment-proxy": "环境变量代理", "direct": "直连"}[p.Network]
	source := map[string]string{"cn-mirror": "国内下载源", "github": "GitHub 官方下载", "github-api": "GitHub 官方接口", "github-cdn": "GitHub 官方 CDN"}[p.Source]
	if network == "" {
		return source
	}
	if source == "" {
		return network
	}
	return network + " · " + source
}
