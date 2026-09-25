import { createContext, useContext, useEffect, useMemo, useRef, useState, type PropsWithChildren } from "react";
import { desktopPlatform } from "../platform/desktop";
import { appearancePersistence, loadLegacyBrowserAppearance } from "./background.service";
import { appearancePresets, defaultAppearance, getAppearancePreset, resolveAccent } from "./appearance.presets";
import { createHypoMuxTheme } from "./createFluentTheme";
import { builtinBackgroundSizes } from "./wallpaper";
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

const migrateAppearance = (value: Partial<AppearanceSettings>): Partial<AppearanceSettings> => {
  // Schema version 2: no forced migration to avoid breaking user preferences
  // Users who prefer fluent-solid should keep their choice
  return { ...value, schemaVersion: 2 };
};

const normaliseAppearance = (value: AppearanceSettings): AppearanceSettings => ({
  ...value,
  schemaVersion: 2,
  builtinBackground: value.builtinBackground === "soft-bloom" ? "soft-bloom" : "soft-dots",
  material: value.material === "solid" ? "solid" : "mica",
  panelMaterial: value.panelMaterial === "solid" ? "solid" : "blur",
  presetId:
    value.presetId === "windows-mica" ||
    value.presetId === "pure-performance" ||
    value.presetId === "fluent-solid"
      ? value.presetId
      : "windows-mica",
  panelOpacity: clamp(value.panelOpacity, 0, 100),
  panelBlur: clamp(value.panelBlur, 0, 40),
  backgroundBlur: 0,
});

const getInitialAppearance = (): AppearanceSettings => {
  const saved = loadLegacyBrowserAppearance();
  let initial = saved ? { ...defaultAppearance, ...migrateAppearance(saved) } : { ...defaultAppearance };

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
  root.dataset.builtinBackground = settings.builtinBackground;
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
  root.style.setProperty("--hm-wallpaper-size", settings.backgroundSource === "builtin"
    ? builtinBackgroundSizes[settings.builtinBackground] ?? settings.backgroundScale
    : settings.backgroundScale);
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
        if (!active) return;

        if (saved) {
          const migrated = migrateAppearance(saved);
          const merged = { ...defaultAppearance, ...migrated };
          const normalized = normaliseAppearance(merged);
          setSettings(normalized);
          setHydrated(true);
        } else {
          // No saved settings - use defaults
          setHydrated(true);
        }
      })
      .catch((error) => {
        console.error("Unable to load appearance settings", error);
        // Do not enable automatic persistence after a failed load. Otherwise
        // the in-memory defaults overwrite a valid appearance document (and
        // delete its background) before the user can recover or retry.
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

  const fluentTheme = useMemo(() => createHypoMuxTheme(resolvedMode, accent), [resolvedMode, accent]);
  const pendingSave = useRef<AppearanceSettings>();
  const saveRevision = useRef(0);
  const flushAppearance = useRef(() => {});
  flushAppearance.current = () => {
    const next = pendingSave.current;
    if (!next) return;
    pendingSave.current = undefined;
    const revision = ++saveRevision.current;
    void appearancePersistence.save(next).then(() => {
      if (revision === saveRevision.current) setPersistenceError(undefined);
    }).catch(error => {
      if (revision === saveRevision.current) setPersistenceError(error instanceof Error ? error.message : String(error));
    });
  };
  useEffect(() => { applyDocumentTokens(settings, resolvedMode, accent); }, [settings, resolvedMode, accent]);
  useEffect(() => {
    if (!hydrated) return;
    pendingSave.current = settings;
    const timer = window.setTimeout(() => flushAppearance.current(), 300);
    return () => window.clearTimeout(timer);
  }, [settings, hydrated]);
  useEffect(() => {
    const flush = () => flushAppearance.current();
    const hide = () => { if (document.hidden) flush(); };
    window.addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", hide);
    return () => { window.removeEventListener("pagehide", flush); document.removeEventListener("visibilitychange", hide); flush(); };
  }, []);
  useEffect(() => {
    if (!hydrated) return;
    let active = true;
    void desktopPlatform.setWindowAppearance({ material: settings.material, mode: resolvedMode, accent })
      .then(result => { if (active) setNativeResult(result); })
      .catch(() => { if (active) setNativeResult({ applied: false, fallback: true }); });
    return () => { active = false; };
  }, [settings.material, resolvedMode, accent, hydrated]);

  const value = useMemo<AppearanceContextValue>(
    () => ({
      settings,
      resolvedMode,
      accent,
      fluentTheme,
      nativeResult,
      persistenceError,
      update: (patch) => {
        setHydrated(true);
        setSettings((current) => normaliseAppearance({ ...current, ...patch }));
      },
      applyPreset: (id) => {
        setHydrated(true);
        setSettings({ ...getAppearancePreset(id).settings, mode: settings.mode });
      },
      reset: () => {
        setHydrated(true);
        setSettings({ ...appearancePresets[0].settings });
      },
    }),
    [accent, fluentTheme, nativeResult, persistenceError, resolvedMode, settings],
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
