// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AppNotificationCenter,
  AppNotificationProvider,
  useAppNotifications,
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
    act(() => vi.advanceTimersByTime(240));
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
