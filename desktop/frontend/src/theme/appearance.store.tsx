import { createContext, useContext, useEffect, useMemo, useState, type PropsWithChildren } from "react";
import { desktopPlatform } from "../platform/desktop";
import { appearancePersistence, loadLegacyBrowserAppearance } from "./background.service";
import { appearancePresets, defaultAppearance, getAppearancePreset, resolveAccent } from "./appearance.presets";
import { createHypoMuxTheme } from "./createFluentTheme";
import type {
  AppearancePresetId,
  AppearanceSettings,
  NativeAppearanceResult,
  ResolvedAppearance,
} from "./appearance.types";

type AppearanceContextValue = {
  settings: AppearanceSettings;
  resolvedMode: ResolvedAppearance;
  accent: string;
  fluentTheme: ReturnType<typeof createHypoMuxTheme>;
  nativeResult: NativeAppearanceResult;
  persistenceError?: string;
  update: (patch: Partial<AppearanceSettings>) => void;
  applyPreset: (id: AppearancePresetId) => void;
  reset: () => void;
};

const AppearanceContext = createContext<AppearanceContextValue | null>(null);

const getSystemMode = (): ResolvedAppearance =>
  window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";

const clamp = (value: number, minimum: number, maximum: number) =>
  Math.min(maximum, Math.max(minimum, Number.isFinite(value) ? value : minimum));

const normaliseAppearance = (value: AppearanceSettings): AppearanceSettings => ({
  ...value,
  material: value.material === "solid" ? "solid" : "mica",
  panelMaterial: value.panelMaterial === "solid" ? "solid" : "blur",
  presetId: value.presetId === "pure-performance" ? "pure-performance" : "windows-mica",
  panelOpacity: clamp(value.panelOpacity, 0, 100),
  panelBlur: clamp(value.panelBlur, 0, 40),
  backgroundBlur: 0,
});

const getInitialAppearance = (): AppearanceSettings => {
  const saved = loadLegacyBrowserAppearance();
  let initial = saved ? { ...defaultAppearance, ...saved } : { ...defaultAppearance };

  if (import.meta.env.DEV) {
    const query = new URLSearchParams(window.location.search);
    const preset = query.get("preset") as AppearancePresetId | null;
    const mode = query.get("mode");
    if (preset && appearancePresets.some((item) => item.id === preset)) {
      initial = { ...getAppearancePreset(preset).settings };
    }
    if (mode === "light" || mode === "dark" || mode === "system") {
      initial.mode = mode;
    }
  }
  return normaliseAppearance(initial);
};

const applyDocumentTokens = (settings: AppearanceSettings, resolvedMode: ResolvedAppearance, accent: string) => {
  const root = document.documentElement;
  root.dataset.appearance = resolvedMode;
  root.dataset.material = settings.material;
  root.dataset.panelMaterial = settings.panelMaterial;
  root.dataset.backgroundSource = settings.backgroundSource;
  root.dataset.density = settings.density;
  root.dataset.motion = settings.motion;
  root.style.setProperty("--hm-accent", accent);
  root.style.setProperty("--hm-panel-opacity", `${settings.panelOpacity / 100}`);
  root.style.setProperty("--hm-panel-blur", `${settings.panelBlur}px`);
  root.style.setProperty("--hm-panel-saturation", `${settings.panelSaturation}%`);
  root.style.setProperty("--hm-border-brightness", `${settings.borderBrightness / 100}`);
  root.style.setProperty("--hm-shadow-strength", `${settings.shadowStrength / 100}`);
  root.style.setProperty("--hm-radius", `${settings.radius}px`);
  root.style.setProperty("--hm-bg-brightness", `${settings.backgroundBrightness}%`);
  root.style.setProperty("--hm-bg-saturation", `${settings.backgroundSaturation}%`);
  root.style.setProperty("--hm-bg-contrast", `${settings.backgroundContrast}%`);
  root.style.setProperty("--hm-bg-overlay", `${settings.backgroundOverlay / 100}`);
  root.style.setProperty("--hm-bg-blur", `${settings.backgroundBlur}px`);
  root.style.setProperty("--hm-bg-scale", settings.backgroundScale);
  root.style.setProperty("--hm-bg-position", settings.backgroundAlignment);
  root.style.setProperty("--hm-solid-background", settings.solidBackground);
  root.style.setProperty("--hm-gradient-background", settings.gradientBackground);
};

export function AppearanceProvider({ children }: PropsWithChildren) {
  const [settings, setSettings] = useState<AppearanceSettings>(getInitialAppearance);
  const [hydrated, setHydrated] = useState(false);
  const [systemMode, setSystemMode] = useState<ResolvedAppearance>(getSystemMode);
  const [nativeResult, setNativeResult] = useState<NativeAppearanceResult>({ applied: false, fallback: true });
  const [persistenceError, setPersistenceError] = useState<string>();
  const resolvedMode = settings.mode === "system" ? systemMode : settings.mode;
  const accent = resolveAccent(settings);

  useEffect(() => {
    let active = true;
    appearancePersistence.load()
      .then((saved) => {
        if (active && saved) setSettings(normaliseAppearance({ ...defaultAppearance, ...saved }));
      })
      .catch((error) => console.error("Unable to load appearance settings", error))
      .finally(() => {
        if (active) setHydrated(true);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setSystemMode(query.matches ? "dark" : "light");
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    applyDocumentTokens(settings, resolvedMode, accent);
    if (hydrated) {
      void appearancePersistence.save(settings)
        .then(() => setPersistenceError(undefined))
        .catch((error) => {
          const message = error instanceof Error ? error.message : String(error);
          setPersistenceError(message);
          console.error("Unable to save appearance settings", error);
        });
    }
    desktopPlatform
      .setWindowAppearance({ material: settings.material, mode: resolvedMode, accent })
      .then(setNativeResult);
  }, [settings, resolvedMode, accent, hydrated]);

  const value = useMemo<AppearanceContextValue>(
    () => ({
      settings,
      resolvedMode,
      accent,
      fluentTheme: createHypoMuxTheme(resolvedMode, accent),
      nativeResult,
      persistenceError,
      update: (patch) => setSettings((current) => normaliseAppearance({ ...current, ...patch })),
      applyPreset: (id) => setSettings({ ...getAppearancePreset(id).settings, mode: settings.mode }),
      reset: () => setSettings({ ...appearancePresets[0].settings }),
    }),
    [accent, nativeResult, persistenceError, resolvedMode, settings],
  );

  return <AppearanceContext.Provider value={value}>{children}</AppearanceContext.Provider>;
}

export const useAppearance = () => {
  const context = useContext(AppearanceContext);
  if (!context) {
    throw new Error("useAppearance must be used inside AppearanceProvider");
  }
  return context;
};
