import { useEffect } from "react";

const REFLECTION_RADIUS = 210;
const FADE_CLEANUP_DELAY = 320;

const clearCardLight = (card: HTMLElement) => {
  card.classList.remove("is-glow-lit", "is-glow-source");
  card.style.removeProperty("--hm-card-light-x");
  card.style.removeProperty("--hm-card-light-y");
  card.style.removeProperty("--hm-card-light-strength");
};

/**
 * Adds a restrained pointer light to cards and lets nearby card edges catch a
 * small amount of reflected light. The work is frame-throttled and limited to
 * visible cards inside the reflection radius.
 */
export function useCardGlowField() {
  useEffect(() => {
    const finePointer = window.matchMedia("(hover: hover) and (pointer: fine)");
    if (!finePointer.matches) return;

    let frame = 0;
    let pointerX = 0;
    let pointerY = 0;
    let sourceCard: HTMLElement | null = null;
    let litCards = new Set<HTMLElement>();
    const fadeTimers = new Map<HTMLElement, number>();

    const cancelFade = (card: HTMLElement) => {
      const timer = fadeTimers.get(card);
      if (timer !== undefined) window.clearTimeout(timer);
      fadeTimers.delete(card);
    };

    const fadeCardLight = (card: HTMLElement) => {
      cancelFade(card);
      card.classList.remove("is-glow-source");
      // Keep the last pointer coordinates while opacity fades. Removing them
      // immediately makes the remaining glow jump back to the card centre.
      card.style.setProperty("--hm-card-light-strength", "0");
      fadeTimers.set(card, window.setTimeout(() => {
        clearCardLight(card);
        fadeTimers.delete(card);
      }, FADE_CLEANUP_DELAY));
    };

    const clear = () => {
      if (frame) cancelAnimationFrame(frame);
      frame = 0;
      litCards.forEach(fadeCardLight);
      litCards = new Set();
      sourceCard = null;
    };

    const render = () => {
      frame = 0;
      if (!sourceCard?.isConnected) {
        clear();
        return;
      }

      const nextLitCards = new Set<HTMLElement>();
      const cards = document.querySelectorAll<HTMLElement>(".hm-card");

      cards.forEach((card) => {
        // Nested interactive cards own the light; their containing surface
        // stays neutral so a hover never resembles a selected page section.
        if (card !== sourceCard && card.contains(sourceCard)) return;

        const rect = card.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0 || rect.bottom < 0 || rect.top > window.innerHeight) return;

        const nearestX = Math.max(rect.left, Math.min(pointerX, rect.right));
        const nearestY = Math.max(rect.top, Math.min(pointerY, rect.bottom));
        const distance = Math.hypot(pointerX - nearestX, pointerY - nearestY);
        const isSource = card === sourceCard;
        if (!isSource && distance > REFLECTION_RADIUS) return;

        const falloff = Math.max(0, 1 - distance / REFLECTION_RADIUS);
        const strength = isSource ? 1 : 0.46 * falloff * falloff;

        cancelFade(card);
        card.classList.add("is-glow-lit");
        card.classList.toggle("is-glow-source", isSource);
        card.style.setProperty("--hm-card-light-x", `${pointerX - rect.left}px`);
        card.style.setProperty("--hm-card-light-y", `${pointerY - rect.top}px`);
        card.style.setProperty("--hm-card-light-strength", strength.toFixed(3));
        nextLitCards.add(card);
      });

      litCards.forEach((card) => {
        if (!nextLitCards.has(card)) fadeCardLight(card);
      });
      litCards = nextLitCards;
    };

    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(render);
    };

    const onPointerMove = (event: PointerEvent) => {
      if (event.pointerType && event.pointerType !== "mouse" && event.pointerType !== "pen") return;
      pointerX = event.clientX;
      pointerY = event.clientY;
      sourceCard = (event.target as Element | null)?.closest<HTMLElement>(".hm-card") ?? null;
      if (sourceCard) schedule();
      else clear();
    };

    const onScrollOrResize = () => {
      if (!sourceCard) return;
      sourceCard = document.elementFromPoint(pointerX, pointerY)?.closest<HTMLElement>(".hm-card") ?? null;
      if (sourceCard) schedule();
      else clear();
    };

    document.addEventListener("pointermove", onPointerMove, { passive: true });
    document.addEventListener("pointerleave", clear);
    window.addEventListener("blur", clear);
    window.addEventListener("resize", onScrollOrResize, { passive: true });
    window.addEventListener("scroll", onScrollOrResize, { capture: true, passive: true });

    return () => {
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerleave", clear);
      window.removeEventListener("blur", clear);
      window.removeEventListener("resize", onScrollOrResize);
      window.removeEventListener("scroll", onScrollOrResize, true);
      if (frame) cancelAnimationFrame(frame);
      const cardsToReset = new Set<HTMLElement>([...litCards, ...fadeTimers.keys()]);
      fadeTimers.forEach((timer) => window.clearTimeout(timer));
      fadeTimers.clear();
      cardsToReset.forEach(clearCardLight);
    };
  }, []);
}
