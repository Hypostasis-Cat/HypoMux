import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { animate } from "motion/react";
import { prefersReducedMotion, springs } from "../../theme/motion";
import { HypoMuxMark } from "./HypoMuxMark";

const LAUNCHING_ATTRIBUTE = "launching";
const EXHAUST_SPREAD = [-34, 0, 34];

type Controls = { stop: () => void };

/**
 * Launch sequence: the laptop is drawn, three exhaust strokes (the links)
 * converge, the rocket rises and ignites, then the mark flies onto the
 * titlebar icon while the overlay clears. Runs once the window is visible
 * (`ready`); a click or key press skips straight to the handoff.
 */
export function LaunchOverlay({ ready, onEnter }: { ready: boolean; onEnter: () => void }) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const markRef = useRef<SVGSVGElement>(null);
  const [done, setDone] = useState(false);
  const onEnterRef = useRef(onEnter);
  onEnterRef.current = onEnter;

  // Before the first paint: hide the titlebar icon until the mark lands on it,
  // and put the mark in its starting pose (laptop undrawn, exhaust spread
  // below, rocket inside the screen) so it never flashes fully drawn. Styles
  // are written directly because Motion applies duration-0 values a frame
  // late; Motion is told the same values so later animations start from them.
  useLayoutEffect(() => {
    document.documentElement.dataset[LAUNCHING_ATTRIBUTE] = "true";
    const mark = markRef.current;
    if (mark && !prefersReducedMotion()) {
      const pose = (element: SVGElement, x: number, y: number) => {
        element.style.transform = `translate(${x}px, ${y}px)`;
        element.style.opacity = "0";
        animate(element, { x, y, opacity: 0 }, { duration: 0 });
      };
      mark.querySelectorAll<SVGPathElement>('[data-part="laptop"] path').forEach(path => {
        if (typeof path.getTotalLength !== "function") return;
        const length = path.getTotalLength();
        path.style.strokeDasharray = `${length} ${length}`;
        path.style.strokeDashoffset = `${length}`;
      });
      mark.querySelectorAll<SVGPathElement>('[data-part="exhaust"] path').forEach((path, index) => pose(path, EXHAUST_SPREAD[index], 64));
      const rocket = mark.querySelector<SVGGElement>('[data-part="rocket"]');
      if (rocket) pose(rocket, 0, 46);
    }
    return () => { delete document.documentElement.dataset[LAUNCHING_ATTRIBUTE]; };
  }, []);

  useEffect(() => {
    if (!ready || done) return;
    const overlay = overlayRef.current, mark = markRef.current;
    if (!overlay || !mark) return;
    const parts = (name: string) => [...mark.querySelectorAll<SVGPathElement>(`[data-part="${name}"] path`)];
    const laptop = parts("laptop"), exhaust = parts("exhaust");
    const rocket = mark.querySelector<SVGGElement>('[data-part="rocket"]');
    const running: Controls[] = [];
    let cancelled = false, handedOff = false;
    const track = (controls: Controls) => { running.push(controls); return controls; };
    const stopAll = () => running.splice(0).forEach(controls => controls.stop());
    const lengths = laptop.map(path => (typeof path.getTotalLength === "function" ? path.getTotalLength() : 0));
    const timers: number[] = [];
    const after = (ms: number, run: () => void) => timers.push(window.setTimeout(run, ms));

    const finish = () => {
      if (cancelled) return;
      delete document.documentElement.dataset[LAUNCHING_ATTRIBUTE];
      setDone(true);
    };

    const settle = () => {
      laptop.forEach(path => animate(path, { strokeDashoffset: 0 }, { duration: 0 }));
      if (rocket) animate(rocket, { y: 0, opacity: 1 }, { duration: 0 });
      exhaust.forEach(path => animate(path, { x: 0, y: 0, opacity: 1 }, { duration: 0 }));
    };

    const handoff = () => {
      if (handedOff || cancelled) return;
      handedOff = true;
      const target = document.querySelector(".product-mark")?.getBoundingClientRect();
      const from = mark.getBoundingClientRect();
      if (target && from.width > 0) {
        track(animate(mark, { x: target.left - from.left, y: target.top - from.top, scale: target.width / from.width }, { ...springs.smooth, bounce: 0 }));
      }
      track(animate(overlay, { opacity: 0 }, { duration: 0.32, delay: 0.06, ease: [0.33, 0, 0.67, 1] }));
      onEnterRef.current();
      after(420, finish);
    };

    const skip = () => {
      if (handedOff) return;
      stopAll();
      timers.splice(0).forEach(clearTimeout);
      settle();
      handoff();
    };

    // A silent start to the tray never shows this window; never leave a half-played launch for later.
    if (document.visibilityState === "hidden") {
      finish();
      return () => { cancelled = true; };
    }
    if (prefersReducedMotion() || lengths.some(length => length === 0)) {
      settle();
      onEnterRef.current();
      track(animate(overlay, { opacity: 0 }, { duration: 0.15 }));
      after(160, finish);
      return () => { cancelled = true; stopAll(); timers.forEach(clearTimeout); };
    }

    // 1 · draw the laptop  2 · the links converge  3 · the rocket rises
    laptop.forEach((path, index) => track(animate(path, { strokeDashoffset: [lengths[index], 0] }, { duration: 0.42, delay: index * 0.06, ease: [0.33, 0, 0.1, 1] })));
    exhaust.forEach((path, index) => track(animate(path, { x: 0, y: 0, opacity: 1 }, { ...springs.smooth, delay: 0.1 + index * 0.05 })));
    if (rocket) track(animate(rocket, { y: 0, opacity: 1 }, { ...springs.gentle, delay: 0.25 }));
    // 4 · ignition  5 · hand off to the titlebar
    after(650, () => track(animate(exhaust, { opacity: [1, 0.35, 1], y: [0, 3, 0] }, { duration: 0.22 })));
    after(750, handoff);

    window.addEventListener("pointerdown", skip, { once: true });
    window.addEventListener("keydown", skip, { once: true });
    return () => {
      cancelled = true;
      stopAll();
      timers.forEach(clearTimeout);
      window.removeEventListener("pointerdown", skip);
      window.removeEventListener("keydown", skip);
    };
  }, [ready, done]);

  if (done) return null;
  return (
    <div ref={overlayRef} className="launch-overlay" aria-hidden="true">
      <HypoMuxMark ref={markRef} className="launch-mark" />
    </div>
  );
}
