import { useEffect, useRef, useState, type ReactNode } from "react";
import type { Skin, SkinState } from "./package";
import type { SkinLayer } from "./layeredPackage";

export function layerAnimation(layer: SkinLayer, state: SkinState): { frames: Keyframe[]; duration: number; iterations: number } | undefined {
  const motion = layer.motions?.[state] ?? layer.motions?.idle;
  if (motion) return {
    frames: motion.frames.map(f => ({ transform: `translate(${(f.x ?? 0) * 100}%, ${(f.y ?? 0) * 100}%) rotate(${f.rotate ?? 0}deg) scale(${f.scaleX ?? 1}, ${f.scaleY ?? 1})`, opacity: f.opacity ?? 1 })),
    duration: motion.duration, iterations: motion.loop ? Infinity : 1,
  };
  if (layer.role === "eyes") return { frames: [
    { transform: "scaleY(1)", offset: 0 }, { transform: "scaleY(1)", offset: .9 },
    { transform: "scaleY(.08)", offset: .94 }, { transform: "scaleY(1)", offset: .98 }, { transform: "scaleY(1)", offset: 1 },
  ], duration: state === "thinking" ? 2600 : 4200, iterations: Infinity };
  if (layer.role === "mouth" && state === "replying") return { frames: [{ transform: "scaleY(1)" }, { transform: "scaleY(1.8)" }, { transform: "scaleY(1)" }], duration: 320, iterations: Infinity };
  if (layer.role === "decoration") return { frames: [{ transform: "rotate(-3deg)" }, { transform: "rotate(3deg)" }, { transform: "rotate(-3deg)" }], duration: 2800, iterations: Infinity };
}

export default function LayeredCharacter({ skin, state, animate, fallback, onError }: {
  skin: Skin; state: SkinState; animate: boolean; fallback?: ReactNode; onError?: () => void;
}) {
  const root = useRef<HTMLSpanElement>(null);
  const [assets, setAssets] = useState<{ skin: Skin; urls: Record<string, string> }>();
  const [failed, setFailed] = useState(false);
  const layers = skin.manifest.layered!.layers;
  useEffect(() => {
    const urls: Record<string, string> = {};
    setFailed(false);
    for (const layer of layers) {
      const bytes = skin.files[layer.src];
      if (!bytes) { setFailed(true); continue; }
      if (!urls[layer.src]) urls[layer.src] = URL.createObjectURL(new Blob([new Uint8Array(bytes)], { type: "image/png" }));
    }
    setAssets({ skin, urls });
    return () => { Object.values(urls).forEach(url => URL.revokeObjectURL(url)); };
  }, [skin, layers]);
  useEffect(() => {
    const element = root.current;
    if (!element || !animate || failed || assets?.skin !== skin || !element.animate) return;
    const animations: Animation[] = [];
    const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    let visible = true;
    const stop = () => { animations.splice(0).forEach(a => a.cancel()); };
    const update = () => {
      stop();
      if (!visible || document.hidden || reduced?.matches || ["off", "reduced"].includes(document.documentElement.dataset.motion ?? "")) return;
      const tilt = state === "thinking" ? -5 : state === "dragging" ? -9 : state === "hover" ? 5 : 0;
      const rise = state === "hover" ? -4 : state === "waiting" ? -1 : -2;
      animations.push(element.animate([
        { transform: `translateY(0) rotate(${tilt}deg) scale(1)` },
        { transform: `translateY(${rise}%) rotate(${tilt + 1}deg) scale(1.015)` },
        { transform: `translateY(0) rotate(${tilt}deg) scale(1)` },
      ], { duration: 3200, iterations: Infinity, easing: "ease-in-out" }));
      Array.from(element.children).forEach((child, i) => {
        const motion = layerAnimation(layers[i], state);
        if (motion) animations.push(child.animate(motion.frames, { duration: motion.duration, iterations: motion.iterations, easing: "ease-in-out", fill: "forwards" }));
      });
    };
    const intersection = typeof IntersectionObserver === "undefined" ? undefined : new IntersectionObserver(entries => { visible = entries[0]?.isIntersecting ?? true; update(); });
    intersection?.observe(element);
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-motion"] });
    document.addEventListener("visibilitychange", update); reduced?.addEventListener("change", update); update();
    return () => { stop(); intersection?.disconnect(); observer.disconnect(); document.removeEventListener("visibilitychange", update); reduced?.removeEventListener("change", update); };
  }, [skin, assets, layers, state, animate, failed]);
  if (failed) return <>{fallback}</>;
  return <span ref={root} className="mux-layered-character" aria-hidden="true" style={{
    width: skin.manifest.canvas.width >= skin.manifest.canvas.height ? "100%" : "auto",
    height: skin.manifest.canvas.width >= skin.manifest.canvas.height ? "auto" : "100%",
    aspectRatio: `${skin.manifest.canvas.width} / ${skin.manifest.canvas.height}`,
  }}>{assets?.skin === skin && layers.map(layer => <img key={layer.id} alt="" draggable={false}
    src={assets.urls[layer.src]} onError={() => { setFailed(true); onError?.(); }}
    style={{ position: "absolute", left: `${layer.x * 100}%`, top: `${layer.y * 100}%`, width: `${layer.width * 100}%`, height: `${layer.height * 100}%`, transformOrigin: `${layer.pivot.x * 100}% ${layer.pivot.y * 100}%` }} />)}</span>;
}
