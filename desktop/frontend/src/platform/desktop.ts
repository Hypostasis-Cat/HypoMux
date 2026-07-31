import { Application, Browser, Call, Window } from "@wailsio/runtime";
import * as DesktopHost from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform/wails/desktophost";
import type { NativeAppearanceResult, ResolvedAppearance, WindowMaterial } from "../theme/appearance.types";

const ignoreOutsideWails = (error: unknown) => {
  if (import.meta.env.DEV) {
    console.debug("Native desktop action is unavailable in browser preview.", error);
  }
};

const callAppearance = async (method: string, value: string): Promise<NativeAppearanceResult> => {
  try {
    if (method === "SetWindowMaterial") {
      return await DesktopHost.SetWindowMaterial(value);
    }
    if (method === "SetWindowTheme") {
      return await DesktopHost.SetWindowTheme(value);
    }
    return await DesktopHost.SetWindowAccent(value);
  } catch (error) {
    ignoreOutsideWails(error);
    return { applied: false, fallback: true, reason: "使用 Web/CSS 材质回退" };
  }
};

// This facade is the only frontend module allowed to import the Wails runtime.
// Browser previews intentionally degrade to no-ops.
export const desktopPlatform = {
  minimise: () => Window.Minimise().catch(ignoreOutsideWails),
  toggleMaximise: () => Window.ToggleMaximise().catch(ignoreOutsideWails),
  close: () => Window.Close().catch(ignoreOutsideWails),
  hideToTray: () => Window.Hide().catch(ignoreOutsideWails),
  showStartup: () =>
    (Call.ByName(
      "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform/wails.DesktopHost.ShowStartup",
    ) as Promise<void>).catch(ignoreOutsideWails),
  quit: () => Application.Quit().catch(ignoreOutsideWails),
  openDirectory: (path: string) => DesktopHost.OpenDirectory(path).catch(ignoreOutsideWails),
  openURL: (url: string) => Browser.OpenURL(url).catch(ignoreOutsideWails),
  setEngineTrayStatus: (phase: string, mode: string) =>
    Call.ByName(
      "github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform/wails.DesktopHost.SetEngineTrayStatus",
      phase,
      mode,
    ).catch(ignoreOutsideWails),
  setWindowMaterial: (material: WindowMaterial) => callAppearance("SetWindowMaterial", material),
  setWindowTheme: (mode: ResolvedAppearance) => callAppearance("SetWindowTheme", mode),
  setWindowAccent: (accent: string) => callAppearance("SetWindowAccent", accent),
  async setWindowAppearance({
    material,
    mode,
    accent,
  }: {
    material: WindowMaterial;
    mode: ResolvedAppearance;
    accent: string;
  }): Promise<NativeAppearanceResult> {
    const results = await Promise.all([
      callAppearance("SetWindowMaterial", material),
      callAppearance("SetWindowTheme", mode),
      callAppearance("SetWindowAccent", accent),
    ]);
    const failed = results.find((result) => result.fallback);
    return failed ?? { applied: true, fallback: false };
  },
};
