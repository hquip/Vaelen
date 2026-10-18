package cli

import (
	"bytes"
	"strings"
	"testing"

	"vsm/internal/updater"
)

func TestUpdateProgressSeparatesDownloadFromValidation(t *testing.T) {
	var out bytes.Buffer
	print := updateProgressPrinter(&out)
	print(updater.Progress{Stage: "downloading", Asset: "vsm.exe", Downloaded: 1024, Total: 2048, OverallDownloaded: 1024, OverallTotal: 2048, BytesPerSecond: 512, Attempt: 2})
	print(updater.Progress{Stage: "verifying", Asset: "vsm.exe"})
	print(updater.Progress{Stage: "validating", Asset: "vsm.exe"})
	print(updater.Progress{Stage: "replacing"})
	for _, text := range []string{"50.0%", "512B/s", "第 2 次尝试", "校验 SHA256", "验证程序版本", "备份并替换"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %s: %s", text, out.String())
		}
	}
}
