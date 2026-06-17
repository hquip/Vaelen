// Package plugin 定义语言插件的统一抽象。核心引擎不内置任何语言知识，
// 所有“某语言怎么下载、装到哪、需要哪些环境变量”都由实现 Plugin 的插件提供。
//
// MVP 阶段插件用 Go 内置实现（golang.go / node.go）；后续可平滑替换为
// Lua（gopher-lua）等外部插件运行时，接口保持不变。
package plugin

import "runtime"

// Plugin 是单个语言版本管理插件需要实现的契约。
type Plugin interface {
	// Name 返回工具名，如 "go" / "node"。
	Name() string
	// ListAll 返回所有可安装的版本号（不含前缀，如 "1.22.0"），新版在前。
	ListAll() ([]string, error)
	// LatestStable 返回最新稳定版本号。
	LatestStable() (string, error)
	// DownloadURL 给定版本与目标平台，返回下载地址。
	DownloadURL(version string, target Target) (string, error)
	// BinPaths 返回安装目录下需要加入 PATH 的子目录（绝对路径）。
	BinPaths(installDir string) []string
	// ExecEnv 返回需要额外设置的环境变量，如 GOROOT。
	ExecEnv(installDir string) map[string]string
}

// Checksummer 是可选接口：实现它的插件，安装时会对下载包做 sha256 完整性校验。
// 返回空字符串表示该版本无可用校验值（跳过校验）。
type Checksummer interface {
	Checksum(version string, t Target) (string, error)
}

// Target 描述目标平台，命名沿用 Go 的 GOOS/GOARCH。
type Target struct {
	OS   string // windows / linux / darwin
	Arch string // amd64 / arm64
}

// CurrentTarget 返回当前运行平台。
func CurrentTarget() Target {
	return Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
}
