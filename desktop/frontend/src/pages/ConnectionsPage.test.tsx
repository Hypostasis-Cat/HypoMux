// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ConnectionListSnapshot, ConnectionView } from "../platform/services";
import type { HomeAdapter } from "../state/useEngineState";
import { ConnectionsPage } from "./ConnectionsPage";

const mocks = vi.hoisted(() => ({
  connections: vi.fn(),
}));

vi.mock("../platform/services", () => ({
  appServices: { engine: { connections: mocks.connections } },
  withServiceTimeout: <T,>(request: Promise<T>) => request,
}));

vi.mock("../i18n/i18n", () => ({
  useI18n: () => ({ locale: "en" }),
}));

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
    mocks.connections.mockResolvedValue(snapshot);
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows only the selected adapter with its shared live throughput", async () => {
    render(<ConnectionsPage initialAdapter="Ethernet" adapterRuntime={adapterRuntime} />);

    expect(await screen.findByText("ethernet.example")).not.toBeNull();
    expect(screen.queryByText("wifi.example")).toBeNull();
    expect(screen.getByText("1 connection(s)")).not.toBeNull();
    expect(screen.getByText("400 B/s")).not.toBeNull();
    expect(screen.getByText("100 B/s")).not.toBeNull();
  });

  it("shows and clears the adapter filter without clearing search", async () => {
    render(<ConnectionsPage initialAdapter="Ethernet" adapterRuntime={adapterRuntime} />);
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
    render(<ConnectionsPage initialAdapter="Ethernet" adapterRuntime={adapterRuntime} />);
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
    const view = render(
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
    render(<ConnectionsPage adapterRuntime={adapterRuntime} />);
    await screen.findByText("ethernet.example");

    fireEvent.click(screen.getByRole("button", { name: "Sort Process ascending" }));

    const rows = screen.getAllByRole("article");
    expect(within(rows[0]).getByText("Alpha.exe")).not.toBeNull();
    expect(within(rows[1]).getByText("Zulu.exe")).not.toBeNull();
  });

  it("uses natural defaults and exposes the active sort state", async () => {
    render(<ConnectionsPage adapterRuntime={adapterRuntime} />);
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
});
