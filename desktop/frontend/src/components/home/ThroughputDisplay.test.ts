import { describe, expect, it } from "vitest";
import { createThroughputChart } from "./ThroughputDisplay";

describe("throughput chart geometry", () => {
  it("builds a smooth cubic path and a closed ambient area", () => {
    const chart = createThroughputChart([0, 1, 4, 2, 6, 5]);

    expect(chart.linePath).toMatch(/^M /);
    expect(chart.linePath).toContain(" C ");
    expect(chart.areaPath).toBe(`${chart.linePath} L 100 44 L 0 44 Z`);
  });

  it("keeps invalid and sparse telemetry inside valid SVG geometry", () => {
    for (const values of [[], [3], [Number.NaN, -4, Number.POSITIVE_INFINITY, 2]]) {
      const chart = createThroughputChart(values);

      expect(chart.linePath).not.toMatch(/NaN|Infinity/);
      expect(chart.areaPath).not.toMatch(/NaN|Infinity/);
    }
  });
});
