import { Tooltip } from "@fluentui/react-components";
import {
  Beaker24Regular,
  BranchFork24Regular,
  HeartPulse24Regular,
  Home24Filled,
  Home24Regular,
  Info24Regular,
  PlugConnected24Regular,
  Settings24Regular,
} from "@fluentui/react-icons";
import { useLayoutEffect, useRef, useState } from "react";
import { useI18n } from "../../i18n/i18n";

export type AppPage = "home" | "routing" | "health" | "connections" | "settings" | "blocked-domains" | "about" | "appearance";

export function CompactNavigation({
  page,
  onPageChange,
}: {
  page: AppPage;
  onPageChange: (page: AppPage) => void;
}) {
  const { locale, t } = useI18n();
  const navigationRef = useRef<HTMLElement>(null);
  const activeButtonRef = useRef<HTMLButtonElement | null>(null);
  const [indicatorTop, setIndicatorTop] = useState<number | null>(null);
  const navigationPage = page === "blocked-domains" ? "settings" : page;
  const mainItems = [
    { id: "home", label: t("nav_home"), icon: <Home24Regular />, activeIcon: <Home24Filled /> },
    { id: "routing", label: t("nav_routing"), icon: <BranchFork24Regular /> },
    { id: "health", label: t("nav_tools"), icon: <HeartPulse24Regular /> },
    { id: "connections", label: locale === "en" ? "Connections" : "活动连接", icon: <PlugConnected24Regular /> },
    { id: "settings", label: t("nav_settings"), icon: <Settings24Regular /> },
  ];

  useLayoutEffect(() => {
    const navigation = navigationRef.current;
    const activeButton = activeButtonRef.current;
    if (!navigation || !activeButton) {
      setIndicatorTop(null);
      return;
    }

    const updateIndicator = () => {
      const navigationRect = navigation.getBoundingClientRect();
      const buttonRect = activeButton.getBoundingClientRect();
      setIndicatorTop(buttonRect.top - navigationRect.top);
    };

    updateIndicator();
    const resizeObserver = new ResizeObserver(updateIndicator);
    resizeObserver.observe(navigation);
    window.addEventListener("resize", updateIndicator);
    return () => {
      resizeObserver.disconnect();
      window.removeEventListener("resize", updateIndicator);
    };
  }, [navigationPage]);

  return (
    <nav ref={navigationRef} className="compact-navigation" aria-label={locale === "en" ? "Main navigation" : "主导航"}>
      <span
        className="nav-selection-window"
        data-visible={indicatorTop !== null}
        style={{ transform: `translate3d(0, ${indicatorTop ?? 0}px, 0)` }}
        aria-hidden="true"
      />
      <div className="nav-items">
        {mainItems.map((item) => {
          const active = item.id === navigationPage;
          return (
            <Tooltip
              key={item.id}
              content={item.label}
              relationship="label"
              positioning="after"
            >
              <button
                ref={active ? activeButtonRef : undefined}
                className={`nav-button${active ? " is-active" : ""}`}
                aria-label={item.label}
                aria-current={active ? "page" : undefined}
                onClick={() => {
                  onPageChange(item.id as AppPage);
                }}
              >
                {active && item.activeIcon ? item.activeIcon : item.icon}
              </button>
            </Tooltip>
          );
        })}
      </div>
      <div className="nav-bottom">
        {import.meta.env.DEV && (
          <Tooltip content="Appearance Lab" relationship="label" positioning="after">
            <button
              ref={navigationPage === "appearance" ? activeButtonRef : undefined}
              className={`nav-button${page === "appearance" ? " is-active" : ""}`}
              aria-label="Appearance Lab"
              aria-current={page === "appearance" ? "page" : undefined}
              onClick={() => onPageChange("appearance")}
            >
              <Beaker24Regular />
            </button>
          </Tooltip>
        )}
        <Tooltip content={t("nav_about")} relationship="label" positioning="after">
          <button
            ref={navigationPage === "about" ? activeButtonRef : undefined}
            className={`nav-button${page === "about" ? " is-active" : ""}`}
            aria-label={t("nav_about")}
            aria-current={page === "about" ? "page" : undefined}
            onClick={() => onPageChange("about")}
          >
            <Info24Regular />
          </button>
        </Tooltip>
      </div>
    </nav>
  );
}
