// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ProxyChainDialog } from "./ProxyChainDialog";
import { LanguageProvider } from "../i18n/i18n";
import { appServices } from "../platform/services";

vi.mock("../platform/services", async (importOriginal) => {
  const original = await importOriginal<typeof import("../platform/services")>();
  return {
    ...original,
    appServices: {
      ...original.appServices,
      routing: { listProcesses: vi.fn(), snapshot: vi.fn(), previewBatch: vi.fn(), save: vi.fn() },
      engine: {
        ...original.appServices.engine,
        connections: vi.fn().mockResolvedValue({ phase: "stopped", mode: "proxy", sampled_at: "", connections: [] }),
      },
    },
  };
});

const renderDialog = (overrides: Partial<Parameters<typeof ProxyChainDialog>[0]> = {}) => {
  const props = {
    open: true,
    socksPort: 12800,
    systemProxyTakeover: true,
    busy: false,
    onClose: vi.fn(),
    onEnableChainMode: vi.fn().mockResolvedValue(true),
    ...overrides,
  };
  render(<LanguageProvider><ProxyChainDialog {...props} /></LanguageProvider>);
  return props;
};

describe("ProxyChainDialog", () => {
  beforeEach(() => {
    // FluentUI MessageBar reflows through ResizeObserver, which jsdom lacks, and
    // the live chain status renders one in every test.
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("releases the system proxy and copies the script for the configured port", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    const props = renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "关闭系统代理接管" }));
    expect(props.onEnableChainMode).toHaveBeenCalledTimes(1);

    const code = screen.getByLabelText("联动配置") as HTMLTextAreaElement;
    expect(code.value).toContain("const HYPOMUX_PORT = 12800;");
    fireEvent.click(screen.getByRole("button", { name: "复制配置" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(code.value));
    expect(await screen.findByText("已复制到剪贴板。")).toBeTruthy();
  });

  it("offers the YAML and sing-box snippets and hides the takeover action once released", () => {
    renderDialog({ systemProxyTakeover: false });

    expect(screen.queryByRole("button", { name: "关闭系统代理接管" })).toBeNull();
    fireEvent.click(screen.getByRole("tab", { name: "Mihomo YAML" }));
    expect((screen.getByLabelText("联动配置") as HTMLTextAreaElement).value).toContain("port: 12800");
    fireEvent.click(screen.getByRole("tab", { name: "sing-box" }));
    expect((screen.getByLabelText("联动配置") as HTMLTextAreaElement).value).toContain("\"server_port\": 12800");
  });

  it("shows when a proxy client is leaving through HypoMux", async () => {
    vi.mocked(appServices.engine.connections).mockResolvedValueOnce({
      phase: "running", mode: "proxy", sampled_at: "", connections: [
        { id: 1, process: "verge-mihomo.exe", protocol: "socks5", adapter: "WLAN", outbound: "aggregation", started_at: "", bytes_up: 0, bytes_down: 0 },
        { id: 2, process: "verge-mihomo.exe", protocol: "socks5", adapter: "以太网", outbound: "aggregation", started_at: "", bytes_up: 0, bytes_down: 0 },
      ],
    } as any);
    renderDialog();

    expect(await screen.findByText("联动已生效：verge-mihomo.exe 有 2 条连接正经 HypoMux 出站，分布在 WLAN、以太网。")).toBeTruthy();
  });

  it("can generate the script without aggregating direct traffic", () => {
    renderDialog();
    const code = () => (screen.getByLabelText("联动配置") as HTMLTextAreaElement).value;

    expect(code()).toContain("const AGGREGATE_DIRECT = true;");
    fireEvent.click(screen.getByRole("checkbox"));
    expect(code()).toContain("const AGGREGATE_DIRECT = false;");
  });

  it("adds aggregation rules for running proxy cores in one click", async () => {
    const routing = vi.mocked(appServices.routing);
    routing.listProcesses.mockResolvedValue(["xray.exe"]);
    routing.snapshot.mockResolvedValue({ revision: "r", match_order: null, rules: [], outbounds: [] } as any);
    routing.previewBatch.mockResolvedValue({
      items: [{ input: "xray.exe", status: "add", rule: { match_type: "process", value: "xray.exe", outbound: "aggregation" } }],
    } as any);
    routing.save.mockResolvedValue({ restart_required: false } as any);
    renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "让代理内核参与聚合" }));

    expect(await screen.findByText(/已添加多卡聚合规则：xray\.exe。/)).toBeTruthy();
    expect(routing.save).toHaveBeenCalledTimes(1);
  });
});
