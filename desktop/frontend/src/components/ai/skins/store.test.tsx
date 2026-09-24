// @vitest-environment jsdom
import "fake-indexeddb/auto";
import { readFileSync } from "node:fs";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { installSkin, loadSkins, removeSkin, savePreferences, saveSkinSize, getSkinSize, useSkins } from "./store";
import { parseSkin } from "./package";

afterEach(cleanup);
it("persists companion visibility across reloads", async () => {
  const { result } = renderHook(useSkins);
  await waitFor(() => expect(result.current.loaded).toBe(true));
  await act(() => savePreferences({ visible: false }));
  await act(() => loadSkins());
  expect(result.current.preferences.visible).toBe(false);
  await act(() => savePreferences({ visible: true }));
  await act(() => loadSkins());
  expect(result.current.preferences.visible).toBe(true);
});

it("persists installation and settings, reloads, and restores default when active skin is removed", async () => {
  const skin = parseSkin(new Uint8Array(readFileSync("public/skins/mux-starter.muxskin")));
  const { result } = renderHook(useSkins);
  await waitFor(() => expect(result.current.loaded).toBe(true));
  await act(() => installSkin(skin));
  expect(result.current.preferences.selected).toBe(skin.manifest.id);
  await act(() => Promise.all([saveSkinSize(skin.manifest.id, 192), savePreferences({ animate: false })]));
  await act(() => loadSkins());
  expect(result.current.skins).toHaveLength(1);
  expect(result.current.preferences).toEqual({ sizes: { [skin.manifest.id]: 192 }, animate: false, visible: true, selected: skin.manifest.id });
  await act(() => installSkin({ ...skin, manifest: { ...skin.manifest, name: "Updated" } }));
  expect(result.current.skins).toHaveLength(1);
  expect(result.current.skins[0].manifest.name).toBe("Updated");
  await expect(installSkin({ ...skin, manifest: { ...skin.manifest, schemaVersion: 2 as 1 } })).rejects.toThrow();
  expect(result.current.preferences.selected).toBe(skin.manifest.id);
  await act(() => removeSkin(skin.manifest.id));
  await act(() => loadSkins());
  expect(result.current.skins).toHaveLength(0);
  expect(result.current.preferences.selected).toBe("builtin.default");
});

it("keeps independent sizes through switching, replacement and reload", async () => {
  const skin = parseSkin(new Uint8Array(readFileSync("public/skins/mux-starter.muxskin")));
  const { result } = renderHook(useSkins);
  await act(() => installSkin(skin));
  await act(() => Promise.all([saveSkinSize("builtin.default", 88), saveSkinSize(skin.manifest.id, 192)]));
  await act(() => savePreferences({ selected: "builtin.default" }));
  expect(getSkinSize(result.current.preferences)).toBe(88);
  await act(() => installSkin(skin));
  await act(() => loadSkins());
  expect(getSkinSize(result.current.preferences)).toBe(192);
  expect(getSkinSize(result.current.preferences, "builtin.default")).toBe(88);
  expect(getSkinSize(result.current.preferences, "local.new")).toBe(112);
  await act(() => removeSkin(skin.manifest.id));
  expect(getSkinSize(result.current.preferences)).toBe(88);
  expect(result.current.preferences.sizes[skin.manifest.id]).toBeUndefined();
});

it("migrates the legacy shared size without coupling later adjustments", async () => {
  const skin = parseSkin(new Uint8Array(readFileSync("public/skins/mux-starter.muxskin")));
  const { result } = renderHook(useSkins);
  await act(() => installSkin(skin));
  await new Promise<void>((resolve, reject) => {
    const request = indexedDB.open("hypomux-companion-skins", 1);
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const db = request.result, tx = db.transaction("items", "readwrite");
      tx.objectStore("items").put({ kind: "preferences", selected: skin.manifest.id, size: 176, animate: true }, "preferences");
      tx.oncomplete = () => { db.close(); resolve(); };
      tx.onerror = () => reject(tx.error);
    };
  });
  await act(() => loadSkins());
  expect(getSkinSize(result.current.preferences)).toBe(176);
  expect(result.current.preferences.visible).toBe(true);
  expect(getSkinSize(result.current.preferences, "builtin.default")).toBe(176);
  await act(() => saveSkinSize("builtin.default", 88));
  await act(() => loadSkins());
  expect(getSkinSize(result.current.preferences)).toBe(176);
  expect(getSkinSize(result.current.preferences, "builtin.default")).toBe(88);
});
