import { describe, expect, it } from "vitest";
import { advanceConnectionsNavigation } from "./connectionNavigation";

describe("advanceConnectionsNavigation", () => {
  it("keeps the clicked adapter separate from the connections search query", () => {
    expect(advanceConnectionsNavigation({ adapter: "", revision: 2 }, "Ethernet 3"))
      .toEqual({ adapter: "Ethernet 3", revision: 3 });
  });

  it("preserves filters when returning through the sidebar", () => {
    expect(advanceConnectionsNavigation({ adapter: "Wi-Fi", revision: 6 }))
      .toEqual({ adapter: "Wi-Fi", revision: 6 });
  });
  it("can explicitly clear an adapter filter", () => {
    expect(advanceConnectionsNavigation({ adapter: "Wi-Fi", revision: 6 }, ""))
      .toEqual({ adapter: "", revision: 7 });
  });
});
