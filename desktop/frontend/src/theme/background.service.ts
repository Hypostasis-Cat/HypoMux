import type { AppearancePersistence, AppearanceSettings } from "./appearance.types";
import { appServices } from "../platform/services";
import { isDesktopRuntime } from "../platform/runtime";

const STORAGE_KEY = "hypomux.appearance.v1";

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
    const payload = await appServices.appearance.load();
    if (payload) {
      removeLegacyBrowserAppearance();
      const loaded = JSON.parse(payload) as AppearanceSettings;
      // If backgroundSource is "local" but localBackgroundUrl is missing,
      // reset to system background to prevent visual corruption
      if (loaded.backgroundSource === "local" && !loaded.localBackgroundUrl) {
        loaded.backgroundSource = "system";
      }
      return loaded;
    }
    if (legacy) {
      const migrated = await appServices.appearance.save(JSON.stringify(legacy));
      if (migrated) {
        removeLegacyBrowserAppearance();
        return JSON.parse(migrated) as AppearanceSettings;
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
    // The Go appearance service stores the image outside appearance.json and
    // rehydrates it on load. When backgroundSource is "local" and localBackgroundUrl
    // was loaded from the backend (not freshly uploaded), we can omit the large
    // data URL - the backend will reuse the existing background file.
    const settingsToSave = { ...settings };
    if (
      settings.backgroundSource === "local" &&
      settings.localBackgroundUrl &&
      settings.localBackgroundUrl.startsWith("data:") &&
      settings.localBackgroundUrl.length > 100000 // > 100KB, likely from backend
    ) {
      // Omit the large data URL - backend will reuse existing file
      delete settingsToSave.localBackgroundUrl;
    }
    await appServices.appearance.save(JSON.stringify(settingsToSave));
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
