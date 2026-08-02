package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"time"

	desktopplatform "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform"
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform/wails"
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/services"
	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/startup"
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
	launchSecurity := startup.PrepareDesktopLaunch(os.Args[1:])
	if launchSecurity.Relaunched {
		return
	}
	if launchSecurity.Elevated {
		desktopplatform.ShowElevationCompatibilityMessage(
			launchSecurity.ProxyCompatible,
			launchSecurity.Detail,
		)
	}
	// Clean up zombie processes and stale network state from previous crashed sessions.
	// This prevents "port already in use" and TUN adapter conflicts.
	// Equivalent to Python version's force_evict_zombie_backends from main.py:83-108.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
	if err := startup.CleanupZombieProcesses(cleanupCtx); err != nil {
		log.Printf("startup cleanup: %v", err)
		// Non-fatal: continue startup even if cleanup fails
	}
	cleanupCancel()

	if !desktopplatform.WebView2Available() {
		desktopplatform.ShowWebView2MissingMessage()
		return
	}
	var mainWindow application.Window
	var trayMenuWindow application.Window
	startSilent := hasArgument(os.Args[1:], "--silent")
	appearanceService := services.NewAppearanceService()
	appearanceBackground := services.NewAppearanceBackgroundHandler(appearanceService)
	frontendAssets := application.AssetFileServerFS(assets)
	app := application.New(application.Options{
		Name:        "HypoMux",
		Description: "HypoMux",
		Icon:        trayIcon,
		Assets: application.AssetOptions{
			Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path == services.AppearanceBackgroundPath {
					appearanceBackground.ServeHTTP(response, request)
					return
				}
				frontendAssets.ServeHTTP(response, request)
			}),
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
		Hidden:          true,
		InitialPosition: application.WindowCentered,
		// Wails only applies Windows backdrops after WebView2 is ready when the
		// window uses its translucent composition path. Using Transparent here
		// leaves the first launch on an uninitialised fallback until a later
		// appearance change happens to reapply the DWM material.
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
		Windows: application.WindowsWindow{
			BackdropType:                      application.None,
			Theme:                             application.SystemDefault,
			DisableFramelessWindowDecorations: false,
		},
	})

	// Create tray menu window - small, frameless, transparent, always on top
	trayMenuWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "tray-menu",
		Title:            "HypoMux Tray Menu",
		Width:            260,
		Height:           226,
		Frameless:        true,
		Hidden:           true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		HideOnFocusLost:  true,
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/tray-menu.html",
		Windows: application.WindowsWindow{
			BackdropType:                      application.Acrylic,
			Theme:                             application.Light,
			DisableFramelessWindowDecorations: true,
			HiddenOnTaskbar:                   true,
		},
	})

	settingsService := services.NewSettingsService()
	adapterService := services.NewAdapterService(settingsService)
	supportLogs := services.NewSupportLogStore()
	tunService := services.NewTunService(settingsService, adapterService)
	blockedDomainService := services.NewBlockedDomainService(settingsService)
	updaterService := services.NewUpdaterService()
	engineService := services.NewEngineServiceWithDomainsAndHostPrivilege(
		settingsService,
		adapterService,
		blockedDomainService,
		services.HostPrivilegeCompatibility{
			Elevated:  launchSecurity.Elevated,
			ProxySafe: launchSecurity.ProxyCompatible,
			Detail:    launchSecurity.Detail,
		},
		supportLogs,
	)
	var diagnosticsService *services.DiagnosticsService
	desktop := wails.NewDesktopHost(app, mainWindow, trayMenuWindow, startSilent, func() {
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
	app.RegisterService(application.NewService(appearanceService))
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
		if startSilent {
			desktop.HideToTray()
			startupSettings := settingsService.Get()
			if shouldAutoStartAcceleration(startSilent, startupSettings) {
				go func() {
					if err := runAutoStartAcceleration(
						startupSettings,
						engineService.Start,
						desktop.SetEngineTrayStatus,
					); err != nil {
						log.Printf("auto-start HypoMux acceleration: %v", err)
					}
				}()
			}
			return
		}
		// The frontend normally reveals the fully rendered window. Keep a
		// fallback so a JavaScript startup failure never leaves the app invisible.
		time.AfterFunc(4*time.Second, desktop.ShowStartup)
	})
	desktop.ConfigureTray(trayIcon)
	desktop.ConfigureCloseToTray()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func shouldAutoStartAcceleration(startSilent bool, settings services.AppSettings) bool {
	return startSilent && settings.Autostart && settings.AutoStartEngine
}

func runAutoStartAcceleration(
	settings services.AppSettings,
	start func(string) (services.EngineSnapshot, error),
	setStatus func(string, string),
) error {
	setStatus("starting", settings.Mode)
	snapshot, err := start(settings.Mode)
	if err != nil {
		setStatus("failed", settings.Mode)
		return err
	}
	setStatus(snapshot.Phase, snapshot.Mode)
	return nil
}

func hasArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}
