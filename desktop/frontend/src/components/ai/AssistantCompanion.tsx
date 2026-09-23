import { useEffect, useRef, useState, type ReactNode } from "react";
import { DefaultCharacter } from "./skins/DefaultCharacter";
import { SkinCharacter } from "./skins/SkinCharacter";
import { getSkinSize, useSkins } from "./skins/store";
import { useI18n } from "../../i18n/i18n";

export function AssistantCompanion({ open, onOpenChange, running, pending, pageLabel, speech, speechId, sample = false, onViewDetails, onSpeechDismiss, children }: {
  open: boolean; onOpenChange: (open: boolean) => void; running: boolean; pending: number; pageLabel: string; speech?: string; speechId?: string; sample?: boolean; onViewDetails?: () => void; onSpeechDismiss?: () => void; children: ReactNode;
}) {
  const { locale } = useI18n();
  const { skins, preferences } = useSkins();
  const skin = skins.find(s => s.manifest.id === preferences.selected);
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
  const dismissRef = useRef(onSpeechDismiss);
  dismissRef.current = onSpeechDismiss;
  useEffect(() => { setSpeechVisible(true); }, [speech, speechId]);
  useEffect(() => {
    if (!speech || hovered || focused || open || running || pending || !speechVisible) return;
    const timer = window.setTimeout(() => { setSpeechVisible(false); dismissRef.current?.(); }, Math.min(14000, Math.max(6000, speech.length * 90)));
    return () => window.clearTimeout(timer);
  }, [speech, speechId, hovered, focused, open, running, pending, speechVisible]);
  const drag = useRef<{ x: number; y: number; right: number; bottom: number; moved: boolean }>();
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const resize = () => setViewport({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  const ratio = skin ? skin.manifest.canvas.width / skin.manifest.canvas.height : 88 / 92;
  const size = Math.max(32, Math.min(getSkinSize(preferences), viewport.width - 48, viewport.height - 72));
  const petWidth = ratio >= 1 ? size : size * ratio;
  const petHeight = ratio >= 1 ? size / ratio : size;
  const characterState = pending ? "waiting" : running ? "thinking" : dragging ? "dragging" : hovered ? "hover" : speech && speechVisible && !open ? "replying" : "idle";
  const right = Math.max(16, Math.min(position.right, viewport.width - petWidth - 16));
  const bottom = Math.max(16, Math.min(position.bottom, viewport.height - petHeight - 40));
  const leftSide = right > viewport.width / 2;
  const below = bottom > (viewport.height - petHeight - 24) / 2;
  const room = below ? bottom - 18 : viewport.height - bottom - petHeight - 50;
  const bubbleWidth = Math.min(370, viewport.width - 32);
  const bubbleLeft = Math.max(16, Math.min(leftSide ? viewport.width - right - petWidth : viewport.width - right - bubbleWidth, viewport.width - bubbleWidth - 16));
  const speechWidth = Math.min(300, viewport.width - 48);
  const anchorX = viewport.width - right - petWidth + petWidth * (skin?.manifest.anchor.x ?? 0.5);
  const anchorY = bottom + 24 + petHeight * (1 - (skin?.manifest.anchor.y ?? 1));
  const speechLeft = Math.max(16, Math.min(leftSide ? anchorX + 16 : anchorX - 16 - speechWidth, viewport.width - speechWidth - 16));
  const close = () => { onOpenChange(false); trigger.current?.focus(); };
  const status = pending ? (locale === "en" ? "Your approval needed" : "等你确认一下") : running ? (locale === "en" ? "Working on it…" : "正在帮你处理…") : (locale === "en" ? "Ask me anything" : "有问题，叫我就好");
  return <div className={`ai-companion${open ? " is-open" : ""}${dragging ? " is-dragging" : ""}${leftSide ? " dock-left" : ""}${below ? " bubble-below" : ""}`} style={{ right, bottom, width: petWidth }} data-skin-animate={preferences.animate} data-state={pending ? "waiting" : running ? "thinking" : "idle"} onMouseEnter={() => { clearTimeout(hoverTimer.current); setHovered(true); }} onMouseLeave={() => { hoverTimer.current = setTimeout(() => setHovered(false), 250); }} onFocus={() => setFocused(true)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false); }}>
    {open && <section className="ai-pet-bubble" style={{ position: "fixed", width: bubbleWidth, left: bubbleLeft, right: "auto", top: below ? viewport.height - bottom + 10 : "auto", bottom: below ? "auto" : bottom + petHeight + 24, maxHeight: Math.max(80, room) }} aria-label={locale === "en" ? "Companion chat" : "小精灵对话"} onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); close(); } }}>{children}</section>}
    {!open && (speech && !running && !pending ? (speechVisible || hovered || focused) && <div className="ai-pet-speech" role="status" tabIndex={0} style={{ left: speechLeft, width: speechWidth, bottom: below ? "auto" : anchorY + 22, top: below ? viewport.height - bottom + 10 : "auto", maxHeight: Math.max(120, below ? bottom - 28 : viewport.height - anchorY - 48) }}><div className="ai-pet-speech-text">{speech}</div>{onViewDetails && <button className="ai-speech-details" onClick={onViewDetails}>{locale === "en" ? "View details" : "查看详情"} ↗</button>}{sample && <small>{locale === "en" ? "Sample reply" : "示例回复"}</small>}</div> : (hovered || focused) && <span className="ai-pet-hint" role="status" style={{ position: "fixed", left: leftSide ? viewport.width - right + 12 : "auto", right: leftSide ? "auto" : right + petWidth + 12, bottom: below ? "auto" : anchorY + 22, top: below ? viewport.height - bottom + 10 : "auto", maxWidth: Math.min(speechWidth, (leftSide ? right : viewport.width - right - petWidth) - 28), whiteSpace: "normal" }}>{status}</span>)}
    <button ref={trigger} className="ai-pet" style={{ width: petWidth, height: petHeight }} aria-label={locale === "en" ? "Network companion" : "网络小精灵"} aria-expanded={open} title={locale === "en" ? `Viewing ${pageLabel} · Drag or Alt + arrow keys to move` : `正在查看：${pageLabel} · 拖动或 Alt + 方向键移动`} onClick={() => { if (!drag.current?.moved) onOpenChange(!open); }}
      onPointerDown={event => { if (event.button !== 0) return; drag.current = { x: event.clientX, y: event.clientY, right, bottom, moved: false }; event.currentTarget.setPointerCapture(event.pointerId); }}
      onPointerMove={event => { const start = drag.current; if (!start || event.buttons !== 1) return; const dx = event.clientX - start.x, dy = event.clientY - start.y; if (Math.abs(dx) + Math.abs(dy) > 5) start.moved = true; if (start.moved) { setDragging(true); setPosition({ right: Math.max(16, Math.min(start.right - dx, viewport.width - petWidth - 16)), bottom: Math.max(16, Math.min(start.bottom + dy * -1, viewport.height - petHeight - 40)) }); } }}
      onPointerUp={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); setDragging(false); }}
      onPointerCancel={() => { drag.current = undefined; setDragging(false); }}
      onKeyDown={event => {
        if (event.key === "Enter" || event.key === " ") drag.current = undefined;
        if (event.altKey && ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(event.key)) {
          event.preventDefault();
          setPosition({ right: Math.max(16, Math.min(right + (event.key === "ArrowLeft" ? 24 : event.key === "ArrowRight" ? -24 : 0), viewport.width - petWidth - 16)), bottom: Math.max(16, Math.min(bottom + (event.key === "ArrowUp" ? 24 : event.key === "ArrowDown" ? -24 : 0), viewport.height - petHeight - 40)) });
        }
      }}>
      {!skin && <span className="ai-pet-aura" />}
      {running && !pending && <span className="ai-pet-thinking" aria-label={locale === "en" ? "Thinking" : "正在思考"}><i /><i /><i /></span>}
      {skin ? <SkinCharacter skin={skin} state={characterState} animate={preferences.animate} fallback={<DefaultCharacter />} /> : <DefaultCharacter />}
      {pending > 0 && <span className="ai-pet-count">{pending}</span>}
    </button>
    <span className="ai-pet-name">{skin ? `${skin.manifest.name} · AI` : locale === "en" ? "Mux · AI" : "小 Mux · AI"}</span>
  </div>;
}
