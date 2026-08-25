// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { HomeAdapter } from "../../state/useEngineState";
import { NetworkAdapterItem } from "./NetworkAdapterItem";

vi.mock("../../i18n/i18n", () => ({
  useI18n: () => ({ locale: "en", t: (key: string) => key }),
}));

const adapter: HomeAdapter = {
  id: "ethernet",
  name: "Ethernet",
  description: "Test adapter",
  address: "192.0.2.10",
  prefix_length: 24,
  if_index: 7,
  dns_servers: ["192.0.2.1"],
  metric: 25,
  automatic_metric: true,
  selected: true,
  weight: 1,
  kind: "ethernet",
  operational: true,
  downloadBPS: 2048,
  uploadBPS: 1024,
  connections: 3,
  bytesDown: 0,
  bytesUp: 0,
  health: "healthy",
};

const renderAdapter = (disabled = false) => {
  const onOpenConnections = vi.fn();
  const onSelectedChange = vi.fn();
  render(
    <NetworkAdapterItem
      adapter={adapter}
      percentage={100}
      disabled={disabled}
      onOpenConnections={onOpenConnections}
      onSelectedChange={onSelectedChange}
      onWeightChange={vi.fn()}
    />,
  );
  return { onOpenConnections, onSelectedChange };
};

describe("NetworkAdapterItem interactions", () => {
  afterEach(cleanup);

  it("uses the card for selection and the live metrics only for navigation", () => {
    const { onOpenConnections, onSelectedChange } = renderAdapter();

    fireEvent.click(screen.getByText("Ethernet"));
    expect(onSelectedChange).toHaveBeenCalledWith(false);
    expect(onOpenConnections).not.toHaveBeenCalled();

    onSelectedChange.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "View active connections for Ethernet" }));
    expect(onOpenConnections).toHaveBeenCalledOnce();
    expect(onSelectedChange).not.toHaveBeenCalled();
  });

  it("keeps live metrics navigable while selection is locked", () => {
    const { onOpenConnections, onSelectedChange } = renderAdapter(true);

    fireEvent.click(screen.getByRole("article"));
    expect(onSelectedChange).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "View active connections for Ethernet" }));
    expect(onOpenConnections).toHaveBeenCalledOnce();
  });
});
