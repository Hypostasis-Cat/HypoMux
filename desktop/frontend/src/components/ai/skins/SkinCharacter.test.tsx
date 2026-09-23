// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SkinCharacter } from "./SkinCharacter";
import type { Skin } from "./package";

const skin: Skin = { manifest: { schemaVersion: 1, id: "test.skin", name: "Test", author: "Test", version: "1", canvas: { width: 128, height: 128 }, anchor: { x: .5, y: 1 }, states: { idle: { type: "image", src: "idle.png" }, thinking: { type: "spritesheet", src: "thinking.png", columns: 2, frames: 4, fps: 2, loop: false } } }, files: { "idle.png": new Uint8Array(), "thinking.png": new Uint8Array() } };
afterEach(() => { cleanup(); delete document.documentElement.dataset.motion; vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals(); });
function setup() {
  vi.useFakeTimers();
  vi.stubGlobal("URL", { createObjectURL: vi.fn(() => "blob:test"), revokeObjectURL: vi.fn() });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
}
it("advances a row-major spritesheet, stops at the last frame, and resets on reduced animation", () => {
  setup();
  const view = render(<SkinCharacter skin={skin} state="thinking" animate />);
  const character = () => view.container.querySelector("span")!;
  expect(character().style.backgroundPosition).toBe("0% 0%");
  act(() => vi.advanceTimersByTime(500)); expect(character().style.backgroundPosition).toBe("100% 0%");
  act(() => vi.advanceTimersByTime(500)); expect(character().style.backgroundPosition).toBe("0% 100%");
  act(() => vi.advanceTimersByTime(3000)); expect(character().style.backgroundPosition).toBe("100% 100%");
  view.rerender(<SkinCharacter skin={skin} state="thinking" animate={false} />);
  expect(character().style.backgroundPosition).toBe("0% 0%");
  expect(vi.getTimerCount()).toBe(0);
});
it("falls back to idle for missing states and releases object URLs", () => {
  setup();
  const view = render(<SkinCharacter skin={skin} state="waiting" animate />);
  expect(view.container.querySelector("span")!.style.backgroundSize).toBe("100% 100%");
  expect(vi.getTimerCount()).toBe(0);
  view.unmount();
  expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:test");
});
it("does not animate when the operating system requests reduced motion", () => {
  setup();
  vi.stubGlobal("matchMedia", () => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  render(<SkinCharacter skin={skin} state="thinking" animate />);
  expect(vi.getTimerCount()).toBe(0);
});
it("gives static expressions motion without adding frame timers and respects the animation switch", () => {
  setup();
  const view = render(<SkinCharacter skin={skin} state="hover" animate />);
  expect(view.container.querySelector("span")!.dataset.skinMotion).toBe("hover");
  expect(vi.getTimerCount()).toBe(0);
  view.rerender(<SkinCharacter skin={skin} state="hover" animate={false} />);
  expect(view.container.querySelector("span")!.dataset.skinMotion).toBeUndefined();
  view.rerender(<SkinCharacter skin={skin} state="thinking" animate />);
  expect(view.container.querySelector("span")!.dataset.skinMotion).toBeUndefined();
});
it("stops static motion when hidden or reduced and resumes when allowed", async () => {
  setup();
  const view = render(<SkinCharacter skin={skin} state="idle" animate />);
  const character = () => view.container.querySelector("span")!;
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(character().dataset.skinMotion).toBeUndefined();
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(character().dataset.skinMotion).toBe("idle");
  await act(async () => { document.documentElement.dataset.motion = "reduced"; });
  expect(character().dataset.skinMotion).toBeUndefined();
  await act(async () => { delete document.documentElement.dataset.motion; });
  expect(character().dataset.skinMotion).toBe("idle");
});
