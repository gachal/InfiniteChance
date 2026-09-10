// Command desktop runs InfiniteChance as a native desktop app (Wails v3):
// gateway + canvas in-process on SQLite, canvas as the main window, the
// admin console as an on-demand configuration window (CONTEXT.md 桌面版).
package main

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	wailsApp := application.New(application.Options{
		Name:        "InfiniteChance",
		Description: "Token gateway + infinite canvas, on your desktop.",
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	dt, err := NewDesktop()
	if err != nil {
		errorWindow(wailsApp, err)
		_ = wailsApp.Run()
		return
	}
	// 端口被占等启动失败在这里同步浮现(共识 Q6),以错误窗口明示。
	if err := dt.Start(); err != nil {
		errorWindow(wailsApp, err)
		_ = wailsApp.Run()
		return
	}
	wailsApp.OnShutdown(dt.Shutdown)

	devTools := os.Getenv("INFINITECHANCE_DEV") != ""

	// 画布主窗口;首启(还没有管理员账号)时画布自己的 /init 引导就绪,
	// 同时自动带开管理台窗口方便走完向导。
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "canvas",
		Title:           "InfiniteChance",
		Width:           1440,
		Height:          900,
		MinWidth:        960,
		MinHeight:       640,
		URL:             devOverride("INFINITECHANCE_DEV_CANVAS", dt.CanvasURL()),
		DevToolsEnabled: devTools,
	})
	if !dt.Initialized() {
		openAdminWindow(wailsApp, dt, devTools)
	}

	// 应用菜单:管理台随时可开(配置渠道、key、价格、模板、审计)。
	menu := application.NewMenu()
	menu.Add("管理台").OnClick(func(*application.Context) {
		openAdminWindow(wailsApp, dt, devTools)
	})
	wailsApp.Menu.SetApplicationMenu(menu)

	_ = wailsApp.Run()
}

// openAdminWindow opens the admin console window or is a no-op when it is
// already open — 配置窗口按需存在,不常驻。
func openAdminWindow(wailsApp *application.App, dt *Desktop, devTools bool) {
	if _, ok := wailsApp.Window.GetByName("admin"); ok {
		return
	}
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "admin",
		Title:           "InfiniteChance 管理台",
		Width:           1280,
		Height:          820,
		MinWidth:        960,
		MinHeight:       640,
		URL:             devOverride("INFINITECHANCE_DEV_ADMIN", dt.GatewayURL()),
		DevToolsEnabled: devTools,
	})
}

func errorWindow(wailsApp *application.App, err error) {
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "InfiniteChance 启动失败",
		HTML: fmt.Sprintf(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<style>body{font-family:-apple-system,'Segoe UI',sans-serif;background:#1c1c1e;color:#eee;
display:grid;place-items:center;height:100vh;margin:0}main{max-width:36em;padding:2em;
line-height:1.7}code{background:#333;padding:.2em .4em;border-radius:4px}h1{font-size:1.2em}</style>
</head><body><main><h1>启动失败</h1><p>%s</p>
<p>常见原因:端口被其他程序占用(可在应用数据目录的 config.json 修改 gateway_port/canvas_port),
或数据目录不可写。</p></main></body></html>`, err),
		Width:  640,
		Height: 360,
	})
}

// devOverride lets `make dev-desktop` point windows at the vite dev servers
// (hot reload) while the Go services keep running from this binary.
func devOverride(envKey, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}
