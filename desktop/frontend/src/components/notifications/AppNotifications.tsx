import { Button } from "@fluentui/react-components";
import {
  CheckmarkCircle20Regular,
  ChevronDown16Regular,
  ChevronUp16Regular,
  Dismiss16Regular,
  ErrorCircle20Regular,
  Info20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
  type ReactNode,
  type CSSProperties,
} from "react";
import { useI18n } from "../../i18n/i18n";
import {
  prepareNotificationMessage,
  type NotificationIntent,
} from "./notificationMessage";
import { resolveNotificationErrorCode } from "./errorCodes";

type NotificationAction = {
  label: string;
  onClick: () => void;
};

export type AppNotificationInput = {
  title: string;
  message?: string;
  intent?: NotificationIntent;
  action?: NotificationAction;
  timeout?: number;
  dedupeKey?: string;
  code?: string;
};

type AppNotification = Required<Pick<AppNotificationInput, "title" | "intent">> & {
  id: string;
  summary: string;
  detail?: string;
  action?: NotificationAction;
  occurrences: number;
  timeout: number;
  code?: string;
};

type AppNotificationContextValue = {
  notify: (input: AppNotificationInput) => void;
  dismiss: (id: string) => void;
  clear: () => void;
  notifications: AppNotification[];
  detailsOpen: boolean;
  setDetailsOpen: (open: boolean | ((current: boolean) => boolean)) => void;
  setInteractionPaused: (paused: boolean) => void;
  /** True while the active notification's auto-dismiss countdown is on hold. */
  countdownPaused: boolean;
};

const AppNotificationContext = createContext<AppNotificationContextValue | null>(null);

// Keep in sync with the island's leave animation in app.css.
const ISLAND_EXIT_MS = 300;
const DETAILS_EXIT_MS = 200;

const notificationIcon = (intent: NotificationIntent): ReactNode => {
  if (intent === "success") return <CheckmarkCircle20Regular />;
  if (intent === "warning") return <Warning20Regular />;
  if (intent === "info") return <Info20Regular />;
  return <ErrorCircle20Regular />;
};

export function AppNotificationProvider({ children }: PropsWithChildren) {
  const { locale } = useI18n();
  const [notifications, setNotifications] = useState<AppNotification[]>([]);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [interactionPaused, setInteractionPaused] = useState(false);
  const [documentHidden, setDocumentHidden] = useState(document.hidden);
  const countdown = useRef<{ notification: AppNotification; remaining: number }>();

  const dismiss = useCallback((id: string) => {
    setNotifications((current) => current.filter((notification) => notification.id !== id));
    setDetailsOpen(false);
  }, []);

  const clear = useCallback(() => {
    setNotifications([]);
    setDetailsOpen(false);
  }, []);

  const notify = useCallback((input: AppNotificationInput) => {
    const intent = input.intent ?? "info";
    const { summary, detail } = prepareNotificationMessage(input.message ?? "", intent, locale);
    const id = input.dedupeKey ?? `${intent}:${input.title}:${summary}`;
    const code = intent === "error"
      ? input.code ?? resolveNotificationErrorCode({
        title: input.title,
        message: input.message ?? "",
        dedupeKey: input.dedupeKey,
      })
      : input.code;
    const next: AppNotification = {
      id,
      title: input.title,
      intent,
      summary,
      detail,
      action: input.action,
      occurrences: 1,
      // Actions must remain available until the user makes a choice.
      timeout: input.timeout ?? (input.action ? 0 : intent === "success" ? 3200 : intent === "info" ? 4200 : 0),
      code,
    };

    setNotifications((current) => {
      const previous = current.find((notification) => notification.id === id);
      if (previous) next.occurrences = previous.occurrences + 1;
      return [next, ...current.filter((notification) => notification.id !== id)].slice(0, 5);
    });
    setDetailsOpen(false);

  }, [locale]);

  useEffect(() => {
    const onVisibilityChange = () => setDocumentHidden(document.hidden);
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => document.removeEventListener("visibilitychange", onVisibilityChange);
  }, []);

  const active = notifications[0];
  useEffect(() => {
    if (!active || active.timeout <= 0) return;
    if (countdown.current?.notification !== active) {
      countdown.current = { notification: active, remaining: active.timeout };
    }
    if (interactionPaused || detailsOpen || documentHidden) return;
    const current = countdown.current;
    const started = Date.now();
    const timer = window.setTimeout(() => dismiss(active.id), current.remaining);
    return () => {
      window.clearTimeout(timer);
      current.remaining = Math.max(0, current.remaining - (Date.now() - started));
    };
  }, [active, detailsOpen, dismiss, documentHidden, interactionPaused]);

  const countdownPaused = interactionPaused || detailsOpen || documentHidden;
  const value = useMemo(() => ({
    notify,
    dismiss,
    clear,
    notifications,
    detailsOpen,
    setDetailsOpen,
    setInteractionPaused,
    countdownPaused,
  }), [clear, countdownPaused, detailsOpen, dismiss, notifications, notify]);

  return (
    <AppNotificationContext.Provider value={value}>
      {children}
    </AppNotificationContext.Provider>
  );
}

export function AppNotificationCenter({ wallpaperBackground }: { wallpaperBackground?: string }) {
  const context = useContext(AppNotificationContext);
  if (!context) throw new Error("AppNotificationCenter must be used inside AppNotificationProvider");

  return <AppNotificationViewport context={context} wallpaperBackground={wallpaperBackground} />;
}

function AppNotificationViewport({
  context,
  wallpaperBackground,
}: {
  context: AppNotificationContextValue;
  wallpaperBackground?: string;
}) {
  const { locale } = useI18n();
  const { notifications, detailsOpen, setDetailsOpen, setInteractionPaused, dismiss, clear, countdownPaused } = context;
  const active = notifications[0];
  const [rendered, setRendered] = useState<AppNotification | undefined>(active);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [detailsMounted, setDetailsMounted] = useState(false);
  // Alternating pulse classes restart the squash animation whenever the
  // visible island swaps or merges content without remounting the region.
  const [pulse, setPulse] = useState<0 | 1 | 2>(0);
  const previousActive = useRef<AppNotification | undefined>(active);

  useEffect(() => {
    const previous = previousActive.current;
    previousActive.current = active;
    if (!active) { setPulse(0); return; }
    if (previous && previous !== active) setPulse((current) => (current === 1 ? 2 : 1));
  }, [active]);

  useEffect(() => {
    if (detailsOpen) { setDetailsMounted(true); return; }
    if (!detailsMounted) return;
    const timer = window.setTimeout(() => setDetailsMounted(false), DETAILS_EXIT_MS);
    return () => window.clearTimeout(timer);
  }, [detailsMounted, detailsOpen]);

  useEffect(() => {
    setInteractionPaused(Boolean(active) && (hovered || focused));
    return () => setInteractionPaused(false);
  }, [active, hovered, focused, setInteractionPaused]);

  useEffect(() => {
    if (!active) { setHovered(false); setFocused(false); }
  }, [active]);

  useEffect(() => {
    if (active) {
      setRendered(active);
      return;
    }
    if (!rendered) return;
    const timer = window.setTimeout(() => setRendered(undefined), ISLAND_EXIT_MS);
    return () => window.clearTimeout(timer);
  }, [active, rendered]);

  const shown = active ?? rendered;
  if (!shown) return null;
  const isLeaving = !active;
  const islandStyle = wallpaperBackground
    ? { "--hm-wallpaper-background": wallpaperBackground } as CSSProperties
    : undefined;
  const contentKey = `${shown.id}:${shown.occurrences}`;
  const stackDepth = Math.min(notifications.length - 1, 2);
  const showDetails = Boolean(shown.detail) && (detailsOpen || detailsMounted);
  const regionClasses = [
    "global-notification-region",
    `is-${shown.intent}`,
    detailsOpen && "is-expanded",
    isLeaving && "is-leaving",
    pulse > 0 && `is-pulse-${pulse === 1 ? "a" : "b"}`,
    stackDepth > 0 && `has-stack-${stackDepth}`,
  ].filter(Boolean).join(" ");
  return (
    <section
      className={regionClasses}
      style={islandStyle}
      role={shown.intent === "error" ? "alert" : "status"}
      aria-live={shown.intent === "error" ? "assertive" : "polite"}
      aria-atomic="true"
      onPointerEnter={() => setHovered(true)}
      onPointerLeave={() => setHovered(false)}
      onFocusCapture={() => setFocused(true)}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false);
      }}
      onKeyDown={(event) => {
        if (event.key !== "Escape" || isLeaving) return;
        event.stopPropagation();
        if (detailsOpen) setDetailsOpen(false);
        else dismiss(shown.id);
      }}
    >
      <span className="global-notification-stack" aria-hidden="true"><span /><span /></span>
      <span className="global-notification-frost" aria-hidden="true" />
      <div className="global-notification-bar">
        <span key={`icon:${contentKey}`} className="global-notification-icon" aria-hidden="true">
          {shown.timeout > 0 && !isLeaving && (
            <svg
              className={`global-notification-countdown${countdownPaused ? " is-paused" : ""}`}
              style={{ "--hm-island-timeout": `${shown.timeout}ms` } as CSSProperties}
              viewBox="0 0 36 36"
            >
              <circle cx="18" cy="18" r="16.5" pathLength={100} />
            </svg>
          )}
          {notificationIcon(shown.intent)}
        </span>
        <div key={`copy:${contentKey}`} className="global-notification-copy">
          <strong>{shown.title}</strong>
          {shown.summary && <span>{shown.summary}</span>}
        </div>
        {shown.code && (
          <span className="global-notification-code" title={locale === "en" ? "Error code" : "错误代码"}>
            {shown.code}
          </span>
        )}
        {shown.occurrences > 1 && (
          <span key={shown.occurrences} className="global-notification-count" title={locale === "en" ? "Repeated notifications" : "重复通知"}>
            ×{shown.occurrences}
          </span>
        )}
        {notifications.length > 1 && (
          <span className="global-notification-queue">
            {locale === "en" ? `${notifications.length} notices` : `${notifications.length} 条通知`}
          </span>
        )}
        {shown.detail && (
          <Button
            className="global-notification-button global-notification-details-button"
            appearance="subtle"
            size="small"
            icon={detailsOpen ? <ChevronUp16Regular /> : <ChevronDown16Regular />}
            onClick={() => setDetailsOpen((open) => !open)}
            aria-expanded={detailsOpen}
            aria-controls={detailsOpen ? "notification-details" : undefined}
          >
            {detailsOpen ? (locale === "en" ? "Hide" : "收起") : (locale === "en" ? "Details" : "详情")}
          </Button>
        )}
        {shown.action && (
          <Button
            className="global-notification-button global-notification-action"
            appearance="subtle"
            size="small"
            onClick={shown.action.onClick}
          >
            {shown.action.label}
          </Button>
        )}
        <Button
          className="global-notification-button global-notification-dismiss"
          appearance="subtle"
          size="small"
          icon={<Dismiss16Regular />}
          aria-label={locale === "en" ? "Dismiss notification" : "关闭通知"}
          onClick={() => dismiss(shown.id)}
        />
      </div>
      {showDetails && (
        <div id="notification-details" className={`global-notification-details${detailsOpen ? "" : " is-closing"}`}>
          <div className="global-notification-details-clip">
            <div className="global-notification-details-content">
              <pre>{shown.detail}</pre>
              {notifications.length > 1 && (
                <Button className="global-notification-button" appearance="subtle" size="small" onClick={clear}>
                  {locale === "en" ? "Clear all" : "清除全部"}
                </Button>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

export function useAppNotifications() {
  const context = useContext(AppNotificationContext);
  if (!context) throw new Error("useAppNotifications must be used inside AppNotificationProvider");
  return context;
}
