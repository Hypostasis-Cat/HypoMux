import { useEffect, useRef, useState, type ReactNode } from "react";
import type { Application } from "pixi.js";
import type { Cubism4InternalModel, Live2DModel } from "pixi-live2d-display/cubism4";
import type { Skin, SkinState } from "./package";
import { readLive2DModel } from "./live2dPackage";

let coreReady: Promise<void> | undefined;
function loadCore() {
  coreReady ??= new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "/live2d/live2dcubismcore.min.js";
    script.onload = () => resolve();
    script.onerror = () => { script.remove(); coreReady = undefined; reject(new Error("Live2D Core 加载失败")); };
    document.head.appendChild(script);
  });
  return coreReady;
}

export default function Live2DCharacter({ skin, state, animate, fallback }: { skin: Skin; state: SkinState; animate: boolean; fallback?: ReactNode }) {
  const host = useRef<HTMLSpanElement>(null);
  const current = useRef({ state, animate });
  current.current = { state, animate };
  const sync = useRef<() => void>(() => {});
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  useEffect(() => { sync.current(); }, [state, animate]);
  useEffect(() => {
    const element = host.current!;
    let disposed = false, failed = false, visible = true, raf = 0, last = 0, activeTime = 0;
    let app: Application | undefined, model: Live2DModel<Cubism4InternalModel> | undefined;
    let lastState: SkinState | undefined;
    const urls: string[] = [];
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)");
    const allowed = () => !failed && current.current.animate && visible && !document.hidden && !reduced.matches && !["reduced", "off"].includes(document.documentElement.dataset.motion ?? "");
    const playState = () => {
      if (!model || !allowed() || lastState === current.current.state) return;
      lastState = current.current.state;
      const motion = skin.manifest.live2d!.motions[lastState];
      model.internalModel.motionManager.stopAllMotions();
      if (motion) void model.motion(motion.group, motion.index, 3).catch(fail);
    };
    const frame = (now: number) => {
      raf = 0;
      if (!model || !app || !allowed() || disposed) return;
      if (now - last >= 1000 / 30) {
        const dt = last ? Math.min(now - last, 66) : 33;
        last = now; activeTime += dt;
        try { model.update(dt); app.render(); } catch { fail(); return; }
      }
      raf = requestAnimationFrame(frame);
    };
    const update = () => {
      if (!model || disposed) return;
      playState();
      if (allowed()) { if (!raf) { last = 0; raf = requestAnimationFrame(frame); } }
      else { cancelAnimationFrame(raf); raf = 0; lastState = undefined; }
    };
    sync.current = update;
    const resize = () => {
      if (!app || !model || disposed) return;
      const width = element.clientWidth, height = element.clientHeight;
      if (!width || !height) return;
      app.renderer.resize(width, height);
      model.scale.set(Math.min(width / model.internalModel.width, height / model.internalModel.height) * .94);
      model.position.set(width / 2, height / 2);
      app.render();
    };
    const pointer = (event: PointerEvent) => {
      if (!model || !allowed()) return;
      const bounds = element.getBoundingClientRect();
      model.internalModel.focusController.focus(
        Math.max(-.7, Math.min(.7, (event.clientX - bounds.left - bounds.width / 2) / Math.max(bounds.width * 2, 1))),
        Math.max(-.5, Math.min(.5, -(event.clientY - bounds.top - bounds.height / 2) / Math.max(bounds.height * 2, 1))),
      );
    };
    const resetFocus = () => model?.internalModel.focusController.focus(0, 0);
    const fail = () => { if (!disposed) { failed = true; cancelAnimationFrame(raf); raf = 0; setStatus("error"); } };
    const contextLost = (event: Event) => { event.preventDefault(); fail(); };
    const intersection = new IntersectionObserver(entries => { visible = entries[0]?.isIntersecting ?? true; update(); });
    const observer = new MutationObserver(update);
    const dimensions = new ResizeObserver(resize);
    intersection.observe(element); dimensions.observe(element);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-motion"] });
    document.addEventListener("visibilitychange", update);
    window.addEventListener("pointermove", pointer, { passive: true });
    window.addEventListener("blur", resetFocus);
    reduced.addEventListener("change", update);
    setStatus("loading");
    void (async () => {
      await loadCore();
      const [PIXI, live2d] = await Promise.all([import("pixi.js"), import("pixi-live2d-display/cubism4")]);
      if (disposed) return;
      const settings = new live2d.Cubism4ModelSettings({ ...readLive2DModel(skin.manifest.live2d!, skin.files), url: "local.model3.json" });
      const mapped = new Map<string, string>();
      settings.resolveURL = path => {
        if (disposed) throw new Error("Live2D load cancelled");
        if (!skin.files[path]) throw new Error("Missing local Live2D resource");
        if (!mapped.has(path)) {
          const type = path.endsWith(".png") ? "image/png" : path.endsWith(".json") ? "application/json" : "application/octet-stream";
          const url = URL.createObjectURL(new Blob([new Uint8Array(skin.files[path])], { type }));
          mapped.set(path, url); urls.push(url);
        }
        return mapped.get(path)!;
      };
      app = new PIXI.Application({ width: 200, height: 200, backgroundAlpha: 0, antialias: true, autoStart: false, resolution: Math.min(window.devicePixelRatio || 1, 2), autoDensity: true });
      app.view.addEventListener("webglcontextlost", contextLost);
      element.appendChild(app.view);
      const loaded = await live2d.Live2DModel.from(settings, { autoUpdate: false, autoInteract: false, motionPreload: live2d.MotionPreloadStrategy.ALL }) as Live2DModel<Cubism4InternalModel>;
      if (disposed) { loaded.destroy({ children: true, texture: true, baseTexture: true }); return; }
      model = loaded;
      model.anchor.set(.5, .5);
      app.stage.addChild(model);
      // Physical movement is driven through model parameters, not image transforms.
      model.internalModel.on("beforeModelUpdate", () => {
        if (!model || !allowed()) return;
        const t = activeTime / 1000;
        const core = model.internalModel.coreModel;
        core.addParameterValueById("ParamBodyAngleZ", Math.sin(t * 1.3) * 2);
        if (current.current.state === "dragging") core.addParameterValueById("ParamAngleZ", -12);
      });
      model.update(1); resize(); setStatus("ready"); update();
    })().catch(fail);
    return () => {
      disposed = true; sync.current = () => {}; cancelAnimationFrame(raf);
      intersection.disconnect(); dimensions.disconnect(); observer.disconnect();
      document.removeEventListener("visibilitychange", update);
      window.removeEventListener("pointermove", pointer); window.removeEventListener("blur", resetFocus);
      reduced.removeEventListener("change", update);
      app?.view.removeEventListener("webglcontextlost", contextLost);
      app?.destroy(true, { children: true, texture: true, baseTexture: true });
      urls.forEach(url => URL.revokeObjectURL(url));
    };
  }, [skin]);
  return <span className="mux-live2d" data-live2d-status={status} data-skin-state={state} style={{ aspectRatio: `${skin.manifest.canvas.width} / ${skin.manifest.canvas.height}` }}>
    <span className="mux-live2d-stage" ref={host} style={{ visibility: status === "ready" ? "visible" : "hidden" }} aria-hidden="true" />
    {status === "loading" && <span className="mux-live2d-loading" role="status" aria-label="Live2D loading">···</span>}
    {status === "error" && fallback}
    {status === "error" && <span className="mux-live2d-error" role="status">Live2D 加载失败</span>}
  </span>;
}
