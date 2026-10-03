import { describe, expect, it } from "vitest";
import type { AdapterView } from "../platform/services";
import { adapterListKey } from "./adapterRuntime";

describe("IPv6 adapter refresh", () => {
  const adapter = { id: "dual", name: "dual", address: "192.0.2.1", source_ipv6: "2001:db8::1", ipv6_if_index: 7 } as AdapterView;
  it("refreshes when only the IPv6 address or binding changes", () => {
    const original = adapterListKey([adapter]);
    expect(adapterListKey([{ ...adapter, source_ipv6: "2001:db8::2" }])).not.toBe(original);
    expect(adapterListKey([{ ...adapter, ipv6_if_index: 8 }])).not.toBe(original);
    expect(adapterListKey([{ ...adapter, ipv6_gateway: "fe80::1%7" }])).not.toBe(original);
    expect(adapterListKey([{ ...adapter, ipv6_metric: 25 }])).not.toBe(original);
  });
});
