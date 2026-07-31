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
      colorNeutralForeground1: "#DFE8EE",
      colorNeutralForeground2: "#BBC8D1",
      colorNeutralForeground3: "#8C9DA9",
      colorNeutralForegroundInverted: "#EDF2F5",
      colorNeutralForegroundDisabled: "rgba(174, 190, 201, 0.46)",
      colorNeutralBackground1: "#1A2934",
      colorNeutralBackground1Hover: "#223541",
      colorNeutralBackground1Pressed: "#15242E",
      colorNeutralBackground1Selected: "#263A47",
      colorNeutralBackground2: "#20313C",
      colorNeutralBackground2Hover: "#293D49",
      colorNeutralBackground2Pressed: "#192A34",
      colorNeutralBackground2Selected: "#2C414D",
      colorNeutralBackground3: "#283A46",
      colorNeutralBackground4: "#30434F",
      colorNeutralBackground5: "#384B56",
      colorNeutralBackground6: "#40535E",
      colorNeutralBackgroundDisabled: "rgba(52, 69, 80, 0.56)",
      colorNeutralBackgroundStatic: "#1C2B36",
      colorNeutralStroke1: "rgba(206, 220, 229, 0.18)",
      colorNeutralStroke1Hover: "rgba(215, 227, 235, 0.27)",
      colorNeutralStroke1Pressed: "rgba(190, 207, 218, 0.22)",
      colorNeutralStroke2: "rgba(206, 220, 229, 0.11)",
      colorNeutralStrokeAccessible: "rgba(174, 194, 207, 0.58)",
      colorNeutralStrokeAccessibleHover: "rgba(195, 211, 221, 0.72)",
      colorNeutralStrokeAccessiblePressed: "rgba(158, 181, 196, 0.66)",
      colorNeutralStrokeDisabled: "rgba(153, 171, 182, 0.25)",
    };
  }

  return {
    ...theme,
    colorNeutralForeground1: "#18242C",
    colorNeutralForeground2: "#3E4E59",
    colorNeutralForeground3: "#667680",
    colorNeutralForegroundInverted: "#F4F7F8",
    colorNeutralBackground1: "#E5EBEE",
    colorNeutralBackground1Hover: "#DDE6EA",
    colorNeutralBackground1Pressed: "#D4DFE4",
    colorNeutralBackground1Selected: "#D9E4E8",
    colorNeutralBackground2: "#EDF1F2",
    colorNeutralBackground3: "#DCE4E8",
    colorNeutralBackgroundStatic: "#E5EBEE",
    colorNeutralStroke1: "rgba(45, 61, 71, 0.16)",
    colorNeutralStroke2: "rgba(45, 61, 71, 0.09)",
    colorNeutralStrokeAccessible: "rgba(65, 84, 96, 0.56)",
    colorNeutralStrokeAccessibleHover: "rgba(48, 67, 80, 0.72)",
    colorNeutralStrokeAccessiblePressed: "rgba(39, 57, 69, 0.66)",
  };
};
