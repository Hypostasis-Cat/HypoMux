import type { AppearanceSettings } from "./appearance.types";

export const builtinBackgrounds = {
  aurora:
    "radial-gradient(circle at 78% 12%, rgba(74, 153, 198, .52), transparent 31%), radial-gradient(circle at 12% 86%, rgba(188, 136, 103, .34), transparent 34%), linear-gradient(135deg, #9cafb9, #d8d0c7 52%, #769aae)",
  harbour:
    "radial-gradient(circle at 18% 20%, rgba(53, 125, 157, .46), transparent 31%), radial-gradient(circle at 82% 72%, rgba(105, 84, 150, .32), transparent 34%), linear-gradient(145deg, #526c79, #a6aeb0 48%, #445c68)",
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
