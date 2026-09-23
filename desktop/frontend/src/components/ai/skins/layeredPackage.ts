import { safePath, skinStates, type SkinState } from "./package";

export type LayerPose = { x?: number; y?: number; rotate?: number; scaleX?: number; scaleY?: number; opacity?: number };
export type LayerMotion = { duration: number; loop: boolean; frames: LayerPose[] };
export type SkinLayer = {
  id: string; src: string; role: "body" | "eyes" | "mouth" | "decoration";
  x: number; y: number; width: number; height: number; pivot: { x: number; y: number };
  motions?: Partial<Record<SkinState, LayerMotion>>;
};
export type LayeredSkin = { layers: SkinLayer[] };
const object = (v: unknown): v is Record<string, any> => !!v && typeof v === "object" && !Array.isArray(v);
const number = (v: unknown, min: number, max: number): v is number => typeof v === "number" && Number.isFinite(v) && v >= min && v <= max;
const invalid = (): never => { throw new Error("Invalid layered skin / 分层皮肤参数无效"); };

export function validateLayered(value: unknown): LayeredSkin {
  if (!object(value) || !Array.isArray(value.layers) || value.layers.length < 1 || value.layers.length > 16) return invalid();
  const ids = new Set<string>();
  const layers: SkinLayer[] = value.layers.map((v: unknown) => {
    if (!object(v) || typeof v.id !== "string" || !/^[a-z][a-z0-9-]{0,31}$/.test(v.id) || ids.has(v.id)
      || typeof v.src !== "string" || !safePath(v.src) || !v.src.endsWith(".png")
      || !["body", "eyes", "mouth", "decoration"].includes(v.role)) return invalid();
    ids.add(v.id);
    const x = v.x ?? 0, y = v.y ?? 0, width = v.width ?? 1, height = v.height ?? 1, pivot = v.pivot ?? { x: .5, y: .5 };
    if (!number(x, -1, 1) || !number(y, -1, 1) || !number(width, .01, 2) || !number(height, .01, 2)
      || !object(pivot) || !number(pivot.x, 0, 1) || !number(pivot.y, 0, 1)) return invalid();
    const motions: Partial<Record<SkinState, LayerMotion>> = {};
    if (v.motions !== undefined) {
      if (!object(v.motions) || Object.keys(v.motions).some(s => !skinStates.includes(s as SkinState))) return invalid();
      for (const state of skinStates) {
        const m = v.motions[state];
        if (m === undefined) continue;
        if (!object(m) || !number(m.duration, 200, 30000) || typeof m.loop !== "boolean" || !Array.isArray(m.frames) || m.frames.length < 2 || m.frames.length > 16) return invalid();
        const frames = m.frames.map((f: unknown): LayerPose => {
          if (!object(f)) return invalid();
          const result: LayerPose = {};
          for (const [key, min, max] of [["x", -.5, .5], ["y", -.5, .5], ["rotate", -180, 180], ["scaleX", .05, 3], ["scaleY", .05, 3], ["opacity", 0, 1]] as const) {
            if (f[key] !== undefined) { if (!number(f[key], min, max)) return invalid(); result[key] = f[key]; }
          }
          if (Object.keys(f).some(k => !["x", "y", "rotate", "scaleX", "scaleY", "opacity"].includes(k))) return invalid();
          return result;
        });
        motions[state] = { duration: m.duration, loop: m.loop, frames };
      }
    }
    return { id: v.id, src: v.src, role: v.role, x, y, width, height, pivot: { x: pivot.x, y: pivot.y }, motions };
  });
  if (layers.filter(l => l.role === "body").length !== 1) return invalid();
  return { layers };
}
