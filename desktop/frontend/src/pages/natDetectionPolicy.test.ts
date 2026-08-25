import { describe, expect, it } from "vitest";
import { isNATDetectionBlocked } from "./natDetectionPolicy";

describe("NAT detection availability", () => {
  it.each(["starting", "running", "degraded", "stopping"] as const)(
    "blocks direct-path detection while the engine is %s",
    (phase) => expect(isNATDetectionBlocked(phase)).toBe(true),
  );

  it.each([undefined, "stopped", "failed"] as const)(
    "allows detection when the engine phase is %s",
    (phase) => expect(isNATDetectionBlocked(phase)).toBe(false),
  );
});
