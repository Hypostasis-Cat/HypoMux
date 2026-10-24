import type { ReactNode } from "react";
import { LazyMotion, MotionConfig, domAnimation } from "motion/react";
import { useAppearance } from "./appearance.store";
import { springs } from "./motion";

/**
 * Loads Motion's DOM animation features once and applies the user's motion
 * preference: "standard" follows the OS setting, "reduced" and "off" keep
 * only opacity changes. Components use `m.*` elements under this provider.
 */
export function MotionProvider({ children }: { children: ReactNode }) {
  const { settings } = useAppearance();
  return (
    <LazyMotion features={domAnimation} strict>
      <MotionConfig reducedMotion={settings.motion === "standard" ? "user" : "always"} transition={springs.smooth}>
        {children}
      </MotionConfig>
    </LazyMotion>
  );
}
