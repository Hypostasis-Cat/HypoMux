import { describe, expect, it } from "vitest";
import { routingApplyState } from "./routingEffect";

describe("routingApplyState", () => {
  it("reports a completed hot reload for a running TUN engine", () => {
    expect(routingApplyState({ restart_required: false }, "running", "tun")).toBe("hot_reloaded");
  });

  it("preserves a restart requirement for a degraded TUN engine", () => {
    expect(routingApplyState({ restart_required: true }, "degraded", "tun")).toBe("restart_required");
  });

  it("defers rules until the next TUN start when the engine is stopped", () => {
    expect(routingApplyState({ restart_required: true }, "stopped", "tun")).toBe("next_tun_start");
  });

  it("marks routing rules inactive in system proxy mode", () => {
    expect(routingApplyState({ restart_required: false }, "running", "proxy")).toBe("inactive_mode");
  });
});
