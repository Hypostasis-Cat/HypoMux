// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
        entries: vi.fn(),
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
    vi.mocked(appServices.ruleSets.entries).mockResolvedValue({ entries: [], total: 0, downloaded: false });
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
    await waitFor(() => expect(screen.getAllByText("Steam")[0]).toBeTruthy());
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
    expect(screen.getAllByText("Steam")[0]).toBeTruthy();
  });

  it("deletes a rule set after confirmation", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);

    renderPanel();
    await waitFor(() => expect(screen.getByRole("button", { name: /删除/ })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /删除/ }));
    expect(appServices.ruleSets.save).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(appServices.ruleSets.save).toHaveBeenCalled());
    expect((vi.mocked(appServices.ruleSets.save).mock.calls[0][0] as RuleSet[]).length).toBe(0);

  });

  it("blocks writes after a list failure and allows retry without losing existing sets", async () => {
    vi.mocked(appServices.ruleSets.list).mockRejectedValueOnce(new Error("读取失败")).mockResolvedValue([sample]);
    renderPanel();
    const retry = await screen.findByRole("button", { name: "重新读取规则集" });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "New" } });
    fireEvent.change(screen.getByLabelText("订阅地址"), { target: { value: "https://example.com/new" } });
    expect((screen.getByRole("button", { name: "添加规则集" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(retry);
    await screen.findAllByText("Steam");
    fireEvent.click(screen.getByRole("button", { name: "添加规则集" }));
    await waitFor(() => expect(appServices.ruleSets.save).toHaveBeenCalled());
    expect(vi.mocked(appServices.ruleSets.save).mock.calls[0][0].map(set => set.name)).toEqual(["Steam", "New"]);
  });

  it("rejects fractional and out-of-range priorities instead of silently changing them", async () => {
    renderPanel();
    await screen.findByText("还没有规则集；在下方添加订阅地址即可按类别分流。");
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "New" } });
    fireEvent.change(screen.getByLabelText("订阅地址"), { target: { value: "https://example.com/new" } });
    for (const priority of ["1.5", "1000", "-1", "abc"]) {
      fireEvent.change(screen.getByLabelText("优先级"), { target: { value: priority } });
      expect(screen.getByText("请输入 0–999 的整数")).toBeTruthy();
      expect((screen.getByRole("button", { name: "添加规则集" }) as HTMLButtonElement).disabled).toBe(true);
    }
    expect(appServices.ruleSets.save).not.toHaveBeenCalled();
  });

  it("keeps the delete dialog and row when persistence fails", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);
    vi.mocked(appServices.ruleSets.save).mockRejectedValue(new Error("磁盘写入失败"));
    renderPanel();
    fireEvent.click(await screen.findByRole("button", { name: "删除" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "确认删除" }));
    await screen.findByText("Error: 磁盘写入失败");
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.getAllByText("Steam").length).toBeGreaterThan(0);
  });

  it("notifies the parent after a subscription change", async () => {
    const onChanged = vi.fn();
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);
    render(<LanguageProvider><RuleSetsPanel outbounds={outbounds} onChanged={onChanged} /></LanguageProvider>);
    fireEvent.click(await screen.findByRole("button", { name: "立即更新" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledOnce());
  });
});

describe("RuleSetsPanel null tolerance", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  afterEach(() => cleanup());

  it("renders the empty state when the backend returns null", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue(null as unknown as RuleSet[]);
    render(
      <LanguageProvider>
        <RuleSetsPanel outbounds={outbounds} />
      </LanguageProvider>,
    );
    await waitFor(() => expect(screen.getByText("还没有规则集；在下方添加订阅地址即可按类别分流。")).toBeTruthy());
  });
});

describe("RuleSetsPanel contents", () => {
  afterEach(() => cleanup());
  it("shows downloaded entries and filters through the service", async () => {
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);
    vi.mocked(appServices.ruleSets.entries).mockResolvedValue({ entries: [{ kind: "domain_suffix", value: "steamcommunity.com" }], total: 1, downloaded: true });
    renderPanel();
    await waitFor(() => expect(screen.getByText("steamcommunity.com")).toBeTruthy());
    expect(screen.getByText(sample.url)).toBeTruthy();
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索规则集内容" }), { target: { value: "community" } });
    await waitFor(() => expect(appServices.ruleSets.entries).toHaveBeenLastCalledWith(sample.id, "community", 0, 100));
  });
  it("cancels deletion without saving", async () => {
    vi.clearAllMocks();
    vi.mocked(appServices.ruleSets.list).mockResolvedValue([sample]);
    renderPanel();
    fireEvent.click(await screen.findByRole("button", { name: "删除" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "取消" }));
    expect(appServices.ruleSets.save).not.toHaveBeenCalled();
  });
});
