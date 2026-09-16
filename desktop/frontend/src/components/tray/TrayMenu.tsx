import { FluentProvider } from "@fluentui/react-components";
import { ArrowExit20Regular, Dismiss20Regular, Window20Regular } from "@fluentui/react-icons";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { desktopPlatform } from "../../platform/desktop";
import { appServices } from "../../platform/services";
import { appearancePersistence } from "../../theme/background.service";
import { defaultAppearance, resolveAccent } from "../../theme/appearance.presets";
import { createHypoMuxTheme } from "../../theme/createFluentTheme";

const phases: Record<string, [string, string]> = {
  stopped: ["未启动", "Stopped"], running: ["运行中", "Running"],
  degraded: ["降级运行", "Degraded"], starting: ["正在启动", "Starting"],
  stopping: ["正在停止", "Stopping"], failed: ["异常", "Failed"],
  waiting_network: ["等待网络就绪", "Waiting for network"],
};

export function TrayMenu() {
  const [appearance, setAppearance] = useState(defaultAppearance);
  const [dark, setDark] = useState(() => window.matchMedia("(prefers-color-scheme: dark)").matches);
  const [english, setEnglish] = useState(false);
  const [snapshot, setSnapshot] = useState<{ phase: string; mode: string }>();
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [keyboardNavigation, setKeyboardNavigation] = useState(false);
  const pending = useRef(false);
  const menu = useRef<HTMLDivElement>(null);
  const card = useRef<HTMLElement>(null);
  const text = (zh: string, en: string) => english ? en : zh;

  useEffect(() => {
    if (!card.current || typeof ResizeObserver === "undefined") return;
    let lastHeight = 0;
    const resize = () => {
      const height = Math.ceil(card.current!.getBoundingClientRect().height) + 16;
      if (height === lastHeight) return;
      lastHeight = height;
      void desktopPlatform.resizeTray(height);
    };
    const observer = new ResizeObserver(resize);
    observer.observe(card.current);
    resize();
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    let alive = true;
    let active = false;
    let inFlight = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let preferencesRevision = 0;
    let focusRevision = 0;
    const refresh = async () => {
      if (!alive || !active || inFlight) return;
      inFlight = true;
      const revision = focusRevision;
      try {
        const next = await appServices.engine.trayStatus();
        if (alive && active && revision === focusRevision) { setSnapshot(next); setUnavailable(false); }
      } catch {
        if (alive && active && revision === focusRevision) setUnavailable(true);
      } finally {
        inFlight = false;
        if (alive && active) timer = setTimeout(() => void refresh(), 1000);
      }
    };
    const focus = () => {
      ++focusRevision;
      active = true;
      clearTimeout(timer);
      setError("");
      setKeyboardNavigation(false);
      menu.current?.querySelector<HTMLButtonElement>("button")?.focus();
      void refresh();
      const revision = ++preferencesRevision;
      // Read only: a second AppearanceProvider would persist stale settings
      // and call native appearance methods on the main window.
      void appearancePersistence.load().then(saved => {
        if (alive && revision === preferencesRevision) setAppearance({ ...defaultAppearance, ...saved });
      }).catch(() => undefined);
      void appServices.settings.get().then(settings => {
        if (alive && revision === preferencesRevision) setEnglish(settings.language === "en");
      }).catch(() => undefined);
    };
    const blur = () => { active = false; ++focusRevision; clearTimeout(timer); };
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const systemTheme = () => setDark(media.matches);
    window.addEventListener("focus", focus);
    window.addEventListener("blur", blur);
    media.addEventListener("change", systemTheme);
    if (document.hasFocus()) focus();
    return () => {
      alive = false;
      clearTimeout(timer);
      window.removeEventListener("focus", focus);
      window.removeEventListener("blur", blur);
      media.removeEventListener("change", systemTheme);
    };
  }, []);

  const act = async (action: "show" | "hide" | "dismiss" | "quit") => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError("");
    try { await desktopPlatform.trayAction(action); }
    catch { setError(text("操作失败，请重试", "Action failed. Try again.")); }
    finally { pending.current = false; setBusy(false); }
  };
  const keyDown = (event: KeyboardEvent) => {
    setKeyboardNavigation(true);
    if (event.key === "Escape" || event.key === "Tab") {
      event.preventDefault();
      void act("dismiss");
      return;
    }
    const buttons = Array.from(menu.current?.querySelectorAll<HTMLButtonElement>("button") ?? []);
    const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
    let next: number;
    switch (event.key) {
      case "ArrowDown": next = (current + 1) % buttons.length; break;
      case "ArrowUp": next = (current - 1 + buttons.length) % buttons.length; break;
      case "Home": next = 0; break;
      case "End": next = buttons.length - 1; break;
      default: return;
    }
    event.preventDefault();
    buttons[next]?.focus();
  };
  const mode = appearance.mode === "system" ? dark ? "dark" : "light" : appearance.mode;
  const phase = unavailable ? "unknown" : snapshot?.phase ?? "loading";
  const status = unavailable ? text("状态暂不可用", "Status unavailable")
    : !snapshot ? text("正在读取状态…", "Reading status…")
    : phases[phase]?.[english ? 1 : 0] ?? text("未知状态", "Unknown state");

  return <FluentProvider lang={english ? "en" : "zh-CN"} theme={createHypoMuxTheme(mode, resolveAccent(appearance))} className="tray-menu">
    <main ref={card} className="tray-menu__card" data-keyboard-navigation={keyboardNavigation}
      onKeyDown={keyDown} onPointerMove={() => setKeyboardNavigation(false)} onPointerDown={() => setKeyboardNavigation(false)}>
      <header className="tray-menu__header">
        <div className="tray-menu__title">HypoMux</div>
        <div className="tray-menu__status" data-phase={phase} role="status">{status}
          {!unavailable && snapshot && <span> · {snapshot.mode === "tun" ? text("虚拟网卡", "TUN") : text("系统代理", "System proxy")}</span>}
        </div>
      </header>
      <div ref={menu} role="menu" aria-label={text("HypoMux 托盘菜单", "HypoMux tray menu")} aria-busy={busy}>
        <button className="tray-menu__action" role="menuitem" aria-disabled={busy} onClick={() => void act("show")}><Window20Regular />{text("显示主窗口", "Show main window")}</button>
        <button className="tray-menu__action" role="menuitem" aria-disabled={busy} onClick={() => void act("hide")}><Dismiss20Regular />{text("隐藏到托盘", "Hide to tray")}</button>
        <div className="tray-menu__separator" role="separator" />
        <button className="tray-menu__action" role="menuitem" aria-disabled={busy} onClick={() => void act("quit")}><ArrowExit20Regular />{text("退出 HypoMux", "Quit HypoMux")}</button>
      </div>
      {error && <div className="tray-menu__error" role="alert">{error}</div>}
    </main>
  </FluentProvider>;
}
