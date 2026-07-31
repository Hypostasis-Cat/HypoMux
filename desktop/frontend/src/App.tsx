import { FluentProvider } from "@fluentui/react-components";
import { useState } from "react";
import "./theme/material.tokens.css";
import "./theme/semantic.tokens.css";
import "./theme/typography.tokens.css";
import "./theme/motion.tokens.css";
import "./app.css";
import { WallpaperLayer } from "./components/material/WallpaperLayer";
import { AppShell } from "./components/shell/AppShell";
import type { AppPage } from "./components/shell/CompactNavigation";
import { AppearanceLab } from "./pages/AppearanceLab";
import { AboutPage } from "./pages/AboutPage";
import { BlockedDomainsPage } from "./pages/BlockedDomainsPage";
import { HomePage } from "./pages/HomePage";
import { HealthPage } from "./pages/HealthPage";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { RoutingPage } from "./pages/RoutingPage";
import { SettingsPage } from "./pages/SettingsPage";
import { AppearanceProvider, useAppearance } from "./theme/appearance.store";
import { LanguageProvider } from "./i18n/i18n";

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

  return (
    <FluentProvider theme={fluentTheme} className="hypomux-provider">
      <WallpaperLayer />
      <AppShell
        page={page}
        onPageChange={navigate}
        pageDirection={pageDirection}
        animatePage={navigationRevision > 0}
      >
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
            : <HomePage onNavigate={navigate} />}
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
