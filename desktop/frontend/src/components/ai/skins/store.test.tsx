// @vitest-environment jsdom
import "fake-indexeddb/auto";
import { readFileSync } from "node:fs";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { installSkin, loadSkins, removeSkin, savePreferences, useSkins } from "./store";
import { parseSkin } from "./package";

afterEach(cleanup);
it("persists installation and settings, reloads, and restores default when active skin is removed", async () => {
  const skin = parseSkin(new Uint8Array(readFileSync("public/skins/mux-starter.muxskin")));
  const { result } = renderHook(useSkins);
  await waitFor(() => expect(result.current.loaded).toBe(true));
  await act(() => installSkin(skin));
  expect(result.current.preferences.selected).toBe(skin.manifest.id);
  await act(() => Promise.all([savePreferences({ size: 192 }), savePreferences({ animate: false })]));
  await act(() => loadSkins());
  expect(result.current.skins).toHaveLength(1);
  expect(result.current.preferences).toEqual({ size: 192, animate: false, selected: skin.manifest.id });
  await act(() => installSkin({ ...skin, manifest: { ...skin.manifest, name: "Updated" } }));
  expect(result.current.skins).toHaveLength(1);
  expect(result.current.skins[0].manifest.name).toBe("Updated");
  expect(() => installSkin({ ...skin, manifest: { ...skin.manifest, schemaVersion: 2 as 1 } })).toThrow();
  expect(result.current.preferences.selected).toBe(skin.manifest.id);
  await act(() => removeSkin(skin.manifest.id));
  await act(() => loadSkins());
  expect(result.current.skins).toHaveLength(0);
  expect(result.current.preferences.selected).toBe("builtin.default");
});
