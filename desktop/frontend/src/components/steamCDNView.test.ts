import { describe, expect, it } from "vitest";
import { steamNodeState, type SteamNode } from "./steamCDNView";

describe("Steam node eligibility presentation", () => {
  const node = { validated: true, preferred: true, expires_at: "2099-01-01T00:00:00Z", cooldown_until: "0001-01-01T00:00:00Z" } as SteamNode;
  it("does not present expired or paused nodes as preferred", () => {
    expect(steamNodeState({ ...node, expires_at: "2020-01-01T00:00:00Z" })).toBe("expired");
    expect(steamNodeState({ ...node, decision_reason: "route_paused" })).toBe("paused");
    expect(steamNodeState({ ...node, cooldown_until: "2099-01-01T00:00:00Z" })).toBe("cooldown");
    expect(steamNodeState({ ...node, validated: false, source: "dns" })).toBe("unavailable");
  });
  it("distinguishes a trial connection from a verified waiting candidate", () => {
    expect(steamNodeState({ ...node, preferred: false, switched_active: 1 })).toBe("trial");
    expect(steamNodeState({ ...node, preferred: false, switched_active: 0 })).toBe("verified");
  });
});
