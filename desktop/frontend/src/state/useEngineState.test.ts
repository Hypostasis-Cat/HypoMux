import { describe, expect, it } from "vitest";
import type { AdapterView } from "../platform/services";
import { adapterListKey } from "./adapterRuntime";
import { HOME_TELEMETRY_POLL_MS, shouldPollEngineSnapshot } from "./useEngineState";

const adapter = (): AdapterView => ({
  id: "Ethernet",
  name: "Ethernet",
  description: "Test adapter",
  address: "192.0.2.10",
  prefix_length: 24,
  if_index: 7,
  dns_servers: ["192.0.2.1"],
  metric: 25,
  automatic_metric: true,
  selected: true,
  weight: 3,
  kind: "ethernet",
  operational: true,
});

describe("adapter live scan comparison", () => {
  it("detects selection and weight changes as well as physical-link changes", () => {
    const current = adapter();

    expect(adapterListKey([{ ...current, selected: false }])).not.toBe(adapterListKey([current]));
    expect(adapterListKey([{ ...current, weight: 8 }])).not.toBe(adapterListKey([current]));
    expect(adapterListKey([{ ...current, address: "192.0.2.11" }])).not.toBe(adapterListKey([current]));
  });

  it("ignores Home-only traffic telemetry when deciding whether configuration changed", () => {
    const current = adapter();
    const withRuntime = {
      ...current,
      downloadBPS: 4096,
      uploadBPS: 1024,
      connections: 8,
      bytesDown: 8192,
      bytesUp: 2048,
      health: "healthy",
    };

    expect(adapterListKey([withRuntime])).toBe(adapterListKey([current]));
  });
});

describe("Home telemetry cadence", () => {
  it("refreshes quickly enough for the throughput display to feel live", () => {
    expect(HOME_TELEMETRY_POLL_MS).toBeLessThanOrEqual(1000);
    expect(HOME_TELEMETRY_POLL_MS).toBeGreaterThanOrEqual(500);
  });

  it("keeps polling an externally-triggered engine transition", () => {
    expect(shouldPollEngineSnapshot(true, false, false, false)).toBe(true);
  });

  it("does not apply polling snapshots over a local engine operation", () => {
    expect(shouldPollEngineSnapshot(true, true, false, false)).toBe(false);
  });
});
