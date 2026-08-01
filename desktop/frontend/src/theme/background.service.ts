import type { AppearancePersistence, AppearanceSettings } from "./appearance.types";
import { appServices } from "../platform/services";
import { isDesktopRuntime } from "../platform/runtime";

const STORAGE_KEY = "hypomux.appearance.v1";
let persistedLocalBackgroundURL: string | undefined;
let appearanceSaveQueue: Promise<void> = Promise.resolve();

const rememberPersistedBackground = (settings: AppearanceSettings) => {
  persistedLocalBackgroundURL = settings.backgroundSource === "local"
    ? settings.localBackgroundUrl
    : undefined;
};

const enqueueAppearanceSave = (operation: () => Promise<void>) => {
  const pending = appearanceSaveQueue.then(operation);
  appearanceSaveQueue = pending.catch(() => undefined);
  return pending;
};

const removeLegacyBrowserAppearance = () => {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Go persistence is authoritative even when WebView storage is unavailable.
  }
};

export const loadLegacyBrowserAppearance = (): AppearanceSettings | null => {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    return value ? (JSON.parse(value) as AppearanceSettings) : null;
  } catch {
    return null;
  }
};

const saveBrowserPreview = (settings: AppearanceSettings) => {
  try {
    const safeSettings = settings.localBackgroundUrl?.startsWith("data:") || settings.localBackgroundUrl?.startsWith("blob:")
      ? { ...settings, localBackgroundUrl: undefined, backgroundSource: "builtin" as const }
      : settings;
    localStorage.setItem(STORAGE_KEY, JSON.stringify(safeSettings));
  } catch {
    // Browser-only visual QA has no durable Go configuration service.
  }
};

export const appearancePersistence: AppearancePersistence = {
  async load() {
    const legacy = loadLegacyBrowserAppearance();
    if (!isDesktopRuntime()) return legacy;
    persistedLocalBackgroundURL = undefined;
    const payload = await appServices.appearance.load();
    if (payload) {
      removeLegacyBrowserAppearance();
      const loaded = JSON.parse(payload) as AppearanceSettings;
      // If backgroundSource is "local" but localBackgroundUrl is missing,
      // reset to system background to prevent visual corruption
      if (loaded.backgroundSource === "local" && !loaded.localBackgroundUrl) {
        loaded.backgroundSource = "system";
      }
      rememberPersistedBackground(loaded);
      return loaded;
    }
    if (legacy) {
      const migrated = await appServices.appearance.save(JSON.stringify(legacy));
      if (migrated) {
        removeLegacyBrowserAppearance();
        const loaded = JSON.parse(migrated) as AppearanceSettings;
        rememberPersistedBackground(loaded);
        return loaded;
      }
      return legacy;
    }
    return null;
  },
  async save(settings) {
    if (!isDesktopRuntime()) {
      saveBrowserPreview(settings);
      return;
    }
    return enqueueAppearanceSave(async () => {
      // A freshly selected image must be sent once regardless of its size.
      // Only omit it after this exact URL has been saved successfully or was
      // rehydrated from the backend during load.
      const settingsToSave = { ...settings };
      const localBackgroundURL = settings.backgroundSource === "local"
        ? settings.localBackgroundUrl
        : undefined;
      if (localBackgroundURL && localBackgroundURL === persistedLocalBackgroundURL) {
        delete settingsToSave.localBackgroundUrl;
      }

      await appServices.appearance.save(JSON.stringify(settingsToSave));
      persistedLocalBackgroundURL = localBackgroundURL;
    });
  },
};

export const backgroundService = {
  fromFile(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      if (file.size < 1 || file.size > 20 * 1024 * 1024) {
        reject(new Error("背景图片为空或超过 20 MiB"));
        return;
      }
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => reject(new Error("无法读取该图片"));
      reader.readAsDataURL(file);
    });
  },
  release(url?: string) {
    if (url?.startsWith("blob:")) {
      URL.revokeObjectURL(url);
    }
  },
};
