import { lazy, Suspense, useEffect, useRef, useState, type ReactNode } from "react";
import LayeredCharacter from "./LayeredCharacter";
import { type Skin, type SkinState } from "./package";

const Live2DCharacter = lazy(() => import("./Live2DCharacter"));
interface CharacterProps { skin: Skin; state: SkinState; animate: boolean; fallback?: ReactNode; onError?: () => void; posterOnly?: boolean }
export function SkinCharacter(props: CharacterProps) {
  const poster = <PNGCharacter {...props} animate={props.skin.manifest.live2d ? false : props.animate} />;
  if (props.skin.manifest.layered && !props.posterOnly) return <LayeredCharacter {...props} fallback={poster} />;
  return props.skin.manifest.live2d && !props.posterOnly
    ? <Suspense fallback={<span className="mux-live2d-loading" role="status" aria-label="Live2D loading">···</span>}><Live2DCharacter {...props} fallback={poster} /></Suspense>
    : poster;
}
function PNGCharacter({ skin, state, animate, fallback, onError }: CharacterProps) {
  const animation = skin.manifest.states[state] ?? skin.manifest.states.idle;
  const [url, setURL] = useState<{ src: string; url: string; skin: Skin }>();
  const [failed, setFailed] = useState(false);
  const [frame, setFrame] = useState(0);
  const [playing, setPlaying] = useState(false);
  const element = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    setFailed(false);
    const bytes = skin.files[animation.src];
    if (!bytes) { setFailed(true); return; }
    const next = URL.createObjectURL(new Blob([new Uint8Array(bytes)], { type: "image/png" }));
    const img = new Image();
    img.onerror = () => { setFailed(true); onError?.(); };
    img.src = next;
    setURL({ src: animation.src, url: next, skin });
    return () => { img.onerror = null; URL.revokeObjectURL(next); };
  }, [skin, animation.src, onError]);
  useEffect(() => {
    setFrame(0);
    setPlaying(false);
    if (!animate || failed) return;
    const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    let timer: ReturnType<typeof setInterval> | undefined, visible = true, current = 0;
    const update = () => {
      clearInterval(timer);
      if (!visible || document.hidden || reduced?.matches || ["reduced", "off"].includes(document.documentElement.dataset.motion ?? "")) { setPlaying(false); setFrame(0); current = 0; return; }
      setPlaying(true);
      if (animation.type !== "spritesheet") return;
      timer = setInterval(() => {
        current++;
        if (current >= animation.frames) {
          if (!animation.loop) { clearInterval(timer); return; }
          current = 0;
        }
        setFrame(current);
      }, 1000 / animation.fps);
    };
    const observer = typeof IntersectionObserver === "undefined" ? undefined : new IntersectionObserver(entries => { visible = entries[0]?.isIntersecting ?? true; update(); });
    if (element.current) observer?.observe(element.current);
    const motionObserver = new MutationObserver(update);
    motionObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["data-motion"] });
    document.addEventListener("visibilitychange", update); reduced?.addEventListener("change", update); update();
    return () => { clearInterval(timer); observer?.disconnect(); motionObserver.disconnect(); document.removeEventListener("visibilitychange", update); reduced?.removeEventListener("change", update); };
  }, [animation, animate, state, failed]);
  if (failed) return <>{fallback}</>;
  const columns = animation.type === "image" ? 1 : animation.columns;
  const rows = animation.type === "image" ? 1 : Math.ceil(animation.frames / columns);
  const index = animation.type === "image" ? 0 : Math.min(frame, animation.frames - 1);
  const x = index % columns, y = Math.floor(index / columns);
  return <span ref={element} className="mux-skin-character" aria-hidden="true" data-skin-state={state} data-skin-motion={animation.type === "image" && animate && playing ? state : undefined} style={{
    width: skin.manifest.canvas.width >= skin.manifest.canvas.height ? "100%" : "auto",
    height: skin.manifest.canvas.width >= skin.manifest.canvas.height ? "auto" : "100%",
    aspectRatio: `${skin.manifest.canvas.width} / ${skin.manifest.canvas.height}`,
    backgroundImage: url?.src === animation.src && url.skin === skin ? `url("${url.url}")` : undefined,
    backgroundSize: `${columns * 100}% ${rows * 100}%`,
    backgroundPosition: `${columns === 1 ? 0 : x / (columns - 1) * 100}% ${rows === 1 ? 0 : y / (rows - 1) * 100}%`,
  }} />;
}
