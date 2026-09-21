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
};

const AppNotificationContext = createContext<AppNotificationContextValue | null>(null);

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

  const value = useMemo(() => ({
    notify,
    dismiss,
    clear,
    notifications,
    detailsOpen,
    setDetailsOpen,
    setInteractionPaused,
  }), [clear, detailsOpen, dismiss, notifications, notify]);

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
  const { notifications, detailsOpen, setDetailsOpen, setInteractionPaused, dismiss, clear } = context;
  const active = notifications[0];
  const [rendered, setRendered] = useState<AppNotification | undefined>(active);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);

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
    const timer = window.setTimeout(() => setRendered(undefined), 240);
    return () => window.clearTimeout(timer);
  }, [active, rendered]);

  const shown = active ?? rendered;
  if (!shown) return null;
  const isLeaving = !active;
  const islandStyle = wallpaperBackground
    ? { "--hm-wallpaper-background": wallpaperBackground } as CSSProperties
    : undefined;
  return (
    <section
      className={`global-notification-region is-${shown.intent}${detailsOpen ? " is-expanded" : ""}${isLeaving ? " is-leaving" : ""}`}
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
      <span className="global-notification-frost" aria-hidden="true" />
      <div className="global-notification-bar">
        <span className="global-notification-icon" aria-hidden="true">
          {notificationIcon(shown.intent)}
        </span>
        <div className="global-notification-copy">
          <strong>{shown.title}</strong>
          {shown.summary && <span>{shown.summary}</span>}
        </div>
        {shown.code && (
          <span className="global-notification-code" title={locale === "en" ? "Error code" : "错误代码"}>
            {shown.code}
          </span>
        )}
        {shown.occurrences > 1 && (
          <span className="global-notification-count" title={locale === "en" ? "Repeated notifications" : "重复通知"}>
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
      {detailsOpen && shown.detail && (
        <div id="notification-details" className="global-notification-details">
          <pre>{shown.detail}</pre>
          {notifications.length > 1 && (
            <Button className="global-notification-button" appearance="subtle" size="small" onClick={clear}>
              {locale === "en" ? "Clear all" : "清除全部"}
            </Button>
          )}
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
