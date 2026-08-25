// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AdapterView } from "../platform/services";
import { NATDetectionPage } from "./NATDetectionPage";

const adapter: AdapterView = {
  id: "ethernet",
  name: "Ethernet",
  description: "Test adapter",
  address: "192.0.2.10",
  prefix_length: 24,
  if_index: 7,
  gateway: "192.0.2.1",
  dns_servers: ["192.0.2.1"],
  metric: 25,
  automatic_metric: true,
  selected: true,
  weight: 1,
  kind: "ethernet",
  operational: true,
};

afterEach(cleanup);

describe("NAT detection aggregation state", () => {
  it("uses the blocked two-row control layout and disables detection", async () => {
    const { container } = render(
      <NATDetectionPage
        adapters={[adapter]}
        enginePhase="running"
        loading={false}
        preview
        text={(zh) => zh}
        notify={vi.fn()}
      />,
    );

    expect(container.querySelector(".nat-control")?.classList.contains("is-engine-blocked")).toBe(true);
    expect(container.querySelector<HTMLButtonElement>(".nat-control-actions button")?.disabled).toBe(true);
    expect(screen.getByRole("status").textContent).toContain("请先停止聚合再检测");
  });
});
