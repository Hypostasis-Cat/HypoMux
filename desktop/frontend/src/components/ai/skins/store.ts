import { useEffect, useSyncExternalStore } from "react";
import type { Skin } from "./package";
import { exportSkinAsync, parseSkinAsync } from "./packageAsync";

export interface SkinPreferences { selected: string; sizes: Record<string, number>; animate: boolean }
interface Snapshot { skins: Skin[]; preferences: SkinPreferences; loaded: boolean; error: string }
const defaults: SkinPreferences = { selected: "builtin.default", sizes: {}, animate: true };
export const getSkinSize = (preferences: SkinPreferences, id = preferences.selected) => typeof preferences.sizes[id] === "number" ? preferences.sizes[id] : 112;
let snapshot: Snapshot = { skins: [], preferences: defaults, loaded: false, error: "" };
const listeners = new Set<() => void>();
function publish(next: Snapshot) { snapshot = next; listeners.forEach(listener => listener()); }
let database: Promise<IDBDatabase> | undefined;
function db() {
  if (!database) database = new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open("hypomux-companion-skins", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("items");
    request.onsuccess = () => { request.result.onversionchange = () => { request.result.close(); database = undefined; }; resolve(request.result); };
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error("Close other app windows and retry / 请关闭其他应用窗口后重试"));
  }).catch(error => { database = undefined; throw error; });
  return database;
}
export async function loadSkins() {
  try {
    const database = await db();
    const values = await new Promise<any[]>((resolve, reject) => {
      const tx = database.transaction("items", "readonly"), request = tx.objectStore("items").getAll();
      tx.oncomplete = () => resolve(request.result); tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error);
    });
    const skins: Skin[] = []; let preferences = defaults, error = "", legacySize: number | undefined;
    for (const value of values) {
      if (value?.kind === "preferences") {
        const sizes: Record<string, number> = Object.create(null);
        if (value.sizes && typeof value.sizes === "object" && !Array.isArray(value.sizes)) {
          for (const [id, size] of Object.entries(value.sizes)) if (typeof size === "number" && Number.isFinite(size)) sizes[id] = Math.max(72, Math.min(200, size));
        } else if (typeof value.size === "number" && Number.isFinite(value.size)) legacySize = Math.max(72, Math.min(200, value.size));
        preferences = { selected: typeof value.selected === "string" ? value.selected : defaults.selected, sizes, animate: typeof value.animate === "boolean" ? value.animate : true };
      } else {
        try { skins.push(await parseSkinAsync(value)); } catch { error = "A damaged skin was skipped. Reimport it to repair. / 已跳过损坏皮肤，可重新导入修复。"; }
      }
    }
    if (legacySize !== undefined) preferences = { ...preferences, sizes: Object.fromEntries([defaults.selected, ...skins.map(s => s.manifest.id)].map(id => [id, legacySize!])) };
    if (!skins.some(s => s.manifest.id === preferences.selected)) preferences = { ...preferences, selected: defaults.selected };
    publish({ skins, preferences, loaded: true, error });
  } catch (error) { publish({ ...snapshot, loaded: true, error: `Cannot access skin storage / 无法访问皮肤存储: ${String(error)}` }); }
}
let initialLoad: Promise<void> | undefined;
export function useSkins() {
  const state = useSyncExternalStore(callback => { listeners.add(callback); return () => { listeners.delete(callback); }; }, () => snapshot);
  useEffect(() => { initialLoad ??= loadSkins(); }, []);
  return state;
}
let queue = Promise.resolve();
function mutate(fn: (store: IDBObjectStore) => Snapshot) {
  const work = queue.then(async () => {
    const database = await db();
    let next = snapshot;
    await new Promise<void>((resolve, reject) => {
      const tx = database.transaction("items", "readwrite");
      tx.oncomplete = () => resolve(); tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error);
      try { next = fn(tx.objectStore("items")); } catch (error) { tx.abort(); reject(error); }
    });
    publish(next);
  });
  queue = work.catch(() => {});
  return work;
}
export function savePreferences(patch: Partial<SkinPreferences>) {
  return mutate(store => {
    const preferences = { ...snapshot.preferences, ...patch };
    store.put({ ...preferences, kind: "preferences" }, "preferences");
    return { ...snapshot, preferences };
  });
}
export function saveSkinSize(id: string, size: number) {
  if (!Number.isFinite(size)) return Promise.reject(new Error("Invalid character size"));
  return mutate(store => {
    const preferences = { ...snapshot.preferences, sizes: { ...snapshot.preferences.sizes, [id]: Math.max(72, Math.min(200, size)) } };
    store.put({ ...preferences, kind: "preferences" }, "preferences");
    return { ...snapshot, preferences };
  });
}
export async function installSkin(skin: Skin) {
  const archive = await exportSkinAsync(skin);
  return mutate(store => {
    if (snapshot.skins.length >= 20 && !snapshot.skins.some(s => s.manifest.id === skin.manifest.id)) throw new Error("Keep at most 20 skins / 最多保存 20 个皮肤，请先删除不需要的皮肤");
    store.put(archive, skin.manifest.id);
    store.put({ ...snapshot.preferences, selected: skin.manifest.id, kind: "preferences" }, "preferences");
    return { ...snapshot, skins: [...snapshot.skins.filter(s => s.manifest.id !== skin.manifest.id), skin], preferences: { ...snapshot.preferences, selected: skin.manifest.id } };
  });
}
export function removeSkin(id: string) {
  return mutate(store => {
    store.delete(id);
    const sizes = { ...snapshot.preferences.sizes }; delete sizes[id];
    const preferences = { ...snapshot.preferences, sizes, selected: snapshot.preferences.selected === id ? defaults.selected : snapshot.preferences.selected };
    store.put({ ...preferences, kind: "preferences" }, "preferences");
    return { ...snapshot, skins: snapshot.skins.filter(s => s.manifest.id !== id), preferences };
  });
}
