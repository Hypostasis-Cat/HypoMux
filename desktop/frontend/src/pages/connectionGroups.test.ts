import { describe, expect, it } from "vitest";
import { groupConnectionsByAdapter } from "./connectionGroups";

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
