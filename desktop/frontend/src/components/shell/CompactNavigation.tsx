import { Tooltip } from "@fluentui/react-components";
import {
  Chat24Regular,
  Beaker24Regular,
  BranchFork24Regular,
  HeartPulse24Regular,
  Home24Filled,
  Home24Regular,
  Info24Regular,
  PlugConnected24Regular,
  Settings24Regular,
  Toolbox24Regular,
} from "@fluentui/react-icons";
import { useLayoutEffect, useRef, useState } from "react";
import { useI18n } from "../../i18n/i18n";

export type AppPage = "assistant" | "tools" | "home" | "routing" | "health" | "connections" | "settings" | "blocked-domains" | "about" | "appearance";

export function CompactNavigation({
  page,
  onPageChange,
  aiEnabled = true,
}: {
  page: AppPage;
  onPageChange: (page: AppPage) => void;
  aiEnabled?: boolean;
}) {
  const { locale, t } = useI18n();
  const navigationRef = useRef<HTMLElement>(null);
  const activeButtonRef = useRef<HTMLButtonElement | null>(null);
  const [indicatorTop, setIndicatorTop] = useState<number | null>(null);
  const navigationPage = !aiEnabled && page === "assistant" ? "home" : page === "blocked-domains" ? "settings" : page;
  const mainItems = [
    { id: "home", label: t("nav_home"), icon: <Home24Regular />, activeIcon: <Home24Filled /> },
    { id: "assistant", label: locale === "en" ? "AI assistant" : "AI 助手", icon: <Chat24Regular /> },
    { id: "routing", label: t("nav_routing"), icon: <BranchFork24Regular /> },
    { id: "health", label: t("nav_tools"), icon: <HeartPulse24Regular /> },
    { id: "connections", label: locale === "en" ? "Connections" : "活动连接", icon: <PlugConnected24Regular /> },
    { id: "tools", label: locale === "en" ? "Toolbox" : "工具箱", icon: <Toolbox24Regular /> },
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
      // Read layout coordinates, not the in-flight CSS transform. The highlight
      // and the other entries must travel to the same destination together.
      let top = 0;
      let element: HTMLElement | null = activeButton;
      while (element && element !== navigation) {
        top += element.offsetTop;
        element = element.offsetParent as HTMLElement | null;
      }
      if (!aiEnabled && activeButton.closest(".nav-item-after-ai")) {
        top -= navigation.querySelector<HTMLElement>(".nav-item-ai")?.offsetHeight ?? 0;
      }
      setIndicatorTop(top);
    };

    updateIndicator();
    const resizeObserver = new ResizeObserver(updateIndicator);
    resizeObserver.observe(navigation);
    window.addEventListener("resize", updateIndicator);
    return () => {
      resizeObserver.disconnect();
      window.removeEventListener("resize", updateIndicator);
    };
  }, [navigationPage, aiEnabled]);

  return (
    <nav ref={navigationRef} className="compact-navigation" data-ai-enabled={aiEnabled} aria-label={locale === "en" ? "Main navigation" : "主导航"}
      onKeyDown={(event) => {
        if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
        const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>(".nav-button:not(:disabled)"));
        const current = buttons.indexOf(event.target as HTMLButtonElement);
        if (current < 0) return;
        event.preventDefault();
        const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1
          : (current + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
        buttons[next]?.focus();
      }}>
      <span
        className="nav-selection-window"
        data-visible={indicatorTop !== null}
        style={{ transform: `translate3d(0, ${indicatorTop ?? 0}px, 0)` }}
        aria-hidden="true"
      />
      <div className="nav-items">
        {mainItems.map((item, index) => {
          const active = item.id === navigationPage;
          const hidden = item.id === "assistant" && !aiEnabled;
          return (
            <div key={item.id} className={`nav-item${item.id === "assistant" ? " nav-item-ai" : index > 1 ? " nav-item-after-ai" : ""}`}
              aria-hidden={hidden || undefined}>
            <Tooltip
              content={item.label}
              relationship="label"
              positioning="after"
            >
              <button
                ref={active ? activeButtonRef : undefined}
                className={`nav-button${active ? " is-active" : ""}`}
                aria-label={item.label}
                aria-current={active ? "page" : undefined}
                disabled={hidden}
                tabIndex={hidden ? -1 : undefined}
                onClick={() => {
                  onPageChange(item.id as AppPage);
                }}
              >
                {active && item.activeIcon ? item.activeIcon : item.icon}
              </button>
            </Tooltip>
            </div>
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
