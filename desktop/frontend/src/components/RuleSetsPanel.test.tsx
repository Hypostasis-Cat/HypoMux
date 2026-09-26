// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RuleSetsPanel } from "./RuleSetsPanel";
import { appServices, type RuleSet } from "../platform/services";
import { LanguageProvider } from "../i18n/i18n";

vi.mock("../platform/services", async (importOriginal) => {
  const original = await importOriginal<typeof import("../platform/services")>();
  return {
    ...original,
    appServices: {
      ...original.appServices,
      ruleSets: {
        list: vi.fn(),
        save: vi.fn(),
        update: vi.fn(),
      },
    },
  };
});

const outbounds = [
  { id: "aggregation", label: "多卡聚合叠加" },
  { id: "direct", label: "直连" },
  { id: "nic_WLAN", label: "WLAN" },
];

const sample: RuleSet = {
  id: "steam-id",
  name: "Steam",
  url: "https://rules.example.test/steam.yaml",
  outbound: "direct",
  priority: 20,
  entry_count: 128,
  format: "clash-provider",
  updated_at: 1758000000,
};

const renderPanel = () =>
  render(
    <LanguageProvider>
      <RuleSetsPanel outbounds={outbounds} />
    </LanguageProvider>,
  );

describe("RuleSetsPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // FluentUI MessageBar reflows through ResizeObserver, which jsdom lacks.
    if (typeof window.ResizeObserver === "undefined") {
      class ResizeObserverStub {
        observe() {}
        unobserve() {}
        disconnect() {}
      }
      (window as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub;
    }
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([]);
    vi.mocked(appServices.ruleSets.save).mockImplementation(async (sets) => sets);
    vi.mocked(appServices.ruleSets.update).mockImplementation(async () => [sample]);
  });

  afterEach(() => cleanup());

  it("renders the empty state when no rule sets are configured", async () => {
    renderPanel();
    await waitFor(() => expect(screen.getByText("还没有规则集；在下方添加订阅地址即可按类别分流。")).toBeTruthy());
  });

  it("shows ingestion status for a fetched rule set", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);
    renderPanel();
    await waitFor(() => expect(screen.getByText("Steam")).toBeTruthy());
    expect(screen.getByText(/128 条 · clash-provider/).textContent).toBeTruthy();
  });

  it("adds a rule set through the service and clears the draft", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([]);
    renderPanel();
    await waitFor(() => expect(screen.getByRole("button", { name: "添加规则集" })).toBeTruthy());
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "Steam" } });
    fireEvent.change(screen.getByLabelText("订阅地址"), { target: { value: "https://rules.example.test/steam.yaml" } });
    fireEvent.click(screen.getByRole("button", { name: "添加规则集" }));
    await waitFor(() => expect(appServices.ruleSets.save).toHaveBeenCalled());
    const saved = vi.mocked(appServices.ruleSets.save).mock.calls[0][0] as RuleSet[];
    expect(saved[0].name).toBe("Steam");
    expect(saved[0].id).toBe("");
    expect(saved[0].priority).toBe(0);
  });

  it("reports update failures while keeping the existing rows", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);
    vi.mocked(appServices.ruleSets.update).mockRejectedValue(new Error("订阅服务器返回状态码 500"));
    renderPanel();
    await waitFor(() => expect(screen.getByRole("button", { name: /立即更新/ })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /立即更新/ }));
    await waitFor(() => expect(screen.getByText(/更新规则集失败：/)).toBeTruthy());
    expect(screen.getByText("Steam")).toBeTruthy();
  });

  it("deletes a rule set after confirmation", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    renderPanel();
    await waitFor(() => expect(screen.getByRole("button", { name: /删除/ })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /删除/ }));
    await waitFor(() => expect(appServices.ruleSets.save).toHaveBeenCalled());
    expect((vi.mocked(appServices.ruleSets.save).mock.calls[0][0] as RuleSet[]).length).toBe(0);
    confirm.mockRestore();
  });
});
