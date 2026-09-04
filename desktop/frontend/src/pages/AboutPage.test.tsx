// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AboutPage } from "./AboutPage";

const mocks = vi.hoisted(() => ({ check: vi.fn(), openURL: vi.fn(), notify: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { updater: { check: mocks.check } } }));
vi.mock("../platform/desktop", () => ({ desktopPlatform: { openURL: mocks.openURL } }));
vi.mock("../components/notifications/AppNotifications", () => ({
  useAppNotifications: () => ({ notify: mocks.notify }),
}));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en", t: (key: string) => key }) }));

beforeEach(() => {
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
    fireEvent.click(screen.getByRole("button", { name: "about_check_update" }));
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
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  }
});
