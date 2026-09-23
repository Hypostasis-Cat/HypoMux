// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { attachWindowTouchDrag, type WindowDragHost } from "./windowTouchDrag";

let element: HTMLElement;
let host: WindowDragHost;
let dispose: () => void;
let captured: Set<number>;
const pointer = (type: string, options: Record<string, unknown> = {}, target: Element = element) => {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.entries({ pointerType: "touch", pointerId: 1, isPrimary: true, button: 0, screenX: 300, screenY: 120, clientX: 200, clientY: 20, ...options })
    .forEach(([key, value]) => Object.defineProperty(event, key, { value }));
  target.dispatchEvent(event);
  return event;
};
const frame = () => vi.advanceTimersByTimeAsync(20);

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => window.setTimeout(() => callback(performance.now()), 16));
  vi.stubGlobal("cancelAnimationFrame", (id: number) => clearTimeout(id));
  element = document.createElement("header");
  element.innerHTML = '<div class="titlebar-identity"><strong>HypoMux</strong></div><div class="titlebar-drag"></div><div class="titlebar-actions"><button><span>Close</span></button></div>';
  document.body.append(element);
  captured = new Set();
  element.setPointerCapture = vi.fn(id => { captured.add(id); });
  element.hasPointerCapture = vi.fn(id => captured.has(id));
  element.releasePointerCapture = vi.fn(id => { captured.delete(id); pointer("lostpointercapture", { pointerId: id }); });
  host = {
    supportsWindowDrag: () => true,
    windowDragState: vi.fn().mockResolvedValue({ x: 100, y: 100, maximised: false, fullscreen: false }),
    restoreWindowForDrag: vi.fn().mockResolvedValue({ width: 800, height: 600 }),
    moveWindow: vi.fn().mockResolvedValue(undefined),
  };
  dispose = attachWindowTouchDrag(element, host);
});
afterEach(() => { dispose(); element.remove(); vi.useRealTimers(); vi.unstubAllGlobals(); });

it("drags from a title child with touch and keeps the final position on release", async () => {
  expect(pointer("pointerdown", {}, element.querySelector("strong")!).defaultPrevented).toBe(true);
  expect(element.hasAttribute("data-touch-window-drag")).toBe(true);
  pointer("pointermove", { screenX: 340, screenY: 150 });
  await frame();
  expect(host.moveWindow).toHaveBeenLastCalledWith(140, 130);
  pointer("pointerup", { screenX: 350, screenY: 160 });
  await frame();
  expect(host.moveWindow).toHaveBeenLastCalledWith(150, 140);
  expect(captured.size).toBe(0);
  expect(element.hasAttribute("data-touch-window-drag")).toBe(false);
});

it("does not intercept mouse, secondary touch, pen barrel buttons, or titlebar controls", async () => {
  expect(pointer("pointerdown", { pointerType: "mouse" }).defaultPrevented).toBe(false);
  expect(pointer("pointerdown", { isPrimary: false, pointerId: 2 }).defaultPrevented).toBe(false);
  expect(pointer("pointerdown", { pointerType: "pen", button: 2 }).defaultPrevented).toBe(false);
  expect(pointer("pointerdown", {}, element.querySelector("button span")!).defaultPrevented).toBe(false);
  pointer("pointermove", { screenX: 400 });
  await frame();
  expect(host.windowDragState).not.toHaveBeenCalled();
  expect(host.moveWindow).not.toHaveBeenCalled();
});

it("ignores a tap and small movement but supports pen movement", async () => {
  pointer("pointerdown");
  pointer("pointerup", { screenX: 302, screenY: 122 });
  await frame();
  expect(host.moveWindow).not.toHaveBeenCalled();
  pointer("pointerdown", { pointerType: "pen" });
  pointer("pointermove", { pointerType: "pen", screenX: 360 });
  await frame();
  expect(host.moveWindow).toHaveBeenLastCalledWith(160, 100);
});

it.each(["pointercancel", "lostpointercapture"])("cancels pending movement on %s", async type => {
  pointer("pointerdown");
  pointer("pointermove", { screenX: 400 });
  pointer(type);
  await frame();
  expect(host.moveWindow).not.toHaveBeenCalled();
  expect(captured.size).toBe(0);
  expect(element.hasAttribute("data-touch-window-drag")).toBe(false);
});

it("does not issue stale movement after a delayed query and cancellation", async () => {
  let resolve!: (state: { x: number; y: number; maximised: boolean; fullscreen: boolean }) => void;
  vi.mocked(host.windowDragState).mockReturnValue(new Promise(done => { resolve = done; }));
  pointer("pointerdown");
  pointer("pointermove", { screenX: 400 });
  window.dispatchEvent(new Event("blur"));
  resolve({ x: 100, y: 100, maximised: false, fullscreen: false });
  await frame();
  expect(host.moveWindow).not.toHaveBeenCalled();
});

it("keeps the final movement when release precedes the position query", async () => {
  let resolve!: (state: { x: number; y: number; maximised: boolean; fullscreen: boolean }) => void;
  vi.mocked(host.windowDragState).mockReturnValue(new Promise(done => { resolve = done; }));
  pointer("pointerdown");
  pointer("pointerup", { screenX: 360, screenY: 150 });
  resolve({ x: 100, y: 100, maximised: false, fullscreen: false });
  await frame();
  expect(host.moveWindow).toHaveBeenLastCalledWith(160, 130);
});

it("serialises position writes and coalesces intermediate pointer moves", async () => {
  let finish!: () => void;
  vi.mocked(host.moveWindow).mockImplementationOnce(() => new Promise(done => { finish = done; }));
  pointer("pointerdown");
  pointer("pointermove", { screenX: 340 });
  await frame();
  pointer("pointermove", { screenX: 370 });
  pointer("pointerup", { screenX: 400 });
  await frame();
  expect(host.moveWindow).toHaveBeenCalledTimes(1);
  finish();
  await frame();
  expect(host.moveWindow).toHaveBeenCalledTimes(2);
  expect(host.moveWindow).toHaveBeenLastCalledWith(200, 100);
});

it("restores a maximised window only after a drag and preserves its grip", async () => {
  vi.mocked(host.windowDragState).mockResolvedValue({ x: 0, y: 0, maximised: true, fullscreen: false });
  pointer("pointerdown", { clientX: window.innerWidth / 2 });
  await frame();
  expect(host.restoreWindowForDrag).not.toHaveBeenCalled();
  pointer("pointermove", { screenX: 400, screenY: 160 });
  await frame();
  expect(host.restoreWindowForDrag).toHaveBeenCalledTimes(1);
  expect(host.moveWindow).toHaveBeenLastCalledWith(0, 140);
});

it("does not move fullscreen windows or call native APIs in a browser preview", async () => {
  vi.mocked(host.windowDragState).mockResolvedValue({ x: 0, y: 0, maximised: false, fullscreen: true });
  pointer("pointerdown"); pointer("pointermove", { screenX: 400 });
  await frame();
  expect(host.moveWindow).not.toHaveBeenCalled();
  host.supportsWindowDrag = () => false;
  expect(pointer("pointerdown").defaultPrevented).toBe(false);
  expect(host.windowDragState).toHaveBeenCalledTimes(1);
});

it("releases capture on unmount and discards queued moves", async () => {
  pointer("pointerdown"); pointer("pointermove", { screenX: 400 });
  dispose();
  await frame();
  expect(captured.size).toBe(0);
  expect(host.moveWindow).not.toHaveBeenCalled();
});
