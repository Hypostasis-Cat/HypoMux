import type { AppearancePersistence, AppearanceSettings } from "./appearance.types";

const STORAGE_KEY = "hypomux.appearance.v1";

export const browserAppearancePersistence: AppearancePersistence = {
  load() {
    try {
      const value = localStorage.getItem(STORAGE_KEY);
      return value ? (JSON.parse(value) as AppearanceSettings) : null;
    } catch {
      return null;
    }
  },
  save(settings) {
    try {
      const safeSettings = settings.localBackgroundUrl?.startsWith("blob:")
        ? { ...settings, localBackgroundUrl: undefined, backgroundSource: "builtin" as const }
        : settings;
      localStorage.setItem(STORAGE_KEY, JSON.stringify(safeSettings));
    } catch {
      // Appearance persistence is best effort until the Go configuration adapter is connected.
    }
  },
};

export const backgroundService = {
  fromFile(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
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
