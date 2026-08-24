import { describe, expect, it } from "vitest";
import type { ConnectionView } from "../platform/services";
import { selectConnections } from "./connectionView";

const connection = (overrides: Partial<ConnectionView>): ConnectionView => ({
  id: 1,
  process: "browser.exe",
  protocol: "tcp",
  domain: "example.com",
  remote_ip: "203.0.113.10",
  remote_port: "443",
  adapter: "WLAN",
  outbound: "aggregation",
  outbound_detail: "WLAN",
  started_at: "2026-08-24T04:00:00Z",
  bytes_up: 0,
  bytes_down: 0,
  ...overrides,
});

describe("selectConnections", () => {
  const connections = [
    connection({ id: 1, process: "old.exe", outbound: "aggregation", started_at: "2026-08-24T04:00:00Z" }),
    connection({ id: 2, process: "new.exe", outbound: "adapter", outbound_detail: "热点", started_at: "2026-08-24T04:02:00Z" }),
    connection({ id: 3, process: "direct.exe", outbound: "direct", domain: "direct.example", started_at: "2026-08-24T04:01:00Z" }),
  ];

  it("filters by outbound policy and searchable egress detail", () => {
    expect(selectConnections(connections, "", "adapter", "longest").map((item) => item.id)).toEqual([2]);
    expect(selectConnections(connections, "热点", "all", "longest").map((item) => item.id)).toEqual([2]);
  });

  it("sorts active connections by longest or shortest duration", () => {
    expect(selectConnections(connections, "", "all", "longest").map((item) => item.id)).toEqual([1, 3, 2]);
    expect(selectConnections(connections, "", "all", "shortest").map((item) => item.id)).toEqual([2, 3, 1]);
  });

  it("does not mutate the service snapshot order", () => {
    const original = connections.map((item) => item.id);
    selectConnections(connections, "", "all", "shortest");
    expect(connections.map((item) => item.id)).toEqual(original);
  });
});
