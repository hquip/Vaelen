//go:build darwin

package main

// Wails 的 macOS 文件对话框代码使用了 UTType（UniformTypeIdentifiers 框架，macOS 11+），
// 但它自己的 cgo 指令没有声明链接这个框架：`wails build` 会往 CGO_LDFLAGS 里自动补上，
// 而本项目用的是普通 `go build`，于是链接时报
//
//	Undefined symbols for architecture arm64: "_OBJC_CLASS_$_UTType"
//
// v1.0.0 的 macOS GUI 就是因此一直编译失败（CI 的 continue-on-error 把失败显示成了成功）。
// 在这里声明后，`go build ./gui` 在任何 Mac 上都能直接链接成功，无需额外环境变量。

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
