// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AssistantCompanion } from "./AssistantCompanion";

vi.mock("../../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
afterEach(() => { cleanup(); vi.useRealTimers(); });

it("dismisses speech automatically and brings it back while hovering", () => {
  vi.useFakeTimers();
  render(<AssistantCompanion open={false} onOpenChange={vi.fn()} running={false} pending={0} pageLabel="Home" speech="Network checked.">{null}</AssistantCompanion>);
  expect(screen.getByText("Network checked.")).toBeTruthy();
  act(() => vi.advanceTimersByTime(15000));
  expect(screen.queryByText("Network checked.")).toBeNull();
  const pet = screen.getByRole("button", { name: "Network companion" });
  fireEvent.mouseEnter(pet);
  expect(screen.getByText("Network checked.")).toBeTruthy();
  act(() => vi.advanceTimersByTime(20000));
  expect(screen.getByText("Network checked.")).toBeTruthy();
  fireEvent.mouseLeave(pet);
  act(() => vi.advanceTimersByTime(300));
  expect(screen.queryByText("Network checked.")).toBeNull();
});
