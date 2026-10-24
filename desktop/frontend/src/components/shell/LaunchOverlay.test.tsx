// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { LaunchOverlay } from "./LaunchOverlay";

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); delete document.documentElement.dataset.motion; });

it("covers the window and hides the titlebar icon until the window is ready", () => {
  const { container } = render(<LaunchOverlay ready={false} onEnter={() => {}} />);
  expect(container.querySelector(".launch-overlay")).not.toBeNull();
  expect(document.documentElement.dataset.launching).toBe("true");
  act(() => { vi.advanceTimersByTime(5000); });
  expect(container.querySelector(".launch-overlay")).not.toBeNull();
});

it("enters the app and always removes itself once ready, even when it cannot draw the mark", () => {
  // jsdom has no SVG geometry, which exercises the same quick path as reduced motion.
  const onEnter = vi.fn();
  const { container, rerender } = render(<LaunchOverlay ready={false} onEnter={onEnter} />);
  rerender(<LaunchOverlay ready onEnter={onEnter} />);
  expect(onEnter).toHaveBeenCalledTimes(1);
  act(() => { vi.advanceTimersByTime(200); });
  expect(container.querySelector(".launch-overlay")).toBeNull();
  expect(document.documentElement.dataset.launching).toBeUndefined();
});

it("skips the sequence when the motion preference is reduced", () => {
  document.documentElement.dataset.motion = "reduced";
  const onEnter = vi.fn();
  const { container } = render(<LaunchOverlay ready onEnter={onEnter} />);
  expect(onEnter).toHaveBeenCalledTimes(1);
  act(() => { vi.advanceTimersByTime(200); });
  expect(container.querySelector(".launch-overlay")).toBeNull();
});
