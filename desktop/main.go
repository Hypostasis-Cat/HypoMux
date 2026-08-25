package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/engineclient"
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
	if hasArgument(os.Args[1:], "--core-service-self-test") {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := runCoreServiceSelfTest(ctx); err != nil {
			log.Printf("Core Service self-test failed: %v", err)
			os.Exit(1)
		}
		return
	}
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
		log.Printf(
			"desktop privilege compatibility fallback: proxy_safe=%t detail=%s",
			launchSecurity.ProxyCompatible,
			launchSecurity.Detail,
		)
	}
	if !desktopplatform.WebView2Available() {
		desktopplatform.ShowWebView2MissingMessage()
		return
	}
	var mainWindow application.Window
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

	settingsService := services.NewSettingsService()
	if err := settingsService.StartupError(); err != nil {
		desktopplatform.ShowErrorMessage(
			"HypoMux - 配置加载失败",
			fmt.Sprintf(
				"无法安全加载设置文件：\n%s\n\n%v\n\n为避免覆盖现有配置，HypoMux 已停止启动。请备份并修复或重命名该文件后重试。",
				settingsService.ConfigPath(),
				err,
			),
		)
		return
	}
	adapterService := services.NewAdapterService(settingsService)
	supportLogs := services.NewSupportLogStore()
	tunService := services.NewTunService(settingsService, adapterService)
	blockedDomainService := services.NewBlockedDomainService(settingsService)
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
	desktop := wails.NewDesktopHost(app, mainWindow, startSilent, func() {
		if diagnosticsService != nil {
			diagnosticsService.Shutdown()
		}
		engineService.Shutdown()
	}, func() bool {
		return settingsService.Get().CloseToTray
	})
	updaterService := services.NewUpdaterService(desktop.Quit)
	diagnosticsService = services.NewDiagnosticsService(
		settingsService, adapterService, desktop, supportLogs,
		func() error {
			snapshot, err := engineService.Snapshot()
			if err != nil {
				return nil
			}
			switch snapshot.Phase {
			case "starting", "running", "degraded", "stopping":
				return fmt.Errorf("请先停止聚合再进行 NAT 类型检测；聚合运行时无法保证检测流量直连所选物理网卡")
			default:
				return nil
			}
		},
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

func runCoreServiceSelfTest(ctx context.Context) error {
	client := engineclient.New()
	defer client.Kill()
	hello, err := client.EnsureElevated(ctx)
	if err != nil {
		return err
	}
	if !hello.Elevated {
		return fmt.Errorf("Core Service 未报告管理员身份")
	}
	if hello.Launcher != "service" || hello.Fallback {
		return fmt.Errorf("连接未使用已安装 Core Service（launcher=%s fallback=%t）", hello.Launcher, hello.Fallback)
	}
	for _, method := range []string{"engine.status", "health.check", "engine.status"} {
		var response map[string]any
		if err := client.Request(ctx, method, nil, &response); err != nil {
			return fmt.Errorf("%s：%w", method, err)
		}
	}
	return nil
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
