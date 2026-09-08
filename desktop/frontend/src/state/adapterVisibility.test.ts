import { describe, expect, it } from "vitest";
import type { AdapterView } from "../platform/services";
import { visibleHomeAdapters, selectVisibleAdapters } from "./adapterVisibility";

const adapters = [
  { id: "ethernet", selected: false, weight: 2 },
  { id: "vmware", is_virtual: true, selected: true, weight: 3 },
  { id: "hyperv", is_virtual: true, selected: false, weight: 1 },
] as AdapterView[];

describe("Home virtual adapter visibility", () => {
  it("hides virtual cards without losing the full list or saved selection", () => {
    expect(visibleHomeAdapters(adapters, true).map((a) => a.id)).toEqual(["ethernet"]);
    expect(visibleHomeAdapters(adapters, false)).toEqual(adapters);
    expect(adapters[1].selected).toBe(true);
    expect(visibleHomeAdapters(adapters.slice(1), true)).toEqual([]);
  });
  it.each([true, false])("bulk selection %s preserves hidden selections and weights", (selected) => {
    const result = selectVisibleAdapters(adapters, true, selected);
    expect(result[0].selected).toBe(selected);
    expect(result.slice(1)).toEqual(adapters.slice(1));
    expect(result.map((a) => a.weight)).toEqual([2, 3, 1]);
    expect(selectVisibleAdapters(adapters, false, selected).every((a) => a.selected === selected)).toBe(true);
  });
});
