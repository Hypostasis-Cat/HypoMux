import type { AppearanceSettings } from "./appearance.types";

export const builtinBackgrounds = {
  "soft-dots": "var(--hm-soft-dots)",
  "soft-bloom": "var(--hm-soft-bloom)",
};

export const backgroundPresets = [
  { id: "soft-bloom", name: "紫雾薄荷", english: "Lilac & mint", description: "紫雾融入薄荷 · 轻盈柔彩", englishDescription: "Soft lilac · fresh mint" },
  { id: "soft-dots", name: "柔光点阵", english: "Soft dots", description: "淡紫柔光 · 细密点阵", englishDescription: "Lavender glow · fine dots" },
] as const;

export const builtinBackgroundSizes: Record<AppearanceSettings["builtinBackground"], string> = {
  "soft-dots": "auto, 22px 22px, auto",
  "soft-bloom": "cover",
};

export const resolveWallpaperBackground = (settings: AppearanceSettings) => {
  let background =
    settings.backgroundSource === "system" && settings.material === "solid"
      ? "var(--hm-window-base)"
      : "var(--hm-system-background)";

  if (settings.backgroundSource === "builtin") {
    background = builtinBackgrounds[settings.builtinBackground];
  } else if (settings.backgroundSource === "local" && settings.localBackgroundUrl) {
    background = `url("${settings.localBackgroundUrl}")`;
  } else if (settings.backgroundSource === "solid") {
    background = "var(--hm-solid-background)";
  } else if (settings.backgroundSource === "gradient") {
    background = "var(--hm-gradient-background)";
  }

  return background;
};
