package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"vsm/internal/logging"
	"vsm/internal/mcpserver"
	"vsm/internal/plugin"
)

// SystemVersion 描述在系统中（非 vsm 管理）发现的某个语言运行时。
type SystemVersion struct {
	Tool           string `json:"tool"`
	Command        string `json:"command"`
	Path           string `json:"path"`
	Version        string `json:"version"`
	VersionError   string `json:"version_error,omitempty"`
	Environment    string `json:"environment,omitempty"`
	ManagedVersion string `json:"managed_version,omitempty"`
}

func (s SystemVersion) RegistryVersion() string {
	if s.ManagedVersion != "" {
		return s.ManagedVersion
	}
	return s.Version
}

var versionRe = regexp.MustCompile(`\d+(\.\d+)+`)

// DetectSystem 发现系统中（非 vsm 管理）已安装的语言运行时——这是 vsm 区别于同类工具
// 的核心能力：不止管理“自己装的”，还能主动接管机器上“已经存在的”。
//
// 两路发现、合并去重：
//  1. PATH 扫描：遍历每个 PATH 目录、按平台可执行后缀逐一探测，识别同一语言并存的多个
//     版本（java 8/11/17/21、node v20/v25），并跳过 0 字节的 Windows 应用执行别名。
//  2. 环境变量：从 JAVA_HOME/GOROOT/各 *_HOME 直接定位运行时——即使它根本不在 PATH。
//
// commands 是“工具名 → 候选主命令名列表”的映射；excludeDir 排除 vsm 自己的 shim 目录。
func DetectSystem(commands map[string][]string, excludeDir string) []SystemVersion {
	exclude := normDir(excludeDir)
	dirs := filepath.SplitList(os.Getenv("PATH"))
	exts := executableExts()

	var out []SystemVersion
	seenBin := map[string]bool{} // 同一真实可执行文件只统计一次（PATH 与环境变量合并去重）
	seenKey := map[string]bool{} // 同一工具的同一版本只保留一次

	// 路 1：PATH 扫描
	for tool, names := range commands {
		for _, dir := range dirs {
			if dir == "" {
				continue
			}
			if exclude != "" && pathWithin(exclude, normDir(dir)) {
				continue
			}
			for _, name := range names {
				for _, ext := range exts {
					full := filepath.Join(dir, name+ext)
					info, err := os.Stat(full)
					if err != nil || info.IsDir() || info.Size() == 0 {
						// 0 字节为 Windows“应用执行别名”占位（WindowsApps 下指向商店的重解析点）。
						continue
					}
					real := tool + "|" + binaryIdentity(full)
					if seenBin[real] {
						continue
					}
					ver, probeErr := probeVersion(tool, full)
					sv := describeRuntime(tool, name+ext, full, ver)
					if ver != "" {
						key := tool + "@" + sv.RegistryVersion()
						if seenKey[key] {
							continue
						}
						seenKey[key] = true
					}
					seenBin[real] = true
					if probeErr != nil {
						sv.VersionError = probeErr.Error()
					}
					out = append(out, sv)
				}
			}
		}
	}

	// 路 2：环境变量发现，补充 PATH 之外的安装
	for _, sv := range append(detectFromEnv(commands), detectCondaPython(commands)...) {
		if exclude != "" && pathWithin(exclude, normDir(filepath.Dir(sv.Path))) {
			continue
		}
		real := sv.Tool + "|" + binaryIdentity(sv.Path)
		if seenBin[real] {
			continue
		}
		if sv.Version != "" {
			key := sv.Tool + "@" + sv.RegistryVersion()
			if seenKey[key] {
				continue
			}
			seenKey[key] = true
		}
		seenBin[real] = true
		out = append(out, sv)
	}
	return out
}

// envRuntimeHints 描述“环境变量 → 语言运行时”的定位线索：变量值指向安装根，
// 主命令通常在根目录或其 bin/ 子目录下。
var envRuntimeHints = []struct{ Env, Tool, Cmd string }{
	{"JAVA_HOME", "java", "java"},
	{"JDK_HOME", "java", "java"},
	{"GOROOT", "go", "go"},
	{"NODE_HOME", "node", "node"},
	{"DENO_INSTALL", "deno", "deno"},
	{"BUN_INSTALL", "bun", "bun"},
	{"ZIG", "zig", "zig"},
	{"ZIG_HOME", "zig", "zig"},
	{"PYTHONHOME", "python", "python"},
	{"MAVEN_HOME", "maven", "mvn"},
	{"M2_HOME", "maven", "mvn"},
	{"GRADLE_HOME", "gradle", "gradle"},
	{"KOTLIN_HOME", "kotlin", "kotlinc"},
	{"SCALA_HOME", "scala", "scala"},
	{"GROOVY_HOME", "groovy", "groovy"},
}

// detectFromEnv 依据 envRuntimeHints 从环境变量定位运行时：对每个已设置的变量，
// 在“安装根”与“安装根/bin”下按平台后缀查找主命令，命中则探测版本。
func detectFromEnv(filters ...map[string][]string) []SystemVersion {
	exts := executableExts()
	var out []SystemVersion
	for _, h := range envRuntimeHints {
		if len(filters) > 0 {
			if _, requested := filters[0][h.Tool]; !requested {
				continue
			}
		}
		root := strings.TrimSpace(os.Getenv(h.Env))
		if root == "" {
			continue
		}
		var found string
		for _, base := range []string{root, filepath.Join(root, "bin")} {
			for _, ext := range exts {
				full := filepath.Join(base, h.Cmd+ext)
				if info, err := os.Stat(full); err == nil && !info.IsDir() && info.Size() > 0 {
					found = full
					break
				}
			}
			if found != "" {
				break
			}
		}
		if found == "" {
			continue
		}
		ver, err := probeVersion(h.Tool, found)
		sv := describeRuntime(h.Tool, filepath.Base(found), found, ver)
		if err != nil {
			sv.VersionError = err.Error()
		}
		out = append(out, sv)
	}
	return out
}

// executableExts 返回当前平台下可执行文件的后缀集合。Windows 依据 PATHEXT
// （形如 .COM;.EXE;.BAT;.CMD），其余平台为单个空字符串（命令本身即可执行）。
func executableExts() []string {
	if runtime.GOOS != "windows" {
		return []string{""}
	}
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		raw = ".COM;.EXE;.BAT;.CMD"
	}
	var exts []string
	for _, e := range strings.Split(raw, ";") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		exts = append(exts, strings.ToLower(e))
	}
	if len(exts) == 0 {
		exts = []string{".exe"}
	}
	return exts
}

// realPath 解析符号链接 / junction，失败时退回清理后的路径，用于跨 PATH 条目去重。
func realPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// normDir 归一化目录路径，便于做大小写无关的前缀比较（主要服务 Windows）。
func normDir(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// probeVersion 用给定可执行文件依次尝试常见版本子命令，返回首个匹配到的版本号。
// 覆盖三类约定：`--version`（多数）、`version`（go/zig 等子命令式）、`-version`（java）。
func probeVersion(tool, bin string) (version string, err error) {
	defer func() {
		fields := logging.Fields{"tool": tool, "version": version}
		level := "debug"
		if err != nil {
			level = "warn"
			fields["error"] = err.Error()
		}
		logging.Record(level, "detect", "version.probe", fields)
	}()

	if tool == "go" {
		return probeGoVersion(bin)
	}
	if tool == "maven" {
		if version, err := probeMavenMetadata(bin); err == nil {
			return version, nil
		}
	}
	flags := []string{"--version", "version", "-version"}
	if tool == "maven" {
		flags = []string{"--version", "-version"}
	}
	if tool == "python" {
		flags = []string{"--version", "-V"}
	}
	if tool == "java" {
		flags = []string{"-version", "--version"}
	}
	var lastErr error
	for _, flag := range flags {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		c := exec.CommandContext(ctx, bin, flag)
		mcpserver.ConfigureProcess(c)
		c.WaitDelay = 250 * time.Millisecond
		if err := configureVersionProbe(c, bin, flag); err != nil {
			cancel()
			return "", err
		}
		if tool == "python" {
			root := filepath.Dir(bin)
			if filepath.Base(root) == "bin" || strings.EqualFold(filepath.Base(root), "Scripts") {
				root = filepath.Dir(root)
			}
			p, _ := plugin.Get("python")
			c.Env = buildEnv(p.BinPaths(root), p.ExecEnv(root))
			clean := c.Env[:0]
			for _, entry := range c.Env {
				key, _, _ := strings.Cut(entry, "=")
				if !strings.EqualFold(key, "PYTHONHOME") && !strings.EqualFold(key, "PYTHONPATH") {
					clean = append(clean, entry)
				}
			}
			c.Env = clean
		}
		var output probeOutput
		c.Stdout, c.Stderr = &output, &output
		err := c.Run()
		if ctx.Err() != nil {
			err = fmt.Errorf("版本探测超时")
		}
		cancel()
		if err != nil {
			message := strings.Join(strings.Fields(output.text), " ")
			if len(message) > 512 {
				message = message[:512]
			}
			lastErr = fmt.Errorf("%s: %w；%s", flag, err, message)
			continue
		}
		text := probeANSIEscape.ReplaceAllString(output.text, "")
		if pattern := specificProbePatterns[tool]; pattern != nil {
			if match := pattern.FindStringSubmatch(text); match != nil {
				return match[1], nil
			}
			lastErr = fmt.Errorf("%s 未返回 %s 自身的版本信息", flag, tool)
			continue
		}
		if m := versionRe.FindString(text); m != "" {
			return m, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("版本命令未返回可识别的版本号")
}

var probeANSIEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
var specificProbePatterns = map[string]*regexp.Regexp{
	"maven":  regexp.MustCompile(`(?m)^Apache Maven\s+(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`),
	"python": regexp.MustCompile(`(?m)^Python\s+(\d+\.\d+(?:\.\d+)?(?:(?:a|b|rc)\d+)?)`),
}

type probeOutput struct{ text string }

func (w *probeOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := (64 << 10) - len(w.text); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		w.text += string(p)
	}
	return n, nil
}

// DetectEnvVars 返回与语言相关、当前已设置的环境变量（用于诊断已有安装）。
func DetectEnvVars() map[string]string {
	keys := []string{
		"GOROOT", "GOPATH", "JAVA_HOME", "JDK_HOME", "PYTHONHOME",
		"NODE_HOME", "DENO_INSTALL", "BUN_INSTALL", "ZIG", "ZIG_HOME",
		"MAVEN_HOME", "M2_HOME", "GRADLE_HOME", "KOTLIN_HOME", "SCALA_HOME",
		"GROOVY_HOME", "CARGO_HOME", "RUSTUP_HOME", "PYENV_ROOT", "DOTNET_ROOT",
	}
	out := map[string]string{}
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			out[k] = v
		}
	}
	return out
}
