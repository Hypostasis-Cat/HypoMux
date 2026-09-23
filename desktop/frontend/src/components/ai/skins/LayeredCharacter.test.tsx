// @vitest-environment jsdom
import { layeredFixture } from "./layeredFixture";
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SkinCharacter } from "./SkinCharacter";
import { layerAnimation } from "./LayeredCharacter";
const skin = layeredFixture();
afterEach(() => { cleanup(); delete document.documentElement.dataset.motion; vi.restoreAllMocks(); vi.unstubAllGlobals(); });
function setup() {
  vi.stubGlobal("URL", { createObjectURL: vi.fn(() => "blob:layer"), revokeObjectURL: vi.fn() });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  const cancel = vi.fn();
  const animate = vi.fn(() => ({ cancel }));
  Object.defineProperty(Element.prototype, "animate", { configurable: true, value: animate });
  return { cancel, animate };
}
it("renders separate parts, animates, stops when disabled, and releases assets", () => {
  const { cancel, animate } = setup();
  const view = render(<SkinCharacter skin={skin} state="replying" animate />);
  expect(view.container.querySelectorAll("img")).toHaveLength(4);
  expect(animate).toHaveBeenCalled();
  view.rerender(<SkinCharacter skin={skin} state="replying" animate={false} />);
  expect(cancel).toHaveBeenCalled();
  view.unmount(); expect(URL.revokeObjectURL).toHaveBeenCalledTimes(4);
});
it("stops on reduced motion and hidden documents, and restarts when visible", async () => {
  const { cancel, animate } = setup();
  render(<SkinCharacter skin={skin} state="idle" animate />);
  await act(async () => { document.documentElement.dataset.motion = "reduced"; });
  expect(cancel).toHaveBeenCalled(); animate.mockClear();
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  await act(async () => { delete document.documentElement.dataset.motion; });
  expect(animate).not.toHaveBeenCalled();
  hidden.mockReturnValue(false); act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(animate).toHaveBeenCalled();
});
it("uses a static poster for library cards, and role defaults or custom tracks for states", () => {
  const { animate } = setup();
  const view = render(<SkinCharacter skin={skin} state="idle" animate={false} posterOnly />);
  expect(view.container.querySelectorAll("img")).toHaveLength(0);
  expect(animate).not.toHaveBeenCalled();
  const layers = skin.manifest.layered!.layers;
  expect(layerAnimation(layers[1], "idle")!.frames).toHaveLength(5);
  expect(layerAnimation(layers[2], "idle")).toBeUndefined();
  expect(layerAnimation(layers[2], "replying")!.duration).toBe(320);
  expect(layerAnimation(layers[3], "thinking")!.duration).toBe(1200);
});
