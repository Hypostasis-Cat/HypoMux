// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AppearanceProvider, useAppearance } from "./appearance.store";
const mocks = vi.hoisted(() => ({ load: vi.fn(), save: vi.fn(), native: vi.fn() }));
vi.mock("./background.service", () => ({ appearancePersistence: { load: mocks.load, save: mocks.save }, loadLegacyBrowserAppearance: () => null }));
vi.mock("../platform/desktop", () => ({ desktopPlatform: { setWindowAppearance: mocks.native } }));
let appearance: ReturnType<typeof useAppearance>;
function Probe() { appearance = useAppearance(); return null; }
beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  mocks.load.mockResolvedValue(null);
  mocks.save.mockResolvedValue(undefined);
  mocks.native.mockResolvedValue({ applied: true, fallback: false });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
it("previews slider changes immediately but coalesces saves without rebuilding theme or native material", async () => {
  await act(async () => { render(<AppearanceProvider><Probe /></AppearanceProvider>); });
  await act(async () => { vi.advanceTimersByTime(300); });
  mocks.save.mockClear(); mocks.native.mockClear();
  const theme = appearance.fluentTheme;
  act(() => appearance.update({ panelOpacity: 55 }));
  act(() => { vi.advanceTimersByTime(200); });
  act(() => appearance.update({ panelOpacity: 70, panelBlur: 24 }));
  expect(document.documentElement.style.getPropertyValue("--hm-panel-opacity")).toBe("0.7");
  expect(appearance.fluentTheme).toBe(theme);
  expect(mocks.native).not.toHaveBeenCalled();
  expect(mocks.save).not.toHaveBeenCalled();
  await act(async () => { vi.advanceTimersByTime(300); });
  expect(mocks.save).toHaveBeenCalledTimes(1);
  expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ panelOpacity: 70, panelBlur: 24 }));
});
it("flushes the most recent pending appearance when leaving the document", async () => {
  await act(async () => { render(<AppearanceProvider><Probe /></AppearanceProvider>); });
  act(() => appearance.update({ panelOpacity: 61 }));
  await act(async () => { window.dispatchEvent(new Event("pagehide")); });
  expect(mocks.save).toHaveBeenCalledTimes(1);
  expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ panelOpacity: 61 }));
  await act(async () => { vi.advanceTimersByTime(300); });
  expect(mocks.save).toHaveBeenCalledTimes(1);
});
