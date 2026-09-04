// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import type { PropsWithChildren, ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppNotificationProvider } from "../components/notifications/AppNotifications";
import type { ConnectionListSnapshot, ConnectionView } from "../platform/services";
import type { HomeAdapter } from "../state/useEngineState";
import {
  connectionRuleCandidates,
  ConnectionsPage,
  preferredConnectionRuleOutbound,
} from "./ConnectionsPage";

const mocks = vi.hoisted(() => ({
  connections: vi.fn(),
  routingSnapshot: vi.fn(),
  previewBatch: vi.fn(),
  saveRules: vi.fn(),
}));

vi.mock("../platform/services", () => ({
  appServices: {
    engine: { connections: mocks.connections },
    routing: {
      snapshot: mocks.routingSnapshot,
      previewBatch: mocks.previewBatch,
      save: mocks.saveRules,
    },
  },
  withServiceTimeout: <T,>(request: Promise<T>) => request,
}));

vi.mock("../i18n/i18n", () => ({
  useI18n: () => ({ locale: "en" }),
}));

const NotificationTestProvider = ({ children }: PropsWithChildren) => (
  // Keep menu/dialog portals in the same Fluent context used by App.tsx.
  <FluentProvider theme={webLightTheme}>
    <AppNotificationProvider>{children}</AppNotificationProvider>
  </FluentProvider>
);

const renderPage = (ui: ReactElement) => render(ui, { wrapper: NotificationTestProvider });

const readyDialogAction = (dialog: HTMLElement, name: string) => waitFor(() => {
  // Opening a menu item also closes its popup. Wait for the dialog to own
  // focus, not just for the asynchronous rule preview to enable its button.
  expect(dialog.contains(document.activeElement)).toBe(true);
  expect(dialog.getAttribute("aria-hidden")).not.toBe("true");
  const button = within(dialog).getByRole("button", { name });
  expect(button.hasAttribute("disabled")).toBe(false);
  return button;
});

const connection = (overrides: Partial<ConnectionView>): ConnectionView => ({
  id: 1,
  process: "Zulu.exe",
  protocol: "tcp",
  client: "127.0.0.1:51001",
  target: "ethernet.example:443",
  domain: "ethernet.example",
  remote_ip: "203.0.113.10",
  remote_port: "443",
  adapter: "Ethernet",
  outbound: "aggregation",
  outbound_detail: "Ethernet",
  started_at: "2026-08-24T01:00:00Z",
  bytes_up: 100,
  bytes_down: 300,
  ...overrides,
});

const connections: ConnectionView[] = [
  connection({}),
  connection({
    id: 2,
    process: "Alpha.exe",
    target: "wifi.example:443",
    domain: "wifi.example",
    remote_ip: "203.0.113.20",
    adapter: "Wi-Fi",
    outbound_detail: "Wi-Fi",
    started_at: "2026-08-24T01:02:00Z",
    bytes_up: 50,
    bytes_down: 150,
  }),
];

const snapshot: ConnectionListSnapshot = {
  phase: "running",
  sampled_at: "2026-08-24T01:03:00Z",
  connections,
};

const runtimeAdapter = (
  name: string,
  ifIndex: number,
  downloadBPS: number,
  uploadBPS: number,
): HomeAdapter => ({
  id: name,
  name,
  description: `${name} test adapter`,
  address: `192.0.2.${ifIndex}`,
  prefix_length: 24,
  if_index: ifIndex,
  dns_servers: ["192.0.2.1"],
  metric: 25,
  automatic_metric: true,
  selected: true,
  weight: 1,
  kind: name === "Wi-Fi" ? "wifi" : "ethernet",
  operational: true,
  downloadBPS,
  uploadBPS,
  connections: 1,
  bytesDown: 0,
  bytesUp: 0,
  health: "healthy",
});

const adapterRuntime = [
  runtimeAdapter("Ethernet", 7, 400, 100),
  runtimeAdapter("Wi-Fi", 8, 200, 50),
];

describe("ConnectionsPage interactions", () => {
  beforeEach(() => {
    // jsdom has no layout: body bounds are zero and offsetParent is always
    // null. Tabster therefore treats the whole document as hidden and can
    // never activate a dialog through its first focusable button. Model a
    // visible viewport while preserving hidden/detached element semantics.
    vi.spyOn(document.body, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 1280, 800));
    vi.spyOn(HTMLElement.prototype, "offsetParent", "get").mockImplementation(function (this: HTMLElement) {
      if (!this.isConnected || this === document.body || getComputedStyle(this).position === "fixed") return null;
      for (let element: HTMLElement | null = this; element; element = element.parentElement) {
        if (element.hidden || getComputedStyle(element).display === "none") return null;
      }
      return this.parentElement;
    });
    mocks.connections.mockResolvedValue(snapshot);
    mocks.routingSnapshot.mockResolvedValue({
      rules: [{ match_type: "process", value: "Existing.exe", outbound: "direct" }],
      outbounds: [
        { id: "aggregation", label: "Aggregated" },
        { id: "direct", label: "Direct / bypass" },
        { id: "nic_Ethernet", label: "Ethernet" },
      ],
    });
    mocks.previewBatch.mockResolvedValue({
      items: [{
        input: "ethernet.example",
        status: "add",
        rule: { match_type: "domain", value: "ethernet.example", outbound: "aggregation" },
      }],
      add_count: 1,
      duplicate_count: 0,
      conflict_count: 0,
      invalid_count: 0,
    });
    mocks.saveRules.mockResolvedValue({ rules: [], outbounds: [] });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it("shows only the selected adapter with its shared live throughput", async () => {
    renderPage(<ConnectionsPage initialAdapter="Ethernet" adapterRuntime={adapterRuntime} />);

    expect(await screen.findByText("ethernet.example")).not.toBeNull();
    expect(screen.queryByText("wifi.example")).toBeNull();
    expect(screen.getByText("1 connection(s)")).not.toBeNull();
    expect(screen.getByText("400 B/s")).not.toBeNull();
    expect(screen.getByText("100 B/s")).not.toBeNull();
  });

  it("shows and clears the adapter filter without clearing search", async () => {
    renderPage(<ConnectionsPage initialAdapter="Ethernet" adapterRuntime={adapterRuntime} />);
    await screen.findByText("ethernet.example");

    const adapterFilter = screen.getByRole("group", { name: "Adapter filter" });
    expect(within(adapterFilter).getByText("Ethernet").getAttribute("title")).toBe("Ethernet");
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "wifi" } });
    expect(screen.queryByText("wifi.example")).toBeNull();

    fireEvent.click(within(adapterFilter).getByRole("button", { name: "Clear adapter filter Ethernet" }));

    expect(await screen.findByText("wifi.example")).not.toBeNull();
    expect((screen.getByRole("searchbox") as HTMLInputElement).value).toBe("wifi");
    expect(screen.queryByRole("group", { name: "Adapter filter" })).toBeNull();
    expect(screen.queryByText("1 connection(s)")).toBeNull();
  });

  it("preserves the egress policy when the adapter filter is cleared", async () => {
    renderPage(<ConnectionsPage initialAdapter="Ethernet" adapterRuntime={adapterRuntime} />);
    await screen.findByText("ethernet.example");

    const egressFilter = screen.getByRole("combobox", { name: "Filter by egress policy" }) as HTMLButtonElement;
    fireEvent.click(egressFilter);
    fireEvent.click(await screen.findByRole("option", { name: "Aggregated" }));
    expect(egressFilter.value).toBe("Aggregated");

    const adapterFilter = screen.getByRole("group", { name: "Adapter filter" });
    fireEvent.click(within(adapterFilter).getByRole("button", { name: "Clear adapter filter Ethernet" }));

    expect(egressFilter.value).toBe("Aggregated");
    expect(await screen.findByText("wifi.example")).not.toBeNull();
  });

  it("clears the search query when adapter navigation advances", async () => {
    const view = renderPage(
      <ConnectionsPage initialAdapter="" adapterRevision={1} adapterRuntime={adapterRuntime} />,
    );
    await screen.findByText("ethernet.example");

    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "wifi" } });
    expect(screen.queryByText("ethernet.example")).toBeNull();
    expect(screen.getByText("wifi.example")).not.toBeNull();

    view.rerender(
      <ConnectionsPage initialAdapter="" adapterRevision={2} adapterRuntime={adapterRuntime} />,
    );

    await waitFor(() => {
      expect(screen.getByText("ethernet.example")).not.toBeNull();
      expect(screen.getByText("wifi.example")).not.toBeNull();
    });
  });

  it("reorders visible rows when a sortable column is clicked", async () => {
    renderPage(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    await screen.findByText("ethernet.example");

    fireEvent.click(screen.getByRole("button", { name: "Sort Process ascending" }));

    const rows = screen.getAllByRole("article");
    expect(within(rows[0]).getByText("Alpha.exe")).not.toBeNull();
    expect(within(rows[1]).getByText("Zulu.exe")).not.toBeNull();
  });

  it("uses natural defaults and exposes the active sort state", async () => {
    renderPage(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    await screen.findByText("ethernet.example");

    const trafficSort = screen.getByRole("button", { name: "Sort Traffic descending" });
    fireEvent.click(trafficSort);

    expect(trafficSort.closest('[role="columnheader"]')?.getAttribute("aria-sort")).toBe("descending");
    expect(screen.getByRole("status").textContent).toContain("Traffic · Descending");

    fireEvent.click(screen.getByRole("button", { name: "Sort Traffic ascending" }));

    const rows = screen.getAllByRole("article");
    expect(within(rows[0]).getByText("Alpha.exe")).not.toBeNull();
    expect(within(rows[1]).getByText("Zulu.exe")).not.toBeNull();
    expect(screen.getByRole("status").textContent).toContain("Traffic · Ascending");
  });

  it("only offers single-NIC routing when such connections exist", async () => {
    renderPage(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    await screen.findByText("ethernet.example");

    fireEvent.click(screen.getByRole("combobox", { name: "Filter by egress policy" }));
    expect(screen.queryByRole("option", { name: "Single-NIC routing" })).toBeNull();

    cleanup();
    mocks.connections.mockResolvedValue({
      ...snapshot,
      connections: [
        ...connections,
        connection({
          id: 3,
          process: "Pinned.exe",
          outbound: "adapter",
          outbound_detail: "Ethernet",
        }),
      ],
    });
    renderPage(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    await screen.findByText("Pinned.exe");

    fireEvent.click(screen.getByRole("combobox", { name: "Filter by egress policy" }));
    expect(await screen.findByRole("option", { name: "Single-NIC routing" })).not.toBeNull();
  });

  it("quick-adds a domain rule from a connection context menu without dropping existing rules", async () => {
    renderPage(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    const row = (await screen.findByText("Zulu.exe")).closest("article");
    expect(row).not.toBeNull();

    fireEvent.contextMenu(row!);
    expect(await screen.findByRole("menuitem", { name: /Add by process/ })).not.toBeNull();
    expect(screen.getByRole("menuitem", { name: /Add by domain/ })).not.toBeNull();
    expect(screen.getByRole("menuitem", { name: /Add by ip/i })).not.toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Quick add routing rule" })).toBeNull();
    expect(screen.getByText("Quick add routing rule")).not.toBeNull();

    fireEvent.click(screen.getByRole("menuitem", { name: /Add by domain/ }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("ethernet.example")).not.toBeNull();
    fireEvent.click(await readyDialogAction(dialog, "Add rule"));

    await waitFor(() => expect(mocks.saveRules).toHaveBeenCalledWith([
      { match_type: "process", value: "Existing.exe", outbound: "direct" },
      { match_type: "domain", value: "ethernet.example", outbound: "aggregation" },
    ]));
  });

  it("updates only the exact conflicting rule selected from a connection", async () => {
    const existingRules = [
      { match_type: "process", value: "Existing.exe", outbound: "direct" },
      { match_type: "domain", value: "ethernet.example", outbound: "direct" },
      { match_type: "domain", value: "other.example", outbound: "direct" },
    ];
    mocks.routingSnapshot.mockResolvedValue({
      rules: existingRules,
      outbounds: [
        { id: "aggregation", label: "Aggregated" },
        { id: "direct", label: "Direct / bypass" },
      ],
    });
    mocks.previewBatch.mockResolvedValue({
      items: [{
        input: "ethernet.example",
        status: "conflict",
        rule: { match_type: "domain", value: "ethernet.example", outbound: "aggregation" },
        existing_outbound: "direct",
      }],
      add_count: 0,
      duplicate_count: 0,
      conflict_count: 1,
      invalid_count: 0,
    });

    renderPage(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    const row = (await screen.findByText("Zulu.exe")).closest("article");
    fireEvent.contextMenu(row!);
    fireEvent.click(await screen.findByRole("menuitem", { name: /Add by domain/ }));

    const dialog = await screen.findByRole("dialog");
    const updateButton = await readyDialogAction(dialog, "Update rule");
    fireEvent.click(updateButton);

    await waitFor(() => expect(mocks.saveRules).toHaveBeenCalledWith([
      existingRules[0],
      existingRules[2],
      { match_type: "domain", value: "ethernet.example", outbound: "aggregation" },
    ]));
  });

  it("derives only usable identities and preserves the current single-NIC egress", () => {
    expect(connectionRuleCandidates(connection({ process: "", domain: "", remote_ip: "2001:db8::8" }))).toEqual([
      { matchType: "ip", value: "2001:db8::8" },
    ]);
    expect(preferredConnectionRuleOutbound(
      connection({ outbound: "adapter", outbound_detail: "Ethernet" }),
      [
        { id: "aggregation", label: "Aggregated" },
        { id: "nic_Ethernet", label: "Ethernet" },
      ],
    )).toBe("nic_Ethernet");
  });

  it("keeps quick-rule dialogs accessible after repeated context-menu transitions", async () => {
    renderPage(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    const row = (await screen.findByText("Zulu.exe")).closest("article")!;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      fireEvent.contextMenu(row);
      fireEvent.click(await screen.findByRole("menuitem", { name: /Add by domain/ }));
      const dialog = await screen.findByRole("dialog");
      await readyDialogAction(dialog, "Add rule");
      fireEvent.click(await readyDialogAction(dialog, "Cancel"));
      // The exiting surface must keep its contents and dimensions until unmount.
      expect(within(dialog).getByText("ethernet.example")).not.toBeNull();
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    }
    expect(mocks.saveRules).not.toHaveBeenCalled();
  });
});
