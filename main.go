package main

import (
	"context"
	"embed"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"log"
	"os"
	"path/filepath"
	"res-downloader/core"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

//go:embed wails.json
var wailsJson string

func main() {
	// Create an instance of the app structure
	app := core.GetApp(assets, wailsJson)
	bind := core.NewBind()
	isMac := runtime.GOOS == "darwin"
	// menu
	appMenu := menu.NewMenu()
	if isMac {
		appMenu.Append(menu.AppMenu())
		appMenu.Append(menu.EditMenu())
		appMenu.Append(menu.WindowMenu())
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:                    app.AppName,
		SingleInstanceLock:       &options.SingleInstanceLock{UniqueId: "d5da932e-87b1-43b7-9f4f-39ae5e7e4563", OnSecondInstanceLaunch: func(_ options.SecondInstanceData) { app.ShowWindow() }},
		Width:                    1280,
		MinWidth:                 960,
		Height:                   800,
		MinHeight:                600,
		Frameless:                !isMac,
		Menu:                     appMenu,
		EnableDefaultContextMenu: true,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: core.Middleware,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup: func(ctx context.Context) {
			logo := `
	 _ __    ___   ___            __| |   ___   __      __  _ __   | |   ___     __ _     __| |   ___   _ __
	| '__|  / _ \ / __|  _____   / _· |  / _ \  \ \ /\ / / | '_ \  | |  / _ \   / _· |   / _· |  / _ \ | ·__|
	| |    |  __/ \__ \ |_____| | (_| | | (_) |  \ V  V /  | | | | | | | (_) | | (_| |  | (_| | |  __/ | |
	|_|     \___| |___/          \__,_|  \___/    \_/\_/   |_| |_| |_|  \___/   \__ ,_|  \__,_|  \___| |_|`

			log.Println(logo)
			fmt.Println("version:", app.Version)
			fmt.Println("lockfile:", app.LockFile)
			app.Startup(ctx)
		},
		OnDomReady: func(ctx context.Context) { wailsruntime.WindowShow(ctx) },
		OnShutdown: func(ctx context.Context) {
			app.OnExit()
		},
		Bind: []interface{}{
			bind,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   fmt.Sprintf("%s %s", app.AppName, app.Version),
				Message: app.Description + app.Copyright,
				Icon:    icon,
			},
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WebviewUserDataPath:               isolatedWebviewPath(),
			WindowIsTranslucent:               false,
			DisableFramelessWindowDecorations: false,
		},
		Linux: &linux.Options{
			ProgramName:         app.AppName,
			Icon:                icon,
			WebviewGpuPolicy:    linux.WebviewGpuPolicyOnDemand,
			WindowIsTranslucent: true,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

func isolatedWebviewPath() string {
	if directory := os.Getenv("RESD_CONFIG_DIR"); directory != "" {
		return filepath.Join(directory, "webview")
	}
	return ""
}
