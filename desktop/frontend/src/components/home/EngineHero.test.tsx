// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EngineHero } from "./EngineHero";

vi.mock("../../i18n/i18n", () => ({
  useI18n: () => ({ locale: "en", t: (key: string) => key }),
}));

const props: ComponentProps<typeof EngineHero> = {
  phase: "stopped",
  mode: "proxy",
  selectedCount: 1,
  download: 0,
  upload: 0,
  connections: 0,
  history: [],
  transitioning: false,
  weighted: false,
  socksPort: 10800,
  httpPort: 10801,
  systemProxyTakeover: false,
  onModeChange: vi.fn(),
  onWeightedChange: vi.fn(),
  onToggle: vi.fn(),
};

const modeNotes = (container: HTMLElement) =>
  Array.from(container.querySelectorAll<HTMLElement>(".engine-mode-note > .engine-mode-note-text"));

afterEach(cleanup);

describe("EngineHero mode note", () => {
  it("keeps every mode's note in one slot so the card height does not change", () => {
    const { container, rerender } = render(<EngineHero {...props} />);
    let notes = modeNotes(container);
    expect(notes).toHaveLength(2);
    expect(notes[0].textContent).toContain("Local proxy ports only");
    expect(notes[0].getAttribute("aria-hidden")).toBeNull();
    expect(notes[1].textContent).toContain("read-only route, WFP");
    expect(notes[1].getAttribute("aria-hidden")).toBe("true");

    rerender(<EngineHero {...props} mode="tun" />);
    notes = modeNotes(container);
    expect(notes).toHaveLength(2);
    expect(notes[0].getAttribute("aria-hidden")).toBe("true");
    expect(notes[1].getAttribute("aria-hidden")).toBeNull();
    expect(notes[1].classList.contains("motion-inline-swap")).toBe(true);
  });
});
