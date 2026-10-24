import { forwardRef, useId } from "react";

/**
 * Stroke geometry of the HypoMux icon (public/support/icon.ico), traced on its
 * 256px grid so the launch animation can draw it. Grouped by the parts the
 * launch sequence moves: the laptop is drawn, the exhaust converges, the
 * rocket rises.
 */
export const MARK_PATHS = {
  laptop: [
    "M84 83 H38 a8 8 0 0 0 -8 8 V193 H226 V91 a8 8 0 0 0 -8 -8 H172",
    "M84 100 H46 V180 H204 V100 H172",
    "M27 203 H229 a7 7 0 0 1 7 7 V226 a7 7 0 0 1 -7 7 H27 a7 7 0 0 1 -7 -7 V210 a7 7 0 0 1 7 -7 Z M89 203 V218 H157 V203",
  ],
  rocket: "M128 21 C112 35 106 53 106 71 V143 H150 V71 C150 53 144 35 128 21 Z M106 71 C94 66 85 75 85 91 V132 H106 M150 71 C162 66 171 75 171 91 V132 H150 M128 92 V112",
  exhaust: ["M106 158 V170", "M128 158 V170", "M150 158 V170"],
} as const;

/** The icon's gradient, bottom-left cyan through violet to top-right pink. */
const GRADIENT = ["#19bfe9", "#a480ec", "#e55b8d"] as const;

export const HypoMuxMark = forwardRef<SVGSVGElement, { className?: string; strokeWidth?: number }>(
  function HypoMuxMark({ className, strokeWidth = 14 }, ref) {
    const gradientId = `${useId().replace(/:/g, "")}-mark`;
    const stroke = { fill: "none", stroke: `url(#${gradientId})`, strokeWidth, strokeLinecap: "round", strokeLinejoin: "round" } as const;
    return (
      <svg ref={ref} className={className} viewBox="0 0 256 256" aria-hidden="true" focusable="false">
        <defs>
          <linearGradient id={gradientId} x1="0" y1="256" x2="256" y2="0" gradientUnits="userSpaceOnUse">
            {GRADIENT.map((color, index) => <stop key={color} offset={index / (GRADIENT.length - 1)} stopColor={color} />)}
          </linearGradient>
        </defs>
        <g data-part="laptop">{MARK_PATHS.laptop.map(d => <path key={d} d={d} {...stroke} />)}</g>
        <g data-part="rocket"><path d={MARK_PATHS.rocket} {...stroke} /></g>
        <g data-part="exhaust">{MARK_PATHS.exhaust.map(d => <path key={d} d={d} {...stroke} />)}</g>
      </svg>
    );
  },
);
