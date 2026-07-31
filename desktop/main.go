package main

import (
	"embed"
	"log"
	"os"

	desktopplatform "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform/wails"
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/services"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var trayIcon []byte

func main() {
	if hasArgument(os.Args[1:], "--recover-network") {
		if err := services.RecoverSystemProxy(); err != nil {
			log.Printf("recover HypoMux system proxy: %v", err)
		}
		return
	}
	if !desktopplatform.WebView2Available() {
		desktopplatform.ShowWebView2MissingMessage()
		return
	}
	var mainWindow application.Window
	app := application.New(application.Options{
		Name:        "HypoMux",
		Description: "Multi-link network aggregation desktop client",
		Icon:        trayIcon,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.hypomux.desktop",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				for _, argument := range data.Args {
					if argument == "--silent" {
						return
					}
				}
				if mainWindow != nil {
					mainWindow.Show().Focus()
				}
			},
		},
	})

	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "main",
		Title:           "HypoMux",
		Width:           1120,
		Height:          800,
		MinWidth:        960,
		MinHeight:       680,
		Frameless:       true,
		InitialPosition: application.WindowCentered,
		// Creating WebView2 as translucent can fail on some preview Windows /
		// WebView2 combinations. Start transparent, then apply DWM material only
		// after the native window and controller are ready.
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
		Windows: application.WindowsWindow{
			BackdropType:                      application.Mica,
			Theme:                             application.SystemDefault,
			DisableFramelessWindowDecorations: false,
		},
	})

	settingsService := services.NewSettingsService()
	adapterService := services.NewAdapterService(settingsService)
	supportLogs := services.NewSupportLogStore()
	tunService := services.NewTunService(settingsService, adapterService)
	blockedDomainService := services.NewBlockedDomainService(settingsService)
	updaterService := services.NewUpdaterService()
	engineService := services.NewEngineServiceWithDomains(
		settingsService, adapterService, blockedDomainService, supportLogs,
	)
	var diagnosticsService *services.DiagnosticsService
	desktop := wails.NewDesktopHost(app, mainWindow, func() {
		if diagnosticsService != nil {
			diagnosticsService.Shutdown()
		}
		engineService.Shutdown()
	}, func() bool {
		return settingsService.Get().CloseToTray
	})
	diagnosticsService = services.NewDiagnosticsService(
		settingsService, adapterService, desktop, supportLogs,
	)
	routingService := services.NewRoutingRuleService(settingsService, adapterService, desktop)
	app.RegisterService(application.NewService(desktop))
	app.RegisterService(application.NewService(settingsService))
	app.RegisterService(application.NewService(adapterService))
	app.RegisterService(application.NewService(engineService))
	app.RegisterService(application.NewService(routingService))
	app.RegisterService(application.NewService(diagnosticsService))
	app.RegisterService(application.NewService(tunService))
	app.RegisterService(application.NewService(blockedDomainService))
	app.RegisterService(application.NewService(updaterService))
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
		desktop.SetWindowMaterial("mica")
		for _, argument := range os.Args[1:] {
			if argument == "--silent" {
				desktop.HideToTray()
				break
			}
		}
	})
	desktop.ConfigureTray(trayIcon)
	desktop.ConfigureCloseToTray()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func hasArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}
