import type { AccentPreset, AppearancePreset, AppearanceSettings } from "./appearance.types";

export const accentColours: Record<Exclude<AccentPreset, "custom">, string> = {
  system: "#0F6CBD",
  hypomux: "#1677D2",
  cyan: "#087E8B",
  purple: "#7455C3",
  orange: "#B95C18",
  green: "#2E7D59",
};

const baseSettings: AppearanceSettings = {
  schemaVersion: 2,
  presetId: "fluent-solid",
  mode: "system",
  material: "solid",
  panelMaterial: "blur",
  accentPreset: "hypomux",
  customAccent: "#1677D2",
  backgroundSource: "system",
  builtinBackground: "aurora",
  solidBackground: "#DCE4EA",
  gradientBackground: "linear-gradient(135deg, #CBD8E2 0%, #E9E3DA 52%, #C7D9E4 100%)",
  backgroundBrightness: 100,
  backgroundSaturation: 94,
  backgroundContrast: 100,
  backgroundOverlay: 26,
  backgroundBlur: 0,
  backgroundScale: "cover",
  backgroundAlignment: "center",
  panelOpacity: 50,
  panelBlur: 20,
  panelSaturation: 118,
  borderBrightness: 52,
  shadowStrength: 24,
  density: "standard",
  radius: 10,
  motion: "standard",
};

const make = (values: Partial<AppearanceSettings>): AppearanceSettings => ({ ...baseSettings, ...values });

export const appearancePresets: AppearancePreset[] = [
  {
    id: "fluent-solid",
    name: "Fluent Solid",
    description: "Stable theme background for the default light and dark appearances.",
    settings: make({ presetId: "fluent-solid", material: "solid", backgroundSource: "system" }),
  },
  {
    id: "windows-mica",
    name: "Windows Mica",
    description: "安静、低透明度，优先使用 Windows 11 原生材质。",
    settings: make({ presetId: "windows-mica", material: "mica", backgroundSource: "system" }),
  },
  {
    id: "pure-performance",
    name: "Pure Performance",
    description: "无模糊、低动效，适合远程桌面和低性能设备。",
    settings: make({
      presetId: "pure-performance",
      material: "solid",
      panelMaterial: "solid",
      backgroundSource: "solid",
      backgroundOverlay: 0,
      panelOpacity: 94,
      panelBlur: 0,
      panelSaturation: 100,
      shadowStrength: 10,
      motion: "reduced",
    }),
  },
];

export const defaultAppearance = appearancePresets[0].settings;

export const getAppearancePreset = (id: AppearanceSettings["presetId"]) =>
  appearancePresets.find((preset) => preset.id === id) ?? appearancePresets[0];

export const resolveAccent = (settings: AppearanceSettings) =>
  settings.accentPreset === "custom" ? settings.customAccent : accentColours[settings.accentPreset];
