import { createDarkTheme, createLightTheme, type BrandVariants, type Theme } from "@fluentui/react-components";
import type { ResolvedAppearance } from "./appearance.types";

const clamp = (value: number, min = 0, max = 1) => Math.min(max, Math.max(min, value));

const hexToRgb = (hex: string) => {
  const cleaned = hex.replace("#", "");
  const value = Number.parseInt(cleaned.length === 3 ? cleaned.split("").map((part) => part + part).join("") : cleaned, 16);
  return { r: (value >> 16) & 255, g: (value >> 8) & 255, b: value & 255 };
};

const rgbToHex = (r: number, g: number, b: number) =>
  `#${[r, g, b].map((value) => Math.round(clamp(value / 255) * 255).toString(16).padStart(2, "0")).join("")}`;

const mix = (hex: string, target: string, amount: number) => {
  const source = hexToRgb(hex);
  const destination = hexToRgb(target);
  return rgbToHex(
    source.r + (destination.r - source.r) * amount,
    source.g + (destination.g - source.g) * amount,
    source.b + (destination.b - source.b) * amount,
  );
};

export const createBrandRamp = (accent: string): BrandVariants => ({
  10: mix(accent, "#000000", 0.88),
  20: mix(accent, "#000000", 0.76),
  30: mix(accent, "#000000", 0.64),
  40: mix(accent, "#000000", 0.52),
  50: mix(accent, "#000000", 0.4),
  60: mix(accent, "#000000", 0.28),
  70: mix(accent, "#000000", 0.16),
  80: mix(accent, "#000000", 0.06),
  90: accent,
  100: mix(accent, "#FFFFFF", 0.12),
  110: mix(accent, "#FFFFFF", 0.24),
  120: mix(accent, "#FFFFFF", 0.36),
  130: mix(accent, "#FFFFFF", 0.48),
  140: mix(accent, "#FFFFFF", 0.6),
  150: mix(accent, "#FFFFFF", 0.72),
  160: mix(accent, "#FFFFFF", 0.84),
});

export const createHypoMuxTheme = (mode: ResolvedAppearance, accent: string): Theme => {
  const theme = mode === "dark" ? createDarkTheme(createBrandRamp(accent)) : createLightTheme(createBrandRamp(accent));

  if (mode === "dark") {
    return {
      ...theme,
      colorBrandForeground1: mix(accent, "#FFFFFF", 0.42),
      colorBrandForeground2: mix(accent, "#FFFFFF", 0.42),
      colorNeutralForeground1: "#EEF2F8",
      colorNeutralForeground2: "#C6CFDD",
      colorNeutralForeground3: "#A3B0C2",
      colorNeutralForegroundInverted: "#242424",
      colorNeutralForegroundDisabled: "rgba(255, 255, 255, 0.36)",
      colorNeutralBackground1: "#242A35",
      colorNeutralBackground1Hover: "#333D4C",
      colorNeutralBackground1Pressed: "#1C2029",
      colorNeutralBackground1Selected: "#303A4A",
      colorNeutralBackground2: "#1C2029",
      colorNeutralBackground2Hover: "#2D3543",
      colorNeutralBackground2Pressed: "#171A21",
      colorNeutralBackground2Selected: "#2D3543",
      colorNeutralBackground3: "#171A21",
      colorNeutralBackground4: "#0A0A0A",
      colorNeutralBackground5: "#000000",
      colorNeutralBackground6: "#000000",
      colorNeutralBackgroundDisabled: "rgba(255, 255, 255, 0.08)",
      colorNeutralBackgroundStatic: "#333D4C",
      colorNeutralStroke1: "rgba(255, 255, 255, 0.13)",
      colorNeutralStroke1Hover: "rgba(255, 255, 255, 0.21)",
      colorNeutralStroke1Pressed: "rgba(255, 255, 255, 0.17)",
      colorNeutralStroke2: "rgba(255, 255, 255, 0.083)",
      colorNeutralStrokeAccessible: "rgba(255, 255, 255, 0.53)",
      colorNeutralStrokeAccessibleHover: "rgba(255, 255, 255, 0.67)",
      colorNeutralStrokeAccessiblePressed: "rgba(255, 255, 255, 0.60)",
      colorNeutralStrokeDisabled: "rgba(255, 255, 255, 0.16)",
    };
  }

  return {
    ...theme,
    colorNeutralForeground1: "#202632",
    colorNeutralForeground2: "#49566A",
    colorNeutralForeground3: "#606C7D",
    colorNeutralForegroundInverted: "#FFFFFF",
    colorNeutralBackground1: "#FFFFFF",
    colorNeutralBackground1Hover: "#F5F7FA",
    colorNeutralBackground1Pressed: "#E0E0E0",
    colorNeutralBackground1Selected: "#EBEBEB",
    colorNeutralBackground2: "#F1F4F8",
    colorNeutralBackground3: "#F5F7FA",
    colorNeutralBackgroundStatic: "#FFFFFF",
    colorNeutralStroke1: "rgba(0, 0, 0, 0.14)",
    colorNeutralStroke2: "rgba(0, 0, 0, 0.083)",
    colorNeutralStrokeAccessible: "rgba(0, 0, 0, 0.56)",
    colorNeutralStrokeAccessibleHover: "rgba(0, 0, 0, 0.72)",
    colorNeutralStrokeAccessiblePressed: "rgba(0, 0, 0, 0.64)",
  };
};
