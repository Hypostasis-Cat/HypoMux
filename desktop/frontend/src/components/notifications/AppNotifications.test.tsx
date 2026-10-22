// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AppNotificationCenter,
  AppNotificationProvider,
  useAppNotifications,
  type AppNotificationInput,
} from "./AppNotifications";

vi.mock("../../i18n/i18n", () => ({
  useI18n: () => ({ locale: "en" }),
}));

const rawError = "stage=tun_data_path endpoint=http://www.msftconnecttest.com/connecttest.txt: curl: (28) Resolving timed out after 4007 milliseconds";

function NotificationTrigger() {
  const { notify } = useAppNotifications();
  return (
    <button
      type="button"
      onClick={() => notify({
        title: "Operation incomplete",
        message: rawError,
        intent: "error",
        dedupeKey: "test-error",
      })}
    >
      Trigger error
    </button>
  );
}

function TimedTriggers({ action = false }: { action?: boolean }) {
  const { notify } = useAppNotifications();
  const send = (title: string) => notify({
    title, intent: "success", timeout: action ? undefined : 1000,
    action: action ? { label: "Review", onClick: () => undefined } : undefined,
  } satisfies AppNotificationInput);
  return <><button onClick={() => send("Saved A")}>Save A</button><button onClick={() => send("Saved B")}>Save B</button></>;
}

function renderTimed(action = false) {
  vi.useFakeTimers();
  render(<AppNotificationProvider><AppNotificationCenter /><TimedTriggers action={action} /></AppNotificationProvider>);
  fireEvent.click(screen.getByRole("button", { name: "Save A" }));
}

describe("AppNotificationCenter", () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("shows a concise in-flow error and keeps diagnostics behind details", () => {
    render(
      <AppNotificationProvider>
        <AppNotificationCenter />
        <NotificationTrigger />
      </AppNotificationProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Trigger error" }));

    const alert = screen.getByRole("alert");
    expect(alert.classList.contains("global-notification-region")).toBe(true);
    expect(alert.textContent).toContain("The operation timed out. Please try again.");
    expect(alert.textContent).toContain("HM-E1001");
    expect(alert.textContent).not.toContain("msftconnecttest");

    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(alert.textContent).toContain(rawError);

    fireEvent.click(screen.getByRole("button", { name: "Trigger error" }));
    expect(screen.getByRole("alert").textContent).toContain("×2");
  });

  it("keeps the island mounted while its exit animation finishes", () => {
    vi.useFakeTimers();
    render(
      <AppNotificationProvider>
        <AppNotificationCenter />
        <NotificationTrigger />
      </AppNotificationProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Trigger error" }));
    fireEvent.click(screen.getByRole("button", { name: "Dismiss notification" }));

    expect(screen.getByRole("alert").classList.contains("is-leaving")).toBe(true);
    act(() => vi.advanceTimersByTime(299));
    expect(screen.getByRole("alert")).toBeTruthy();
    act(() => vi.advanceTimersByTime(1));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("pauses while hovered and resumes with the remaining time", () => {
    renderTimed();
    act(() => vi.advanceTimersByTime(600));
    fireEvent.pointerEnter(screen.getByRole("status"));
    act(() => vi.advanceTimersByTime(5000));
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(false);
    fireEvent.pointerLeave(screen.getByRole("status"));
    act(() => vi.advanceTimersByTime(399));
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(false);
    act(() => vi.advanceTimersByTime(1));
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(true);
  });

  it("keeps keyboard focus protected after the pointer leaves", () => {
    renderTimed();
    const region = screen.getByRole("status");
    const dismiss = screen.getByRole("button", { name: "Dismiss notification" });
    fireEvent.pointerEnter(region);
    fireEvent.focus(dismiss);
    fireEvent.pointerLeave(region);
    act(() => vi.advanceTimersByTime(5000));
    expect(region.classList.contains("is-leaving")).toBe(false);
    fireEvent.blur(dismiss, { relatedTarget: screen.getByRole("button", { name: "Save A" }) });
    act(() => vi.advanceTimersByTime(1000));
    expect(region.classList.contains("is-leaving")).toBe(true);
  });

  it("gives queued notifications their own reading time once displayed", () => {
    renderTimed();
    act(() => vi.advanceTimersByTime(500));
    fireEvent.click(screen.getByRole("button", { name: "Save B" }));
    act(() => vi.advanceTimersByTime(1000));
    expect(screen.getByRole("status").textContent).toContain("Saved A");
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(false);
    act(() => vi.advanceTimersByTime(1000));
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(true);
  });

  it("keeps action notifications available and supports Escape dismissal", () => {
    renderTimed(true);
    act(() => vi.advanceTimersByTime(30000));
    expect(screen.getByRole("button", { name: "Review" })).toBeTruthy();
    fireEvent.keyDown(screen.getByRole("button", { name: "Review" }), { key: "Escape" });
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(true);
  });

  it("does not expire notifications while the document is hidden", () => {
    renderTimed();
    const hidden = vi.spyOn(document, "hidden", "get");
    hidden.mockReturnValue(true);
    fireEvent(document, new Event("visibilitychange"));
    act(() => vi.advanceTimersByTime(10000));
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(false);
    hidden.mockReturnValue(false);
    fireEvent(document, new Event("visibilitychange"));
    act(() => vi.advanceTimersByTime(1000));
    expect(screen.getByRole("status").classList.contains("is-leaving")).toBe(true);
    hidden.mockRestore();
  });

  it("lets details collapse before unmounting them", () => {
    vi.useFakeTimers();
    render(<AppNotificationProvider><AppNotificationCenter /><NotificationTrigger /></AppNotificationProvider>);
    fireEvent.click(screen.getByRole("button", { name: "Trigger error" }));
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    fireEvent.click(screen.getByRole("button", { name: "Hide" }));

    const details = document.getElementById("notification-details");
    expect(details?.classList.contains("is-closing")).toBe(true);
    expect(screen.getByRole("alert").classList.contains("is-expanded")).toBe(false);
    act(() => vi.advanceTimersByTime(200));
    expect(document.getElementById("notification-details")).toBeNull();
  });

  it("traces the auto-dismiss countdown and pauses it with the timer", () => {
    renderTimed();
    const ring = document.querySelector<SVGElement>(".global-notification-countdown");
    expect(ring?.style.getPropertyValue("--hm-island-timeout")).toBe("1000ms");
    expect(ring?.classList.contains("is-paused")).toBe(false);
    fireEvent.pointerEnter(screen.getByRole("status"));
    expect(document.querySelector(".global-notification-countdown")?.classList.contains("is-paused")).toBe(true);
  });

  it("omits the countdown for notifications that wait for the user", () => {
    renderTimed(true);
    expect(document.querySelector(".global-notification-countdown")).toBeNull();
  });

  it("stacks queued notices and pulses when the visible content changes", () => {
    renderTimed();
    const region = screen.getByRole("status");
    expect(region.className).not.toMatch(/is-pulse|has-stack/);

    fireEvent.click(screen.getByRole("button", { name: "Save B" }));
    expect(region.classList.contains("has-stack-1")).toBe(true);
    expect(region.classList.contains("is-pulse-a")).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Dismiss notification" }));
    expect(region.textContent).toContain("Saved A");
    expect(region.classList.contains("has-stack-1")).toBe(false);
    expect(region.classList.contains("is-pulse-b")).toBe(true);
  });
});
