// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AssistantCompanion } from "./AssistantCompanion";
const state = vi.hoisted(() => ({ loaded: false, skins: [] as any[], preferences: { selected: "builtin.default", sizes: {}, animate: true } }));
vi.mock("../../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
vi.mock("./skins/store", () => ({ useSkins: () => state, getSkinSize: () => 112 }));
vi.mock("./skins/SkinCharacter", () => ({ SkinCharacter: () => <span data-testid="custom-skin" /> }));
afterEach(cleanup);
it("does not flash the default character while saved preferences and skins are loading", () => {
  const props = { open: false, onOpenChange: vi.fn(), running: false, pending: 0, pageLabel: "Home", children: null };
  const view = render(<AssistantCompanion {...props} />);
  expect(screen.getByRole("status", { name: "Loading companion" })).toBeTruthy();
  expect(view.container.querySelector(".ai-pet-character")).toBeNull();
  expect(view.container.querySelector(".ai-pet-aura")).toBeNull();
  state.preferences.selected = "custom.paimon";
  state.skins = [{ manifest: { id: "custom.paimon", name: "Paimon", canvas: { width: 100, height: 100 }, anchor: { x: .5, y: 1 } } }];
  state.loaded = true;
  view.rerender(<AssistantCompanion {...props} />);
  expect(screen.getByTestId("custom-skin")).toBeTruthy();
  expect(view.container.querySelector(".ai-pet-character")).toBeNull();
  state.preferences.selected = "builtin.default";
  view.rerender(<AssistantCompanion {...props} />);
  expect(view.container.querySelector(".ai-pet-character")).not.toBeNull();
});
