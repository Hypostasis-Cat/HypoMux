// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SkinWardrobe } from "./SkinWardrobe";

const mocks = vi.hoisted(() => ({ save: vi.fn(), install: vi.fn(), remove: vi.fn() }));
vi.mock("../../../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
vi.mock("./SkinCharacter", () => ({ SkinCharacter: ({ state }: { state: string }) => <span data-testid="character-state">{state}</span> }));
vi.mock("./store", () => ({
  useSkins: () => ({ loaded: true, error: "", preferences: { selected: "builtin.default", size: 112, animate: true }, skins: [{ manifest: { id: "local.test", name: "Test companion", author: "Artist", version: "1.0.0", canvas: { width: 394, height: 394 }, anchor: { x: .5, y: 1 }, states: { idle: { type: "image", src: "idle.png" }, thinking: { type: "image", src: "thinking.png" } } }, files: {} }] }),
  savePreferences: mocks.save, installSkin: mocks.install, removeSkin: mocks.remove, loadSkins: vi.fn(),
}));
beforeEach(() => { mocks.save.mockReset().mockResolvedValue(undefined); mocks.install.mockReset(); mocks.remove.mockReset(); });
afterEach(cleanup);

it("previews installed skins without installing or applying, then explicitly applies the selection", async () => {
  render(<SkinWardrobe />);
  fireEvent.click(screen.getByRole("button", { name: "Preview Test companion" }));
  expect(screen.getByRole("heading", { name: "Test companion" })).toBeTruthy();
  expect(mocks.save).not.toHaveBeenCalled();
  expect(mocks.install).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Thinking" }));
  expect(screen.getByRole("button", { name: "Thinking" }).getAttribute("aria-pressed")).toBe("true");
  fireEvent.click(screen.getByRole("button", { name: "Apply character" }));
  await waitFor(() => expect(mocks.save).toHaveBeenCalledWith({ selected: "local.test" }));
  expect(mocks.install).not.toHaveBeenCalled();
});

it("previews size while dragging and keeps the slider enabled during persistence", async () => {
  let finish!: () => void;
  mocks.save.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve; }));
  render(<SkinWardrobe />);
  const slider = screen.getByRole("slider", { name: "Character size" });
  fireEvent.change(slider, { target: { value: "176" } });
  expect(mocks.save).not.toHaveBeenCalled();
  fireEvent.pointerUp(slider);
  expect(mocks.save).toHaveBeenCalledWith({ size: 176 });
  expect((slider as HTMLInputElement).disabled).toBe(false);
  fireEvent.change(slider, { target: { value: "184" } });
  expect((slider as HTMLInputElement).value).toBe("184");
  finish();
  await waitFor(() => expect((slider as HTMLInputElement).disabled).toBe(false));
});
