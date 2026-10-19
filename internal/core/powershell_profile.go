package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"vsm/internal/config"
)

type PowerShellProfiles struct {
	Paths    []string `json:"paths"`
	Warnings []string `json:"warnings"`
}

// PowerShellActivationCommand uses the paired CLI even when PATH contains an old copy.
func PowerShellActivationCommand(vsmCmd string) string {
	return "& " + quotePowerShell(vsmCmd) + " activate powershell | Out-String | Invoke-Expression"
}

// PersistPowerShellHooks configures both Windows PowerShell and installed pwsh.
// Profile discovery respects redirected/OneDrive Documents folders. Never alter
// the user's execution policy or existing prompt/custom startup commands.
func PersistPowerShellHooks(vsmCmd string) (PowerShellProfiles, error) {
	result := PowerShellProfiles{Paths: []string{}, Warnings: []string{}}
	if runtime.GOOS != "windows" {
		return result, fmt.Errorf("仅用于 Windows PowerShell 激活")
	}
	if vsmCmd == "" {
		return result, fmt.Errorf("未找到配套 VSM CLI")
	}
	for _, shell := range []string{"powershell", "pwsh"} {
		bin, err := exec.LookPath(shell)
		if err != nil {
			if shell == "pwsh" {
				continue
			}
			return result, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, bin, "-NoProfile", "-NonInteractive", "-Command", `[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); [pscustomobject]@{path=$PROFILE.CurrentUserAllHosts;policy=(Get-ExecutionPolicy).ToString()} | ConvertTo-Json -Compress`)
		hideConsole(cmd)
		cmd.WaitDelay = time.Second
		out, err := cmd.Output()
		cancel()
		if err != nil {
			return result, fmt.Errorf("读取 %s 配置位置失败: %w", shell, err)
		}
		var profile struct {
			Path   string `json:"path"`
			Policy string `json:"policy"`
		}
		if err := json.Unmarshal(bytes.TrimPrefix(out, []byte{0xef, 0xbb, 0xbf}), &profile); err != nil {
			return result, err
		}
		if !filepath.IsAbs(profile.Path) {
			return result, fmt.Errorf("%s 未返回有效配置文件位置", shell)
		}
		line := PowerShellActivationCommand(vsmCmd)
		if _, err := persistPowerShellBlock(profile.Path, line); err != nil {
			return result, err
		}
		result.Paths = append(result.Paths, profile.Path)
		if profile.Policy == "Restricted" || profile.Policy == "AllSigned" {
			result.Warnings = append(result.Warnings, shell+" 执行策略为 "+profile.Policy+"，可能阻止配置加载；VSM 不更改执行策略，可在当前终端手动执行激活命令")
		}
	}
	return result, nil
}

// Windows PowerShell profiles commonly use UTF-16 or a UTF-8 BOM. Preserve the
// original encoding and all text outside our marked block, including on upgrade.
func persistPowerShellBlock(path, line string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	encoding := "utf8bom"
	data := raw
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}), bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		encoding = "utf16le"
		var order binary.ByteOrder = binary.LittleEndian
		if raw[0] == 0xfe {
			encoding = "utf16be"
			order = binary.BigEndian
		}
		if len(raw)%2 != 0 {
			return "", fmt.Errorf("配置文件 UTF-16 内容损坏: %s", path)
		}
		units := make([]uint16, (len(raw)-2)/2)
		for i := range units {
			units[i] = order.Uint16(raw[2+i*2:])
		}
		for i := 0; i < len(units); i++ {
			u := units[i]
			if u >= 0xd800 && u <= 0xdbff {
				if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
					return "", fmt.Errorf("配置文件 UTF-16 内容损坏: %s", path)
				}
				i++
			} else if u >= 0xdc00 && u <= 0xdfff {
				return "", fmt.Errorf("配置文件 UTF-16 内容损坏: %s", path)
			}
		}
		data = []byte(string(utf16.Decode(units)))
	case bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}):
		data = raw[3:]
	case len(raw) > 0:
		encoding = "utf8"
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("配置文件不是 UTF-8/UTF-16，请手动添加 VSM 激活命令: %s", path)
	}
	text := string(data)
	if strings.Contains(text, "# SIG # Begin signature block") {
		return "", fmt.Errorf("PowerShell 配置文件已签名，为保留签名未修改 %s；请在当前终端手动激活 VSM", path)
	}
	newline := "\n"
	if len(raw) == 0 || strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	block := hookBlockBegin + newline + line + newline + hookBlockEnd
	begin, end := strings.Index(text, hookBlockBegin), strings.Index(text, hookBlockEnd)
	status := "added"
	if begin >= 0 || end >= 0 {
		if begin < 0 || end < begin || strings.Count(text, hookBlockBegin) != 1 || strings.Count(text, hookBlockEnd) != 1 {
			return "", fmt.Errorf("VSM 激活区块不完整，请手动检查 %s", path)
		}
		updated := text[:begin] + block + text[end+len(hookBlockEnd):]
		if updated == text {
			return "exists", nil
		}
		text, status = updated, "updated"
	} else {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += newline
		}
		text += newline + block + newline
	}
	data = []byte(text)
	switch encoding {
	case "utf8bom":
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	case "utf16le", "utf16be":
		var order binary.ByteOrder = binary.LittleEndian
		bom := []byte{0xff, 0xfe}
		if encoding == "utf16be" {
			order = binary.BigEndian
			bom = []byte{0xfe, 0xff}
		}
		units := utf16.Encode([]rune(text))
		data = make([]byte, 2+len(units)*2)
		copy(data, bom)
		for i, u := range units {
			order.PutUint16(data[2+i*2:], u)
		}
	}
	if err := config.WriteFileAtomic(path, data, 0o644); err != nil {
		return "", err
	}
	return status, nil
}
