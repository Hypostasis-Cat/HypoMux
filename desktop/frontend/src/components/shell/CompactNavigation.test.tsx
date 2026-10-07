// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CompactNavigation } from "./CompactNavigation";

vi.mock("../../i18n/i18n", () => ({ useI18n: () => ({ locale: "en", t: (key: string) => key }) }));
beforeEach(() => vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it("hides the AI entry while retaining all network navigation", () => {
  const props = { page: "settings" as const, onPageChange: vi.fn() };
  const view = render(<CompactNavigation {...props} aiEnabled />);
  expect(screen.getByRole("button", { name: "AI assistant" })).toBeTruthy();
  view.rerender(<CompactNavigation {...props} aiEnabled={false} />);
  expect(screen.queryByRole("button", { name: "AI assistant" })).toBeNull();
  expect(screen.getByRole("button", { name: "nav_settings" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "nav_routing" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Toolbox" })).toBeTruthy();
  view.rerender(<CompactNavigation {...props} aiEnabled />);
  expect(screen.getByRole("button", { name: "AI assistant" })).toBeTruthy();
});

it("keeps keyboard focus and skips the exiting AI entry", () => {
  const props = { page: "settings" as const, onPageChange: vi.fn() };
  const view = render(<CompactNavigation {...props} aiEnabled />);
  const routing = screen.getByRole("button", { name: "nav_routing" });
  const ai = screen.getByRole("button", { name: "AI assistant" }) as HTMLButtonElement;
  routing.focus();
  view.rerender(<CompactNavigation {...props} aiEnabled={false} />);
  expect(document.activeElement).toBe(routing);
  expect(ai.disabled).toBe(true);
  fireEvent.keyDown(routing, { key: "ArrowUp" });
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "nav_home" }));
  fireEvent.click(ai);
  expect(props.onPageChange).not.toHaveBeenCalled();
  view.rerender(<CompactNavigation {...props} aiEnabled />);
  expect(screen.getByRole("button", { name: "AI assistant" })).toBe(ai);
  fireEvent.keyDown(screen.getByRole("button", { name: "nav_home" }), { key: "ArrowDown" });
  expect(document.activeElement).toBe(ai);
});
