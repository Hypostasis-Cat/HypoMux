import { FluentProvider, Spinner } from "@fluentui/react-components";
import { lazy, Suspense, useEffect, useRef, useState } from "react";
import "./theme/design.tokens.css";
import "./theme/material.tokens.css";
import "./theme/semantic.tokens.css";
import "./theme/typography.tokens.css";
import "./theme/motion.tokens.css";
import "./app.css";
import "./theme/controls.css";
import { WallpaperLayer } from "./components/material/WallpaperLayer";
import { AppShell } from "./components/shell/AppShell";
import { LaunchOverlay } from "./components/shell/LaunchOverlay";
import type { AppPage } from "./components/shell/CompactNavigation";
import { HomePage } from "./pages/HomePage";
import { advanceConnectionsNavigation } from "./pages/connectionNavigation";
import type { EnginePhase, HomeAdapter } from "./state/useEngineState";
import { AppearanceProvider, useAppearance } from "./theme/appearance.store";
import { MotionProvider } from "./theme/MotionProvider";
import { LanguageProvider, useI18n } from "./i18n/i18n";
import { desktopPlatform } from "./platform/desktop";
import { resolveWallpaperBackground } from "./theme/wallpaper";
import { TrayMenu } from "./components/tray/TrayMenu";
import {
  AppNotificationCenter,
  AppNotificationProvider,
  useAppNotifications,
} from "./components/notifications/AppNotifications";

const AppearanceLab = lazy(() => import("./pages/AppearanceLab").then((module) => ({ default: module.AppearanceLab })));
const AboutPage = lazy(() => import("./pages/AboutPage").then((module) => ({ default: module.AboutPage })));
const BlockedDomainsPage = lazy(() => import("./pages/BlockedDomainsPage").then((module) => ({ default: module.BlockedDomainsPage })));
const HealthPage = lazy(() => import("./pages/HealthPage").then((module) => ({ default: module.HealthPage })));
const ConnectionsPage = lazy(() => import("./pages/ConnectionsPage").then((module) => ({ default: module.ConnectionsPage })));
const RoutingPage = lazy(() => import("./pages/RoutingPage").then((module) => ({ default: module.RoutingPage })));
const ToolsPage = lazy(() => import("./pages/ToolsPage").then((module) => ({ default: module.ToolsPage })));
const SettingsPage = lazy(() => import("./pages/SettingsPage").then((module) => ({ default: module.SettingsPage })));

function NotificationVisualFixture() {
  const { notify } = useAppNotifications();
  const shown = useRef(false);

  useEffect(() => {
    const fixture = new URLSearchParams(window.location.search).get("notification");
    if (shown.current || !fixture) return;
    shown.current = true;
    if (fixture === "success") {
      notify({
        title: "加速已启动",
        message: "2 条链路已加入系统代理加速。",
        intent: "success",
        dedupeKey: "visual-qa:success",
      });
      return;
    }
    if (fixture === "queue") {
      notify({ title: "网卡已重新扫描", message: "发现 2 张可用网卡。", intent: "info", timeout: 0, dedupeKey: "visual-qa:queue-info" });
      notify({ title: "规则已保存", message: "分流规则已立即生效。", intent: "success", timeout: 0, dedupeKey: "visual-qa:queue-success" });
      notify({ title: "WLAN 信号较弱", message: "该链路的权重已临时降低。", intent: "warning", timeout: 0, dedupeKey: "visual-qa:queue-warning" });
      return;
    }
    notify({
      title: "操作未完成",
      message: "stage=tun_data_path endpoint=http://www.msftconnecttest.com/connecttest.txt, outbound=windows-tun: curl: (28) Resolving timed out after 4007 milliseconds",
      intent: "error",
      action: { label: "重试", onClick: () => undefined },
      dedupeKey: "visual-qa:error",
    });
  }, [notify]);

  return null;
}

function HypoMuxWindow() {
  const { locale } = useI18n();
  const [page, setPage] = useState<AppPage>(() => {
    const requested = new URLSearchParams(window.location.search).get("page");
    if (import.meta.env.DEV && (
      requested === "assistant" || requested === "tools" || requested === "appearance" || requested === "routing" ||
      requested === "health" || requested === "connections" || requested === "settings" ||
      requested === "blocked-domains" || requested === "about"
    )) {
      return requested;
    }
    return "home";
  });
  const [pageDirection, setPageDirection] = useState<"forward" | "backward">("forward");
  const [navigationRevision, setNavigationRevision] = useState(0);
  const [connectionsNavigation, setConnectionsNavigation] = useState({ adapter: "", revision: 0 });
  const [connectionAdapters, setConnectionAdapters] = useState<HomeAdapter[] | undefined>(undefined);
  const [enginePhase, setEnginePhase] = useState<EnginePhase | undefined>(undefined);
  const [startupRevealed, setStartupRevealed] = useState(false);
  // The launch handoff slides the first page in, like a navigation would.
  const [launchEntered, setLaunchEntered] = useState(false);
  const { fluentTheme, settings } = useAppearance();
  const pageOrder: AppPage[] = [
    "home",
    "assistant",
    "routing",
    "health",
    "connections",
    "tools",
    "settings",
    "blocked-domains",
    "about",
    "appearance",
  ];

  const navigate = (nextPage: AppPage, adapterName?: string) => {
    if (nextPage === "connections") {
      setConnectionsNavigation((current) => advanceConnectionsNavigation(current, adapterName));
    }
    if (nextPage === page) return;
    const currentIndex = pageOrder.indexOf(page);
    const nextIndex = pageOrder.indexOf(nextPage);
    setPageDirection(nextIndex >= currentIndex ? "forward" : "backward");
    setNavigationRevision((current) => current + 1);
    setPage(nextPage);
  };

  useEffect(() => {
    let firstFrame = 0;
    let secondFrame = 0;
    void desktopPlatform.showStartup().finally(() => {
      firstFrame = window.requestAnimationFrame(() => {
        secondFrame = window.requestAnimationFrame(() => setStartupRevealed(true));
      });
    });
    return () => {
      window.cancelAnimationFrame(firstFrame);
      window.cancelAnimationFrame(secondFrame);
    };
  }, []);

  return (
    <FluentProvider theme={fluentTheme} className="hypomux-provider">
      <AppNotificationProvider>
        {import.meta.env.DEV && <NotificationVisualFixture />}
        <LaunchOverlay ready={startupRevealed} onEnter={() => setLaunchEntered(true)} />
        <WallpaperLayer />
        <AppNotificationCenter wallpaperBackground={resolveWallpaperBackground(settings)} />
        <AppShell
          page={page}
          onPageChange={navigate}
          pageDirection={pageDirection}
          animatePage={navigationRevision > 0 || launchEntered}
          persistentPage="home"
          persistentChildren={(
            <HomePage
              onNavigate={navigate}
              onAdapterRuntimeChange={setConnectionAdapters}
              onEnginePhaseChange={setEnginePhase}
            />
          )}
          renderPage={target => (
          <Suspense fallback={<div className="page-loading"><Spinner size="small" label={locale === "en" ? "Loading page…" : "正在加载页面…"} /></div>}>
          {target === "appearance" && import.meta.env.DEV
            ? <AppearanceLab />
            : target === "about"
              ? <AboutPage />
              : target === "tools"
                ? <ToolsPage />
              : target === "settings"
                ? <SettingsPage
                  adapterRuntime={connectionAdapters}
                  onOpenBlockedDomains={() => navigate("blocked-domains")}
                />
                : target === "blocked-domains"
                  ? <BlockedDomainsPage onBack={() => navigate("settings")} />
            : target === "health"
              ? <HealthPage adapterRuntime={connectionAdapters} enginePhase={enginePhase} />
              : target === "connections"
                ? <ConnectionsPage
                  initialAdapter={connectionsNavigation.adapter}
                  adapterRevision={connectionsNavigation.revision}
                  adapterRuntime={connectionAdapters ?? []}
                />
            : target === "routing"
              ? <RoutingPage />
              : null}
          </Suspense>
          )}
        />
      </AppNotificationProvider>
    </FluentProvider>
  );
}

export default function App() {
  if (new URLSearchParams(window.location.search).get("tray") === "1") return <TrayMenu />;
  return (
    <LanguageProvider>
      <AppearanceProvider>
        <MotionProvider>
          <HypoMuxWindow />
        </MotionProvider>
      </AppearanceProvider>
    </LanguageProvider>
  );
}
