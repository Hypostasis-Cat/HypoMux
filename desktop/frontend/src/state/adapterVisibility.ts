import type { AdapterView } from "../platform/services";

export const ADAPTER_VISIBILITY_EVENT = "hypomux:adapter-visibility-changed";

export const visibleHomeAdapters = <T extends AdapterView>(adapters: readonly T[], hideVirtual: boolean): T[] =>
  adapters.filter((adapter) => !hideVirtual || !adapter.is_virtual);

export const selectVisibleAdapters = (adapters: readonly AdapterView[], hideVirtual: boolean, selected: boolean): AdapterView[] =>
  adapters.map((adapter) => hideVirtual && adapter.is_virtual ? adapter : { ...adapter, selected });
