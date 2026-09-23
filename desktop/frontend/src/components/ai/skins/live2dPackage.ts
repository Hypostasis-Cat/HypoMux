import { safePath, skinStates, type SkinState } from "./package";

export interface Live2DSkin {
  model: string;
  motions: Partial<Record<SkinState, { group: string; index: number }>>;
}
const object = (v: unknown): v is Record<string, any> => !!v && typeof v === "object" && !Array.isArray(v);
const bad = (): never => { throw new Error("Invalid Live2D resources / Live2D 模型或动作引用无效"); };
export function validateLive2D(value: unknown, files?: Record<string, Uint8Array>): Live2DSkin {
  if (!object(value) || typeof value.model !== "string" || !safePath(value.model) || !value.model.endsWith(".model3.json") || !object(value.motions)) return bad();
  const motions: Live2DSkin["motions"] = {};
  for (const [state, motion] of Object.entries(value.motions)) {
    if (!skinStates.includes(state as SkinState) || !object(motion) || !/^[A-Za-z][A-Za-z0-9_-]{0,39}$/.test(motion.group) || !Number.isInteger(motion.index) || motion.index < 0 || motion.index > 119) return bad();
    motions[state as SkinState] = { group: motion.group, index: motion.index };
  }
  const config = { model: value.model, motions };
  if (files) {
    const model = readLive2DModel(config, files);
    for (const motion of Object.values(motions)) if (!model.FileReferences.Motions[motion.group]?.[motion.index]) return bad();
  }
  return config;
}

// Rebuild settings from a closed set of data fields. Viewer commands, audio,
// remote URLs and arbitrary settings from third-party models are never loaded.
export function readLive2DModel(config: Live2DSkin, files: Record<string, Uint8Array>) {
  const json = (path: string) => {
    const data = files[path];
    if (!data || data.length > 2 * 1024 * 1024) return bad();
    try { const parsed = JSON.parse(new TextDecoder().decode(data)); if (!object(parsed)) return bad(); return parsed; } catch { return bad(); }
  };
  const input = json(config.model), refs = input.FileReferences;
  if (input.Version !== 3 || !object(refs) || !Array.isArray(refs.Textures) || !refs.Textures.length || refs.Textures.length > 8) return bad();
  const base = config.model.slice(0, config.model.lastIndexOf("/") + 1);
  const resource = (path: unknown, suffix: string) => {
    if (typeof path !== "string" || !safePath(path) || !path.endsWith(suffix) || !files[base + path]) return bad();
    if (suffix.endsWith(".json")) json(base + path);
    return base + path;
  };
  const moc = resource(refs.Moc, ".moc3");
  if (new TextDecoder().decode(files[moc].subarray(0, 4)) !== "MOC3") return bad();
  const motions: Record<string, { File: string }[]> = Object.create(null);
  if (refs.Motions !== undefined && !object(refs.Motions)) return bad();
  for (const [group, entries] of Object.entries(refs.Motions ?? {})) {
    if (!/^[A-Za-z][A-Za-z0-9_-]{0,39}$/.test(group) || !Array.isArray(entries) || entries.length > 120) return bad();
    motions[group] = entries.map(entry => { if (!object(entry)) return bad(); return { File: resource(entry.File, ".motion3.json") }; });
  }
  const expressions = refs.Expressions;
  if (expressions !== undefined && (!Array.isArray(expressions) || expressions.length > 32)) return bad();
  const groups = input.Groups;
  if (groups !== undefined && (!Array.isArray(groups) || groups.length > 16)) return bad();
  return {
    Version: 3,
    FileReferences: {
      Moc: moc, Textures: refs.Textures.map((p: unknown) => resource(p, ".png")), Motions: motions,
      ...(refs.Physics ? { Physics: resource(refs.Physics, ".physics3.json") } : {}),
      ...(refs.Pose ? { Pose: resource(refs.Pose, ".pose3.json") } : {}),
      ...(expressions ? { Expressions: expressions.map((e: any) => {
        if (!object(e) || typeof e.Name !== "string" || e.Name.length > 80) return bad();
        return { Name: e.Name, File: resource(e.File, ".exp3.json") };
      }) } : {}),
    },
    Groups: (groups ?? []).map((g: any) => {
      if (!object(g) || g.Target !== "Parameter" || !["EyeBlink", "LipSync"].includes(g.Name) || !Array.isArray(g.Ids) || g.Ids.length > 64 || g.Ids.some((id: unknown) => typeof id !== "string" || id.length > 100)) return bad();
      return { Target: "Parameter", Name: g.Name, Ids: g.Ids as string[] };
    }),
  };
}
