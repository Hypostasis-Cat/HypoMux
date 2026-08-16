import { FluentProvider, Spinner } from "@fluentui/react-components";
import { lazy, Suspense, useEffect, useState } from "react";
import "./theme/design.tokens.css";
import "./theme/material.tokens.css";
import "./theme/semantic.tokens.css";
import "./theme/typography.tokens.css";
import "./theme/motion.tokens.css";
import "./app.css";
import { WallpaperLayer } from "./components/material/WallpaperLayer";
import { AppShell } from "./components/shell/AppShell";
import type { AppPage } from "./components/shell/CompactNavigation";
import { HomePage } from "./pages/HomePage";
import { AppearanceProvider, useAppearance } from "./theme/appearance.store";
import { LanguageProvider } from "./i18n/i18n";
import { desktopPlatform } from "./platform/desktop";

const AppearanceLab = lazy(() => import("./pages/AppearanceLab").then((module) => ({ default: module.AppearanceLab })));
const AboutPage = lazy(() => import("./pages/AboutPage").then((module) => ({ default: module.AboutPage })));
const BlockedDomainsPage = lazy(() => import("./pages/BlockedDomainsPage").then((module) => ({ default: module.BlockedDomainsPage })));
const HealthPage = lazy(() => import("./pages/HealthPage").then((module) => ({ default: module.HealthPage })));
const ConnectionsPage = lazy(() => import("./pages/ConnectionsPage").then((module) => ({ default: module.ConnectionsPage })));
const RoutingPage = lazy(() => import("./pages/RoutingPage").then((module) => ({ default: module.RoutingPage })));
const SettingsPage = lazy(() => import("./pages/SettingsPage").then((module) => ({ default: module.SettingsPage })));

function HypoMuxWindow() {
  const [page, setPage] = useState<AppPage>(() => {
    const requested = new URLSearchParams(window.location.search).get("page");
    if (import.meta.env.DEV && (
      requested === "appearance" || requested === "routing" ||
      requested === "health" || requested === "connections" || requested === "settings" ||
      requested === "blocked-domains" || requested === "about"
    )) {
      return requested;
    }
    return "home";
  });
  const [pageDirection, setPageDirection] = useState<"forward" | "backward">("forward");
  const [navigationRevision, setNavigationRevision] = useState(0);
  const [startupRevealed, setStartupRevealed] = useState(false);
  const { fluentTheme } = useAppearance();
  const pageOrder: AppPage[] = [
    "home",
    "routing",
    "health",
    "connections",
    "settings",
    "blocked-domains",
    "about",
    "appearance",
  ];

  const navigate = (nextPage: AppPage) => {
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
      <div className={`startup-reveal${startupRevealed ? " is-ready" : ""}`} aria-hidden="true" />
      <WallpaperLayer />
      <AppShell
        page={page}
        onPageChange={navigate}
        pageDirection={pageDirection}
        animatePage={navigationRevision > 0}
        persistentPage="home"
        persistentChildren={<HomePage onNavigate={navigate} />}
      >
        <Suspense fallback={<div className="page-loading"><Spinner /></div>}>
          {page === "appearance" && import.meta.env.DEV
            ? <AppearanceLab />
            : page === "about"
              ? <AboutPage />
              : page === "settings"
                ? <SettingsPage onOpenBlockedDomains={() => navigate("blocked-domains")} />
                : page === "blocked-domains"
                  ? <BlockedDomainsPage onBack={() => navigate("settings")} />
            : page === "health"
              ? <HealthPage />
              : page === "connections"
                ? <ConnectionsPage />
            : page === "routing"
              ? <RoutingPage />
              : null}
        </Suspense>
      </AppShell>
    </FluentProvider>
  );
}

export default function App() {
  return (
    <LanguageProvider>
      <AppearanceProvider>
        <HypoMuxWindow />
      </AppearanceProvider>
    </LanguageProvider>
  );
}
