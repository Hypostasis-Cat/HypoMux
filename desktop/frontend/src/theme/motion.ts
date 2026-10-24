import type { Transition } from "motion/react";

/**
 * HypoMux motion language. These springs mirror motion.tokens.css, which
 * carries the same physics to CSS as linear() samples; change both together.
 *
 * - snappy: press, toggles, hover and state swaps
 * - smooth: indicators that travel, expand/collapse, list rows
 * - gentle: pages, large surfaces and numbers
 */
export const springs = {
  snappy: { type: "spring", visualDuration: 0.18, bounce: 0 },
  smooth: { type: "spring", visualDuration: 0.34, bounce: 0.12 },
  gentle: { type: "spring", visualDuration: 0.48, bounce: 0.06 },
  /** A pressed control springing back; CSS uses --hm-spring-release. */
  release: { type: "spring", visualDuration: 0.34, bounce: 0.35 },
} as const satisfies Record<string, Transition>;

/** True when the app's motion preference or the OS asks for reduced motion. */
export function prefersReducedMotion() {
  const mode = document.documentElement.dataset.motion;
  if (mode === "reduced" || mode === "off") return true;
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
