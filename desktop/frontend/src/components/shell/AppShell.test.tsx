// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { publishAIAvailability } from "../../state/aiAvailability";
import { AppShell } from "./AppShell";
import { usePageActive } from "./PageActivity";
import type { AppPage } from "./CompactNavigation";

const mocks = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("../../platform/services", () => ({ appServices: { settings: { get: mocks.get } } }));
vi.mock("../ai/AIAssistant", () => ({ AIAssistant: () => <div data-testid="assistant">Mux</div> }));
vi.mock("../material/useCardGlowField", () => ({ useCardGlowField: () => {} }));
vi.mock("./TitleBar", () => ({ TitleBar: () => null }));
vi.mock("./CompactNavigation", () => ({ CompactNavigation: ({ aiEnabled }: { aiEnabled: boolean }) => <nav>{aiEnabled && <button>AI assistant</button>}</nav> }));
vi.mock("../../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
afterEach(cleanup);
beforeEach(() => { mocks.get.mockReset().mockResolvedValue({ ai_enabled: false }); });

function DraftPage({ page }: { page: AppPage }) {
  const [draft, setDraft] = useState("");
  const active = usePageActive();
  return <div data-testid={page} data-active={active} style={{ overflow: "auto", height: 100 }}>
    <input aria-label={`${page} draft`} value={draft} onChange={event => setDraft(event.target.value)} />
  </div>;
}

it("mounts pages on demand and preserves drafts, scroll and DOM identity across navigation", () => {
  const shell = (page: AppPage) => <AppShell page={page} onPageChange={() => {}} pageDirection="forward" animatePage renderPage={item => <DraftPage page={item} />} />;
  const view = render(shell("routing"));
  expect(screen.queryByTestId("settings")).toBeNull();
  const draft = screen.getByRole("textbox", { name: "routing draft" });
  const scroller = screen.getByTestId("routing");
  fireEvent.change(draft, { target: { value: "unfinished.exe" } });
  scroller.scrollTop = 240;
  view.rerender(shell("settings"));
  expect(screen.queryByRole("textbox", { name: "routing draft" })).toBeNull();
  expect(scroller.dataset.active).toBe("false");
  view.rerender(shell("routing"));
  expect(screen.getByRole("textbox", { name: "routing draft" })).toBe(draft);
  expect((draft as HTMLInputElement).value).toBe("unfinished.exe");
  expect(scroller.scrollTop).toBe(240);
  expect(scroller.dataset.active).toBe("true");
  expect(screen.getByTestId("settings").dataset.active).toBe("false");
});

it("does not mount AI while settings load or when persisted AI is off", async () => {
  let resolve!: (settings: { ai_enabled: boolean }) => void;
  mocks.get.mockReturnValue(new Promise(done => { resolve = done; }));
  render(<AppShell page="home" onPageChange={() => {}} pageDirection="forward" animatePage={false} />);
  expect(screen.queryByTestId("assistant")).toBeNull();
  expect(screen.queryByRole("button", { name: "AI assistant" })).toBeNull();
  await act(async () => resolve({ ai_enabled: false }));
  expect(screen.queryByTestId("assistant")).toBeNull();
});

it("unmounts AI, hides its entry and redirects an open workspace when disabled", async () => {
  mocks.get.mockResolvedValue({ ai_enabled: true });
  const navigate = vi.fn();
  render(<AppShell page="assistant" onPageChange={navigate} pageDirection="forward" animatePage={false} />);
  expect(await screen.findByTestId("assistant")).toBeTruthy();
  expect(screen.getByRole("button", { name: "AI assistant" })).toBeTruthy();
  expect(navigate).not.toHaveBeenCalled();
  act(() => publishAIAvailability(false));
  expect(screen.queryByTestId("assistant")).toBeNull();
  expect(screen.queryByRole("button", { name: "AI assistant" })).toBeNull();
  expect(navigate).toHaveBeenCalledWith("home");
  act(() => publishAIAvailability(true));
  expect(await screen.findByTestId("assistant")).toBeTruthy();
});

it("keeps AI disabled after a settings read failure and recovers on confirmed settings", async () => {
  mocks.get.mockRejectedValue(new Error("read failed"));
  render(<AppShell page="home" onPageChange={() => {}} pageDirection="forward" animatePage={false} />);
  await waitFor(() => expect(mocks.get).toHaveBeenCalled());
  expect(screen.queryByTestId("assistant")).toBeNull();
  act(() => publishAIAvailability(true));
  expect(await screen.findByTestId("assistant")).toBeTruthy();
});

it("ignores an older startup response after a confirmed toggle", async () => {
  let resolve!: (settings: { ai_enabled: boolean }) => void;
  mocks.get.mockReturnValue(new Promise(done => { resolve = done; }));
  render(<AppShell page="home" onPageChange={() => {}} pageDirection="forward" animatePage={false} />);
  act(() => publishAIAvailability(false));
  await act(async () => resolve({ ai_enabled: true }));
  expect(screen.queryByTestId("assistant")).toBeNull();
  expect(screen.queryByRole("button", { name: "AI assistant" })).toBeNull();
});
