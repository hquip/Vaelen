package core

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var goVersionToken = regexp.MustCompile(`^go(\d+\.\d+(?:\.\d+)?(?:(?:beta|rc)\d+)?(?:[-+.][A-Za-z0-9]+)*)$`)
var goVersionPlatform = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)

func goToolchainVersion(text string) string {
	// runtime.Version can append experiment/compiler details after the version.
	fields := strings.Fields(text)
	if len(fields) > 0 {
		if match := goVersionToken.FindStringSubmatch(fields[0]); match != nil {
			return match[1]
		}
	}
	return ""
}

// Read cmd/go's own build metadata first: launching "go version" in a project
// can download/run a different toolchain and report that version instead.
func probeGoVersion(bin string) (string, error) {
	if info, err := buildinfo.ReadFile(bin); err == nil && info.Path == "cmd/go" {
		if version := goToolchainVersion(info.GoVersion); version != "" {
			return version, nil
		}
	}
	return probeGoVersionCommand(bin)
}

// Wrappers or older binaries may not contain readable metadata. In that case
// disable toolchain selection and user config for this child only. Never infer
// a version from its directory name, diagnostic output, or a failed command.
func probeGoVersionCommand(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.Env = goProbeEnv(os.Environ())
	cmd.WaitDelay = 250 * time.Millisecond
	hideConsole(cmd)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("Go 版本探测超时")
	}
	if err != nil {
		return "", fmt.Errorf("go version 执行失败: %w", err)
	}
	return parseGoVersionOutput(string(output))
}

func parseGoVersionOutput(output string) (string, error) {
	text := strings.TrimSpace(output)
	fields := strings.Fields(text)
	if strings.ContainsAny(text, "\r\n") || len(fields) < 4 || fields[0] != "go" || fields[1] != "version" || !goVersionPlatform.MatchString(fields[len(fields)-1]) {
		return "", fmt.Errorf("go version 未返回有效版本信息")
	}
	if version := goToolchainVersion(fields[2]); version != "" {
		return version, nil
	}
	return "", fmt.Errorf("无法识别 Go 版本: %.256s", fields[2])
}

func goProbeEnv(original []string) []string {
	env := make([]string, 0, len(original)+3)
	for _, entry := range original {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			env = append(env, entry)
			continue
		}
		switch strings.ToUpper(key) {
		case "GOROOT", "GOTOOLCHAIN", "GOENV", "GOWORK", "GOFLAGS":
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off")
}
