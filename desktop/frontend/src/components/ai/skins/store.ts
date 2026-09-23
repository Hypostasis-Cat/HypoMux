import { useEffect, useSyncExternalStore } from "react";
import { exportSkin, parseSkin, type Skin } from "./package";

export interface SkinPreferences { selected: string; size: number; animate: boolean }
interface Snapshot { skins: Skin[]; preferences: SkinPreferences; loaded: boolean; error: string }
const defaults: SkinPreferences = { selected: "builtin.default", size: 112, animate: true };
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
    const skins: Skin[] = []; let preferences = defaults, error = "";
    for (const value of values) {
      if (value?.kind === "preferences") {
        preferences = { selected: typeof value.selected === "string" ? value.selected : defaults.selected, size: typeof value.size === "number" && Number.isFinite(value.size) ? Math.max(72, Math.min(200, value.size)) : defaults.size, animate: typeof value.animate === "boolean" ? value.animate : true };
      } else {
        try { skins.push(parseSkin(value)); } catch { error = "A damaged skin was skipped. Reimport it to repair. / 已跳过损坏皮肤，可重新导入修复。"; }
      }
    }
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
export function installSkin(skin: Skin) {
  const archive = exportSkin(skin);
  parseSkin(archive);
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
    if (snapshot.preferences.selected === id) store.put({ ...snapshot.preferences, selected: defaults.selected, kind: "preferences" }, "preferences");
    return { ...snapshot, skins: snapshot.skins.filter(s => s.manifest.id !== id), preferences: { ...snapshot.preferences, selected: snapshot.preferences.selected === id ? defaults.selected : snapshot.preferences.selected } };
  });
}
