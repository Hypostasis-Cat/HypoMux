// @vitest-environment jsdom
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Live2DCharacter from "./Live2DCharacter";
import type { Skin } from "./package";

const mocks = vi.hoisted(() => ({ render: vi.fn(), destroy: vi.fn(), motion: vi.fn(async () => true), focus: vi.fn(), modelDestroy: vi.fn() }));
vi.mock("pixi.js", () => ({ Application: class {
  view = document.createElement("canvas");
  renderer = { resize: vi.fn() };
  stage = { addChild: vi.fn() };
  render = mocks.render;
  destroy = mocks.destroy;
} }));
vi.mock("pixi-live2d-display/cubism4", () => ({
  Cubism4ModelSettings: class {}, MotionPreloadStrategy: { ALL: "ALL" },
  Live2DModel: { from: async () => ({ anchor: { set: vi.fn() }, scale: { set: vi.fn() }, position: { set: vi.fn() },
    motion: mocks.motion, update: vi.fn(), destroy: mocks.modelDestroy,
    internalModel: { width: 200, height: 200, on: vi.fn(), motionManager: { stopAllMotions: vi.fn() }, focusController: { focus: mocks.focus } },
  }) },
}));
const encode = (value: object) => new TextEncoder().encode(JSON.stringify(value));
const skin: Skin = {
  manifest: { schemaVersion: 2, id: "test.live2d", name: "Test", author: "Test", version: "1", canvas: { width: 200, height: 200 }, anchor: { x: .5, y: 1 }, states: { idle: { type: "image", src: "preview.png" } }, live2d: { model: "model.model3.json", motions: { hover: { group: "Hello", index: 0 } } } },
  files: { "model.model3.json": encode({ Version: 3, FileReferences: { Moc: "model.moc3", Textures: ["preview.png"], Motions: { Hello: [{ File: "hello.motion3.json" }] } } }), "model.moc3": new TextEncoder().encode("MOC3"), "preview.png": new Uint8Array(), "hello.motion3.json": encode({ Version: 3 }) },
};
afterEach(() => { cleanup(); delete document.documentElement.dataset.motion; vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.clearAllMocks(); });
function setup() {
  const frames = new Map<number, FrameRequestCallback>(); let next = 0;
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => { frames.set(++next, cb); return next; });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  let intersect: (visible: boolean) => void = () => {};
  vi.stubGlobal("IntersectionObserver", class {
    constructor(callback: IntersectionObserverCallback) { intersect = visible => callback([{ isIntersecting: visible } as IntersectionObserverEntry], this as unknown as IntersectionObserver); }
    observe() {} disconnect() {}
  });
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(200);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(200);
  return { frames, intersect: (visible: boolean) => act(() => intersect(visible)) };
}
async function ready(view: ReturnType<typeof render>) {
  await act(async () => { document.querySelector('script[src*="live2dcubismcore"]')!.dispatchEvent(new Event("load")); });
  await waitFor(() => expect(view.container.querySelector("[data-live2d-status='ready']")).not.toBeNull());
}
it("plays state motions, pauses on preferences/visibility, and releases the renderer on unmount", async () => {
  const { frames } = setup();
  const view = render(<Live2DCharacter skin={skin} state="idle" animate fallback={<span data-testid="static-poster" />} />);
  expect(view.container.querySelector('[data-testid="static-poster"]')).toBeNull();
  expect(view.container.querySelector('[aria-label="Live2D loading"]')).not.toBeNull();
  await ready(view);
  expect(frames.size).toBe(1);
  view.rerender(<Live2DCharacter skin={skin} state="hover" animate />);
  expect(mocks.motion).toHaveBeenCalledWith("Hello", 0, 3);
  view.rerender(<Live2DCharacter skin={skin} state="hover" animate={false} />);
  expect(frames.size).toBe(0);
  view.rerender(<Live2DCharacter skin={skin} state="idle" animate />);
  expect(frames.size).toBe(1);
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(frames.size).toBe(0);
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(frames.size).toBe(1);
  await act(async () => { document.documentElement.dataset.motion = "reduced"; });
  expect(frames.size).toBe(0);
  view.unmount();
  expect(mocks.destroy).toHaveBeenCalledWith(true, { children: true, texture: true, baseTexture: true });
});

it.each([false, true])("redraws the same model after returning from a hidden workspace (animate=%s)", async animate => {
  const { frames, intersect } = setup();
  const view = render(<Live2DCharacter skin={skin} state="idle" animate={animate} />);
  await ready(view);
  const canvas = view.container.querySelector("canvas");
  intersect(false);
  expect(frames.size).toBe(0);
  mocks.render.mockClear();
  intersect(true);
  expect(mocks.render).toHaveBeenCalled();
  expect(view.container.querySelector("canvas")).toBe(canvas);
  expect(mocks.destroy).not.toHaveBeenCalled();
  expect(frames.size).toBe(animate ? 1 : 0);

  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  mocks.render.mockClear();
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(mocks.render).toHaveBeenCalled();
});

it("rebuilds a lost WebGL context without selecting another skin", async () => {
  const { frames } = setup();
  const view = render(<Live2DCharacter skin={skin} state="idle" animate fallback={<span data-testid="static-poster" />} />);
  await ready(view);
  const canvas = view.container.querySelector("canvas")!;
  const lost = new Event("webglcontextlost", { cancelable: true });
  act(() => { canvas.dispatchEvent(lost); });
  expect(lost.defaultPrevented).toBe(true);
  expect(frames.size).toBe(0);
  expect(view.container.querySelector('[data-testid="static-poster"]')).not.toBeNull();
  await act(async () => { canvas.dispatchEvent(new Event("webglcontextrestored")); });
  await ready(view);
  expect(mocks.destroy).toHaveBeenCalledTimes(1);
  expect(view.container.querySelector('[data-testid="static-poster"]')).toBeNull();
  expect(frames.size).toBe(1);
});
