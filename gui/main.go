// Command vsm-gui 是 vsm 的桌面 GUI（Wails v2）。
//
// 平台说明：
//   - Windows：走 go-webview2（内置 WebView2 运行时），免 CGO，可直接交叉编译。
//   - macOS / Linux：Wails 依赖系统 WebView（WKWebView / WebKit2GTK）与 CGO，
//     必须在对应平台上本地编译（见 .github/workflows/release.yml 的矩阵）。
//
// 它只是一个「壳」：所有版本管理逻辑都复用 internal/core 与 internal/plugin，
// 与 CLI 完全共享同一套核心，因此三个平台的 GUI 行为一致。
package main

import (
	"embed"
	"runtime"
	"vsm/internal/buildinfo"
	"vsm/internal/logging"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon 是窗口/应用图标（macOS 的 Dock 与「关于」面板会用到）。
// 与 Windows 的 .syso 资源对应，让三端图标保持一致。
//
//go:embed appicon.png
var appIcon []byte

const (
	appTitle  = "VSM · 多语言版本管理器"
	appWidth  = 960
	appHeight = 680
	minWidth  = 720
	minHeight = 520
)

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     appTitle + " · " + buildinfo.DisplayVersion(),
		Width:     appWidth,
		Height:    appHeight,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 18, B: 23, A: 1},
		Menu:             appMenu(),
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Mac: &mac.Options{
			// 深色外观与界面默认主题一致，避免启动瞬间白屏闪烁
			Appearance:           mac.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			// 隐藏标题栏只留红绿灯按钮，界面更贴近原生 macOS 观感
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   appTitle + " · " + buildinfo.DisplayVersion(),
				Message: "纯 Go 编写的多语言版本管理器\nhttps://github.com/hquip/Vaelen",
				Icon:    appIcon,
			},
		},
	})
	if err != nil {
		logging.Record("error", "gui", "app.failed", logging.Fields{"error": err.Error()})
		println("Error:", err.Error())
	}
}

// appMenu 返回 macOS 的应用菜单；其它平台返回 nil（不显示菜单栏，保持原有界面）。
//
// macOS 上没有「编辑」菜单时，WebView 里的输入框无法使用 ⌘C / ⌘V / ⌘A
// （比如粘贴代理地址），也没有 ⌘Q 退出——这些快捷键都由菜单项提供。
func appMenu() *menu.Menu {
	if runtime.GOOS != "darwin" {
		return nil
	}
	m := menu.NewMenu()
	m.Append(menu.AppMenu())    // 关于 / 隐藏 / 退出（⌘Q）
	m.Append(menu.EditMenu())   // 撤销 / 剪切 / 复制 / 粘贴 / 全选
	m.Append(menu.WindowMenu()) // 最小化（⌘M）/ 缩放
	return m
}
