package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// SystemVersion 描述在系统中（非 vsm 管理）发现的某个语言运行时。
type SystemVersion struct {
	Tool    string `json:"tool"`
	Command string `json:"command"`
	Path    string `json:"path"`
	Version string `json:"version"`
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
			if exclude != "" && strings.HasPrefix(normDir(dir), exclude) {
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
					real := strings.ToLower(realPath(full))
					if seenBin[real] {
						continue
					}
					ver := probeVersion(full)
					if ver != "" {
						key := tool + "@" + ver
						if seenKey[key] {
							continue
						}
						seenKey[key] = true
					}
					seenBin[real] = true
					out = append(out, SystemVersion{Tool: tool, Command: name + ext, Path: full, Version: ver})
				}
			}
		}
	}

	// 路 2：环境变量发现，补充 PATH 之外的安装
	for _, sv := range detectFromEnv() {
		real := strings.ToLower(realPath(sv.Path))
		if seenBin[real] {
			continue
		}
		if sv.Version != "" {
			key := sv.Tool + "@" + sv.Version
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
func detectFromEnv() []SystemVersion {
	exts := executableExts()
	var out []SystemVersion
	for _, h := range envRuntimeHints {
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
		out = append(out, SystemVersion{Tool: h.Tool, Command: filepath.Base(found), Path: found, Version: probeVersion(found)})
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
	return strings.ToLower(filepath.Clean(p))
}

// probeVersion 用给定可执行文件依次尝试常见版本子命令，返回首个匹配到的版本号。
// 覆盖三类约定：`--version`（多数）、`version`（go/zig 等子命令式）、`-version`（java）。
func probeVersion(bin string) string {
	for _, args := range [][]string{{"--version"}, {"version"}, {"-version"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		c := exec.CommandContext(ctx, bin, args...)
		hideConsole(c)
		out, _ := c.CombinedOutput()
		cancel()
		if m := versionRe.FindString(string(out)); m != "" {
			return m
		}
	}
	return ""
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
