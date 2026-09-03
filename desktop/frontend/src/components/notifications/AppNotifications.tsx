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
} from "react";
import { useI18n } from "../../i18n/i18n";
import {
  prepareNotificationMessage,
  type NotificationIntent,
} from "./notificationMessage";

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
};

type AppNotification = Required<Pick<AppNotificationInput, "title" | "intent">> & {
  id: string;
  summary: string;
  detail?: string;
  action?: NotificationAction;
  occurrences: number;
};

type AppNotificationContextValue = {
  notify: (input: AppNotificationInput) => void;
  dismiss: (id: string) => void;
  clear: () => void;
  notifications: AppNotification[];
  detailsOpen: boolean;
  setDetailsOpen: (open: boolean | ((current: boolean) => boolean)) => void;
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
  const timers = useRef(new Map<string, ReturnType<typeof window.setTimeout>>());

  const dismiss = useCallback((id: string) => {
    const timer = timers.current.get(id);
    if (timer !== undefined) window.clearTimeout(timer);
    timers.current.delete(id);
    setNotifications((current) => current.filter((notification) => notification.id !== id));
    setDetailsOpen(false);
  }, []);

  const clear = useCallback(() => {
    for (const timer of timers.current.values()) window.clearTimeout(timer);
    timers.current.clear();
    setNotifications([]);
    setDetailsOpen(false);
  }, []);

  const notify = useCallback((input: AppNotificationInput) => {
    const intent = input.intent ?? "info";
    const { summary, detail } = prepareNotificationMessage(input.message ?? "", intent, locale);
    const id = input.dedupeKey ?? `${intent}:${input.title}:${summary}`;
    const next: AppNotification = {
      id,
      title: input.title,
      intent,
      summary,
      detail,
      action: input.action,
      occurrences: 1,
    };

    setNotifications((current) => {
      const previous = current.find((notification) => notification.id === id);
      if (previous) next.occurrences = previous.occurrences + 1;
      return [next, ...current.filter((notification) => notification.id !== id)].slice(0, 5);
    });
    setDetailsOpen(false);

    const currentTimer = timers.current.get(id);
    if (currentTimer !== undefined) window.clearTimeout(currentTimer);
    const timeout = input.timeout ?? (intent === "success" ? 3200 : intent === "info" ? 4200 : 0);
    if (timeout > 0) {
      timers.current.set(id, window.setTimeout(() => dismiss(id), timeout));
    }
  }, [dismiss, locale]);

  useEffect(() => () => {
    for (const timer of timers.current.values()) window.clearTimeout(timer);
  }, []);

  const value = useMemo(() => ({
    notify,
    dismiss,
    clear,
    notifications,
    detailsOpen,
    setDetailsOpen,
  }), [clear, detailsOpen, dismiss, notifications, notify]);

  return (
    <AppNotificationContext.Provider value={value}>
      {children}
    </AppNotificationContext.Provider>
  );
}

export function AppNotificationCenter() {
  const context = useContext(AppNotificationContext);
  const { locale } = useI18n();
  if (!context) throw new Error("AppNotificationCenter must be used inside AppNotificationProvider");
  const { notifications, detailsOpen, setDetailsOpen, dismiss, clear } = context;
  const active = notifications[0];
  if (!active) return null;

  return (
    <section
      className={`global-notification-region is-${active.intent}${detailsOpen ? " is-expanded" : ""}`}
      role={active.intent === "error" ? "alert" : "status"}
      aria-live={active.intent === "error" ? "assertive" : "polite"}
      aria-atomic="true"
    >
      <div className="global-notification-bar">
        <span className="global-notification-icon" aria-hidden="true">
          {notificationIcon(active.intent)}
        </span>
        <div className="global-notification-copy">
          <strong>{active.title}</strong>
          {active.summary && <span>{active.summary}</span>}
        </div>
        {active.occurrences > 1 && (
          <span className="global-notification-count" title={locale === "en" ? "Repeated notifications" : "重复通知"}>
            ×{active.occurrences}
          </span>
        )}
        {notifications.length > 1 && (
          <span className="global-notification-queue">
            {locale === "en" ? `${notifications.length} notices` : `${notifications.length} 条通知`}
          </span>
        )}
        {active.detail && (
          <Button
            appearance="subtle"
            size="small"
            icon={detailsOpen ? <ChevronUp16Regular /> : <ChevronDown16Regular />}
            onClick={() => setDetailsOpen((open) => !open)}
            aria-expanded={detailsOpen}
          >
            {detailsOpen ? (locale === "en" ? "Hide" : "收起") : (locale === "en" ? "Details" : "详情")}
          </Button>
        )}
        {active.action && (
          <Button appearance="subtle" size="small" onClick={active.action.onClick}>
            {active.action.label}
          </Button>
        )}
        <Button
          appearance="subtle"
          size="small"
          icon={<Dismiss16Regular />}
          aria-label={locale === "en" ? "Dismiss notification" : "关闭通知"}
          onClick={() => dismiss(active.id)}
        />
      </div>
      {detailsOpen && active.detail && (
        <div className="global-notification-details">
          <pre>{active.detail}</pre>
          {notifications.length > 1 && (
            <Button appearance="subtle" size="small" onClick={clear}>
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
