import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../../i18n/i18n";

export function AssistantCompanion({ open, onOpenChange, running, pending, pageLabel, speech, speechId, sample = false, onViewDetails, children }: {
  open: boolean; onOpenChange: (open: boolean) => void; running: boolean; pending: number; pageLabel: string; speech?: string; speechId?: string; sample?: boolean; onViewDetails?: () => void; children: ReactNode;
}) {
  const { locale } = useI18n();
  const [position, setPosition] = useState({ right: 24, bottom: 28 });
  const [viewport, setViewport] = useState({ width: window.innerWidth, height: window.innerHeight });
  const [dragging, setDragging] = useState(false);
  const [hovered, setHovered] = useState(false);
  const hoverTimer = useRef<ReturnType<typeof setTimeout>>();
  useEffect(() => () => clearTimeout(hoverTimer.current), []);
  const [focused, setFocused] = useState(false);
  const [speechVisible, setSpeechVisible] = useState(true);
  useEffect(() => {
    if (!open) { setHovered(false); setFocused(false); }
  }, [open]);
  useEffect(() => { setSpeechVisible(true); }, [speech, speechId, running]);
  useEffect(() => {
    if (!speech || hovered || focused || open || running || pending || !speechVisible) return;
    const timer = window.setTimeout(() => setSpeechVisible(false), Math.min(14000, Math.max(6000, speech.length * 90)));
    return () => window.clearTimeout(timer);
  }, [speech, hovered, focused, open, running, pending, speechVisible]);
  const drag = useRef<{ x: number; y: number; right: number; bottom: number; moved: boolean }>();
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const resize = () => setViewport({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  const right = Math.max(16, Math.min(position.right, viewport.width - 152));
  const bottom = Math.max(16, Math.min(position.bottom, viewport.height - 148));
  const leftSide = right > viewport.width / 2;
  const below = bottom > viewport.height / 2;
  const room = below ? bottom - 18 : viewport.height - bottom - 142;
  const bubbleWidth = Math.min(370, viewport.width - 112);
  const bubbleLeft = Math.max(16, Math.min(leftSide ? viewport.width - right - 88 : viewport.width - right - bubbleWidth, viewport.width - bubbleWidth - 16));
  const speechWidth = Math.min(300, viewport.width - 48);
  const speechLeft = Math.max(16, Math.min(leftSide ? viewport.width - right : viewport.width - right - 78 - speechWidth, viewport.width - speechWidth - 16));
  const close = () => { onOpenChange(false); trigger.current?.focus(); };
  const status = pending ? (locale === "en" ? "Your approval needed" : "等你确认一下") : running ? (locale === "en" ? "Working on it…" : "正在帮你处理…") : (locale === "en" ? "Ask me anything" : "有问题，叫我就好");
  return <div className={`ai-companion${open ? " is-open" : ""}${dragging ? " is-dragging" : ""}${leftSide ? " dock-left" : ""}${below ? " bubble-below" : ""}`} style={{ right, bottom }} data-state={pending ? "waiting" : running ? "thinking" : "idle"} onMouseEnter={() => { clearTimeout(hoverTimer.current); setHovered(true); }} onMouseLeave={() => { hoverTimer.current = setTimeout(() => setHovered(false), 250); }} onFocus={() => setFocused(true)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false); }}>
    {open && <section className="ai-pet-bubble" style={{ position: "fixed", left: bubbleLeft, right: "auto", top: below ? viewport.height - bottom + 10 : "auto", bottom: below ? "auto" : bottom + 116, maxHeight: Math.max(200, room) }} aria-label={locale === "en" ? "Companion chat" : "小精灵对话"} onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); close(); } }}>{children}</section>}
    {!open && (speech && !running && !pending ? (speechVisible || hovered || focused) && <div className="ai-pet-speech" role="status" tabIndex={0} style={{ left: speechLeft, width: speechWidth, bottom: below ? "auto" : bottom + 46, top: below ? viewport.height - bottom + 10 : "auto", maxHeight: Math.max(120, below ? bottom - 28 : viewport.height - bottom - 96) }}><div className="ai-pet-speech-text">{speech}</div>{onViewDetails && <button className="ai-speech-details" onClick={onViewDetails}>{locale === "en" ? "View details" : "查看详情"} ↗</button>}{sample && <small>{locale === "en" ? "Sample reply" : "示例回复"}</small>}</div> : (hovered || focused) && <span className="ai-pet-hint" role="status">{status}</span>)}
    <button ref={trigger} className="ai-pet" aria-label={locale === "en" ? "Network companion" : "网络小精灵"} aria-expanded={open} title={locale === "en" ? `Viewing ${pageLabel} · Drag or Alt + arrow keys to move` : `正在查看：${pageLabel} · 拖动或 Alt + 方向键移动`} onClick={() => { if (!drag.current?.moved) onOpenChange(!open); }}
      onPointerDown={event => { if (event.button !== 0) return; drag.current = { x: event.clientX, y: event.clientY, right, bottom, moved: false }; event.currentTarget.setPointerCapture(event.pointerId); }}
      onPointerMove={event => { const start = drag.current; if (!start || event.buttons !== 1) return; const dx = event.clientX - start.x, dy = event.clientY - start.y; if (Math.abs(dx) + Math.abs(dy) > 5) start.moved = true; if (start.moved) { setDragging(true); setPosition({ right: Math.max(16, Math.min(start.right - dx, viewport.width - 152)), bottom: Math.max(16, Math.min(start.bottom + dy * -1, viewport.height - 148)) }); } }}
      onPointerUp={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); setDragging(false); }}
      onPointerCancel={() => { drag.current = undefined; setDragging(false); }}
      onKeyDown={event => {
        if (event.key === "Enter" || event.key === " ") drag.current = undefined;
        if (event.altKey && ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(event.key)) {
          event.preventDefault();
          setPosition({ right: Math.max(16, Math.min(right + (event.key === "ArrowLeft" ? 24 : event.key === "ArrowRight" ? -24 : 0), viewport.width - 152)), bottom: Math.max(16, Math.min(bottom + (event.key === "ArrowUp" ? 24 : event.key === "ArrowDown" ? -24 : 0), viewport.height - 148)) });
        }
      }}>
      <span className="ai-pet-aura" />
      {running && !pending && <span className="ai-pet-thinking" aria-label={locale === "en" ? "Thinking" : "正在思考"}><i /><i /><i /></span>}
      <svg className="ai-pet-character" viewBox="0 0 100 100" fill="none" aria-hidden="true">
        <ellipse className="ai-pet-shadow" cx="50" cy="87" rx="24" ry="5" />
        <g className="ai-pet-body">
          <path className="ai-pet-ear" d="M24 40 23 17Q23 11 29 15L43 30M76 40 77 17Q77 11 71 15L57 30" />
          <path className="ai-pet-shell" d="M17 51C17 30 32 23 50 23S83 30 83 51V59C83 77 68 83 50 83S17 77 17 59Z" />
          <path className="ai-pet-shine" d="M27 40Q34 30 48 31" />
          <rect className="ai-pet-face" x="25" y="42" width="50" height="27" rx="13.5" />
          <g className="ai-pet-eyes"><path d="M39 51V57M61 51V57" /></g>
          <path className="ai-pet-mouth" d="M46 61Q50 65 54 61" />
          <circle className="ai-pet-node" cx="50" cy="32" r="3" />
        </g>
      </svg>
      {pending > 0 && <span className="ai-pet-count">{pending}</span>}
    </button>
    <span className="ai-pet-name">{locale === "en" ? "Mux · AI" : "小 Mux · AI"}</span>
  </div>;
}
