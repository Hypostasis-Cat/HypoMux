// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AboutPage } from "./AboutPage";

const mocks = vi.hoisted(() => ({ check: vi.fn(), download: vi.fn(), installAndQuit: vi.fn(), progress: vi.fn(), openURL: vi.fn(), notify: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { updater: {
  check: mocks.check, download: mocks.download, installAndQuit: mocks.installAndQuit, progress: mocks.progress,
} } }));
vi.mock("../platform/desktop", () => ({ desktopPlatform: { openURL: mocks.openURL } }));
vi.mock("../components/notifications/AppNotifications", () => ({
  useAppNotifications: () => ({ notify: mocks.notify }),
}));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en", t: (key: string) => key }) }));

beforeEach(() => {
  mocks.progress.mockResolvedValue({ state: "idle", downloaded: 0, total: 0 });
  // Give Tabster a visible viewport; jsdom otherwise hides every focus target.
  vi.spyOn(document.body, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 1280, 800));
  vi.spyOn(HTMLElement.prototype, "offsetParent", "get").mockImplementation(function (this: HTMLElement) {
    if (!this.isConnected || this === document.body || getComputedStyle(this).position === "fixed") return null;
    for (let element: HTMLElement | null = this; element; element = element.parentElement) {
      if (element.hidden || getComputedStyle(element).display === "none") return null;
    }
    return this.parentElement;
  });
  // Model browser scrolling when Fluent focuses a link at the end of long notes.
  const focus = HTMLElement.prototype.focus;
  vi.spyOn(HTMLElement.prototype, "focus").mockImplementation(function (this: HTMLElement, options?: FocusOptions) {
    focus.call(this, options);
    const notes = this.closest<HTMLElement>(".update-notes");
    if (notes && !options?.preventScroll) notes.scrollTop = 900;
  });
});

it("retries installer startup without downloading again, and allows a fresh download", async () => {
  mocks.check.mockResolvedValue({ available: true, current_version: "2.7.0", release: {
    tag_name: "v2.7.1", installer_digest: "sha256:one", installer_size: 100, notes: "New release",
  } });
  mocks.download.mockResolvedValue("C:/Temp/HypoMuxUpdate-test/HypoMux_Setup_2.7.1.exe");
  mocks.installAndQuit.mockRejectedValue(new Error("Could not start installer"));
  render(<FluentProvider theme={webLightTheme}><AboutPage /></FluentProvider>);
  fireEvent.click(screen.getByRole("button", { name: "about_check_update" }));
  const dialog = await screen.findByRole("dialog");
  for (let attempt = 1; attempt <= 2; attempt++) {
    fireEvent.click(within(dialog).getByRole("button", { name: "about_update_now" }));
    await waitFor(() => {
      expect(mocks.installAndQuit).toHaveBeenCalledTimes(attempt);
      expect(within(dialog).getByRole("button", { name: "about_update_now" }).hasAttribute("disabled")).toBe(false);
    });
    expect(mocks.download).toHaveBeenCalledTimes(1);
  }
  expect(mocks.notify).toHaveBeenCalledWith(expect.objectContaining({ title: "Update incomplete", intent: "error" }));
  fireEvent.click(within(dialog).getByRole("button", { name: "Download again" }));
  await waitFor(() => {
    expect(mocks.installAndQuit).toHaveBeenCalledTimes(3);
    expect(within(dialog).getByRole("button", { name: "about_update_now" }).hasAttribute("disabled")).toBe(false);
  });
  expect(mocks.download).toHaveBeenCalledTimes(2);

  // A later check must not install a cached package belonging to another release.
  fireEvent.click(within(dialog).getByRole("button", { name: "about_update_later" }));
  const checkButton = await screen.findByRole("button", { name: "about_check_update" });
  const nextRelease = { tag_name: "v2.7.2", installer_digest: "sha256:two", installer_size: 120 };
  mocks.check.mockResolvedValue({ available: true, current_version: "2.7.0", release: nextRelease });
  fireEvent.click(checkButton);
  const nextDialog = await screen.findByRole("dialog");
  fireEvent.click(within(nextDialog).getByRole("button", { name: "about_update_now" }));
  await waitFor(() => expect(mocks.installAndQuit).toHaveBeenCalledTimes(4));
  expect(mocks.download).toHaveBeenCalledTimes(3);
  expect(mocks.download).toHaveBeenLastCalledWith(nextRelease);
});

it("shows verification and launch separately and ignores late download progress", async () => {
  mocks.check.mockResolvedValue({ available: true, current_version: "2.7.0", release: {
    tag_name: "v2.7.1", installer_digest: "sha256:one", installer_size: 100,
  } });
  let finishDownload!: (path: string) => void;
  mocks.download.mockImplementation(() => new Promise<string>((resolve) => { finishDownload = resolve; }));
  let finishInstall!: () => void;
  mocks.installAndQuit.mockImplementation(() => new Promise<void>((resolve) => { finishInstall = resolve; }));
  let lateProgress!: (value: object) => void;
  mocks.progress.mockResolvedValueOnce({ state: "verifying", downloaded: 100, total: 100 })
    .mockImplementationOnce(() => new Promise((resolve) => { lateProgress = resolve; }));
  render(<FluentProvider theme={webLightTheme}><AboutPage /></FluentProvider>);
  fireEvent.click(screen.getByRole("button", { name: "about_check_update" }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "about_update_now" }));
  await within(dialog).findByRole("button", { name: "Verifying installer…" });
  await waitFor(() => expect(lateProgress).toBeTypeOf("function"));
  finishDownload("installer.exe");
  await within(dialog).findByRole("button", { name: "Starting installer…" });
  lateProgress({ state: "downloading", downloaded: 20, total: 100 });
  finishInstall();
  await waitFor(() => expect(mocks.notify).toHaveBeenCalledWith(expect.objectContaining({ title: "Download complete" })));
  expect(within(dialog).getByRole("button", { name: "Starting installer…" })).toBeTruthy();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

it.each([true, false])("opens and reopens notes at the top (footer link: %s)", async (hasLink) => {
  mocks.check.mockResolvedValue({
    available: true,
    current_version: "2.5.8",
    release: {
      tag_name: "v2.5.9",
      notes: "# What's new\n\n" + "Update details.\n\n".repeat(60) +
        (hasLink ? "[Full changelog](https://github.com/Hypostasis-Cat/HypoMux/releases)" : "End of notes."),
    },
  });
  render(<FluentProvider theme={webLightTheme}><AboutPage /></FluentProvider>);

  for (let attempt = 0; attempt < 3; attempt++) {
    // Dialog removal and Tabster releasing aria-hidden on the page are separate
    // events. Wait for the real accessible, enabled trigger before reopening.
    const checkButton = await waitFor(() => {
      const button = screen.getByRole("button", { name: "about_check_update" });
      expect(button.hasAttribute("disabled")).toBe(false);
      return button;
    });
    fireEvent.click(checkButton);
    const dialog = await screen.findByRole("dialog");
    const title = within(dialog).getByText("about_update_available_title");
    const notes = dialog.querySelector<HTMLElement>(".update-notes")!;
    await waitFor(() => {
      expect(document.activeElement).toBe(title);
      expect(notes.scrollTop).toBe(0);
      expect(notes.parentElement!.scrollTop).toBe(0);
    });
    // Reading is not interrupted by a continuous scroll reset.
    notes.scrollTop = 400;
    fireEvent.scroll(notes);
    expect(notes.scrollTop).toBe(400);
    if (hasLink) {
      fireEvent.click(within(dialog).getByRole("link", { name: "Full changelog" }));
      expect(mocks.openURL).toHaveBeenCalledWith("https://github.com/Hypostasis-Cat/HypoMux/releases");
    }
    fireEvent.click(within(dialog).getByRole("button", { name: "about_update_later" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(screen.getByRole("button", { name: "about_check_update" })).toBe(checkButton);
    });
  }
});
