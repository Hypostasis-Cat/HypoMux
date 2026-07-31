export type AppearanceMode = "system" | "light" | "dark";
export type ResolvedAppearance = "light" | "dark";
export type WindowMaterial = "mica" | "solid";
export type PanelMaterial = "blur" | "solid";
export type AccentPreset = "system" | "hypomux" | "cyan" | "purple" | "orange" | "green" | "custom";
export type BackgroundSource = "system" | "builtin" | "local" | "solid" | "gradient";
export type BackgroundScale = "cover" | "contain" | "fill";
export type BackgroundAlignment = "center" | "top" | "bottom" | "left" | "right";
export type InterfaceDensity = "compact" | "standard" | "comfortable";
export type MotionMode = "off" | "reduced" | "standard";

export type AppearanceSettings = {
  presetId: AppearancePresetId;
  mode: AppearanceMode;
  material: WindowMaterial;
  panelMaterial: PanelMaterial;
  accentPreset: AccentPreset;
  customAccent: string;
  backgroundSource: BackgroundSource;
  builtinBackground: "aurora" | "harbour";
  localBackgroundUrl?: string;
  solidBackground: string;
  gradientBackground: string;
  backgroundBrightness: number;
  backgroundSaturation: number;
  backgroundContrast: number;
  backgroundOverlay: number;
  backgroundBlur: number;
  backgroundScale: BackgroundScale;
  backgroundAlignment: BackgroundAlignment;
  panelOpacity: number;
  panelBlur: number;
  panelSaturation: number;
  borderBrightness: number;
  shadowStrength: number;
  density: InterfaceDensity;
  radius: number;
  motion: MotionMode;
};

export type AppearancePresetId = "windows-mica" | "pure-performance";

export type AppearancePreset = {
  id: AppearancePresetId;
  name: string;
  description: string;
  settings: AppearanceSettings;
};

export interface AppearancePersistence {
  load(): Promise<AppearanceSettings | null>;
  save(settings: AppearanceSettings): Promise<void>;
}

export type NativeAppearanceResult = {
  applied: boolean;
  fallback: boolean;
  reason?: string;
};
