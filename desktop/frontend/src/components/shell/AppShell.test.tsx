// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { AppShell } from "./AppShell";
import { usePageActive } from "./PageActivity";
import type { AppPage } from "./CompactNavigation";

vi.mock("../ai/AIAssistant", () => ({ AIAssistant: () => null }));
vi.mock("../material/useCardGlowField", () => ({ useCardGlowField: () => {} }));
vi.mock("./TitleBar", () => ({ TitleBar: () => null }));
vi.mock("./CompactNavigation", () => ({ CompactNavigation: () => null }));
vi.mock("../../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
afterEach(cleanup);

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
