// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TrayMenu } from "./TrayMenu";

const mocks = vi.hoisted(() => ({ snapshot: vi.fn(), settings: vi.fn(), appearance: vi.fn(), action: vi.fn(), resize: vi.fn() }));
vi.mock("../../platform/desktop", () => ({ desktopPlatform: { trayAction: mocks.action, resizeTray: mocks.resize } }));
vi.mock("../../platform/services", () => ({ appServices: { engine: { trayStatus: mocks.snapshot }, settings: { get: mocks.settings } } }));
vi.mock("../../theme/background.service", () => ({ appearancePersistence: { load: mocks.appearance } }));

beforeEach(() => {
  vi.resetAllMocks();
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  mocks.snapshot.mockResolvedValue({ phase: "running", mode: "tun" });
  mocks.settings.mockResolvedValue({ language: "zh" });
  mocks.appearance.mockResolvedValue({ mode: "dark" });
  mocks.action.mockResolvedValue(undefined);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers(); });

it("does not poll the hidden popup and stops polling after focus loss", async () => {
  vi.useFakeTimers();
  vi.mocked(document.hasFocus).mockReturnValue(false);
  render(<TrayMenu />);
  await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
  expect(mocks.snapshot).not.toHaveBeenCalled();
  await act(async () => { fireEvent.focus(window); });
  expect(mocks.snapshot).toHaveBeenCalledTimes(1);
  await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
  expect(mocks.snapshot).toHaveBeenCalledTimes(2);
  fireEvent.blur(window);
  await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
  expect(mocks.snapshot).toHaveBeenCalledTimes(2);
});

it("loads real state and refreshes state, language and theme when reopened", async () => {
  render(<TrayMenu />);
  await waitFor(() => expect(screen.getByRole("status").textContent).toBe("运行中 · 虚拟网卡"));
  mocks.snapshot.mockResolvedValue({ phase: "failed", mode: "proxy" });
  mocks.settings.mockResolvedValue({ language: "en" });
  mocks.appearance.mockResolvedValue({ mode: "light" });
  fireEvent.blur(window);
  fireEvent.focus(window);
  await waitFor(() => expect(screen.getByRole("status").textContent).toBe("Failed · System proxy"));
  expect(screen.getByRole("menuitem", { name: "Show main window" })).toBeTruthy();
  expect(mocks.appearance).toHaveBeenCalledTimes(2);
});

it.each([["显示主窗口", "show"], ["隐藏到托盘", "hide"], ["退出 HypoMux", "quit"]])("dispatches %s to the host", async (name, action) => {
  render(<TrayMenu />);
  fireEvent.click(screen.getByRole("menuitem", { name }));
  await waitFor(() => expect(mocks.action).toHaveBeenCalledWith(action));
});

it("moves keyboard focus and dismisses without hiding the main window", async () => {
  render(<TrayMenu />);
  await screen.findByText("运行中");
  const items = screen.getAllByRole("menuitem");
  expect(document.activeElement).toBe(items[0]);
  fireEvent.keyDown(items[0], { key: "ArrowUp" });
  expect(document.activeElement).toBe(items[2]);
  fireEvent.keyDown(items[2], { key: "Home" });
  expect(document.activeElement).toBe(items[0]);
  fireEvent.keyDown(items[0], { key: "Escape" });
  await waitFor(() => expect(mocks.action).toHaveBeenCalledWith("dismiss"));
});

it("does not present stale success when reading status fails and recovers on reopen", async () => {
  mocks.snapshot.mockRejectedValue(new Error("offline"));
  render(<TrayMenu />);
  await screen.findByText("状态暂不可用");
  expect(screen.getByRole("status").getAttribute("data-phase")).toBe("unknown");
  mocks.snapshot.mockResolvedValue({ phase: "stopped", mode: "proxy" });
  fireEvent.focus(window);
  await screen.findByText("未启动");
});

it("reports failed actions and prevents duplicate dispatch while pending", async () => {
  let reject!: (error: Error) => void;
  mocks.action.mockReturnValue(new Promise((_, fail) => { reject = fail; }));
  render(<TrayMenu />);
  const button = screen.getByRole("menuitem", { name: "隐藏到托盘" });
  fireEvent.click(button);
  fireEvent.click(button);
  expect(mocks.action).toHaveBeenCalledTimes(1);
  reject(new Error("unavailable"));
  expect(await screen.findByRole("alert")).toBeTruthy();
});
