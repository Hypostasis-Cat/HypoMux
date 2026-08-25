import { describe, expect, it } from "vitest";
import type { ConnectionView } from "../platform/services";
import { sortConnections, type ConnectionSortKey } from "./connectionSort";

const connections: ConnectionView[] = [
  {
    id: 1,
    process: "Zulu.exe",
    protocol: "tcp",
    client: "127.0.0.1:51001",
    target: "z.example:443",
    domain: "z.example",
    remote_ip: "203.0.113.30",
    remote_port: "443",
    adapter: "Wi-Fi",
    outbound: "adapter",
    outbound_detail: "Wi-Fi",
    started_at: "2026-08-24T01:00:00Z",
    bytes_up: 100,
    bytes_down: 300,
  },
  {
    id: 2,
    process: "Alpha.exe",
    protocol: "udp",
    client: "127.0.0.1:51002",
    target: "a.example:53",
    domain: "a.example",
    remote_ip: "203.0.113.10",
    remote_port: "53",
    adapter: "Ethernet",
    outbound: "direct",
    outbound_detail: "",
    started_at: "2026-08-24T01:02:00Z",
    bytes_up: 25,
    bytes_down: 25,
  },
  {
    id: 3,
    process: "Mike.exe",
    protocol: "tcp",
    client: "127.0.0.1:51003",
    target: "m.example:80",
    domain: "m.example",
    remote_ip: "203.0.113.20",
    remote_port: "80",
    adapter: "Ethernet",
    outbound: "aggregation",
    outbound_detail: "",
    started_at: "2026-08-24T01:01:00Z",
    bytes_up: 200,
    bytes_down: 100,
  },
];

describe("sortConnections", () => {
  it.each<[ConnectionSortKey, number[], number[]]>([
    ["process", [2, 3, 1], [1, 3, 2]],
    ["destination", [2, 3, 1], [1, 3, 2]],
    ["policy", [1, 3, 2], [2, 3, 1]],
    ["traffic", [2, 3, 1], [1, 3, 2]],
    ["duration", [2, 3, 1], [1, 3, 2]],
  ])("sorts the %s column in both directions", (key, ascendingIDs, descendingIDs) => {
    expect(sortConnections(connections, { key, direction: "ascending" }).map(({ id }) => id))
      .toEqual(ascendingIDs);
    expect(sortConnections(connections, { key, direction: "descending" }).map(({ id }) => id))
      .toEqual(descendingIDs);
  });

  it("does not mutate the source list", () => {
    sortConnections(connections, { key: "traffic", direction: "descending" });
    expect(connections.map(({ id }) => id)).toEqual([1, 2, 3]);
  });
});
