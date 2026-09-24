import { describe, expect, it } from "vitest";
import { groupConnectionsByAdapter, groupConnectionsByProcess } from "./connectionGroups";
import type { ConnectionView } from "../platform/services";

describe("groupConnectionsByProcess", () => {
  it("combines case-insensitive names, sums traffic and sorts by group totals without changing telemetry", () => {
    const flow = (id: number, process?: string): ConnectionView => ({ id, process, protocol: "tcp", outbound: "direct", started_at: `2026-09-24T01:00:0${id}Z`, bytes_up: 100, bytes_down: 200 });
    const flows = [flow(1, "cs2.exe"), flow(2, "browser.exe"), flow(3, " CS2.EXE "), flow(4), flow(5)];
    const groups = groupConnectionsByProcess(flows, { key: "traffic", direction: "descending" });
    expect(groups).toHaveLength(4);
    expect(groups[0].connections.map((item) => item.id)).toEqual([1, 3]);
    expect(groups[0].summary).toMatchObject({ bytes_up: 200, bytes_down: 400, started_at: flows[0].started_at });
    expect(flows[0].bytes_down).toBe(200);
    expect(groups.slice(2).map((group) => group.connections.length)).toEqual([1, 1]);
  });
});

describe("groupConnectionsByAdapter", () => {
  it("groups connections using the shared home adapter throughput", () => {
    const groups = groupConnectionsByAdapter(
      [
        { id: 1, adapter: "Ethernet" },
        { id: 2, adapter: "Wi-Fi" },
        { id: 3, adapter: "Ethernet" },
      ],
      [
        { name: "Ethernet", downloadBPS: 100, uploadBPS: 50 },
        { name: "Wi-Fi", downloadBPS: 600, uploadBPS: 50 },
      ],
    );

    expect(groups.map((group) => ({
      adapter: group.adapter,
      downloadBPS: group.downloadBPS,
      uploadBPS: group.uploadBPS,
      connectionIDs: group.connections.map((connection) => connection.id),
    }))).toEqual([
      { adapter: "Wi-Fi", downloadBPS: 600, uploadBPS: 50, connectionIDs: [2] },
      { adapter: "Ethernet", downloadBPS: 100, uploadBPS: 50, connectionIDs: [1, 3] },
    ]);
  });
});
