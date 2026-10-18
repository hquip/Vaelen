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
		case "downloading":
			percent := float64(0)
			if p.OverallTotal > 0 {
				percent = min(100, 100*float64(p.OverallDownloaded)/float64(p.OverallTotal))
			}
			fmt.Fprintf(out, "下载 %s · %.1f%% · %s/%s · %s/s · 第 %d 次尝试\n", p.Asset, percent, humanSize(p.OverallDownloaded), humanSize(p.OverallTotal), humanSize(int64(p.BytesPerSecond)), p.Attempt)
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
