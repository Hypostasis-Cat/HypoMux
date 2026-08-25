import type { AdapterView } from "../platform/services";

export const adapterListKey = (items: readonly AdapterView[]) => JSON.stringify(items.map((item) => ({
  id: item.id,
  name: item.name,
  description: item.description,
  address: item.address,
  prefixLength: item.prefix_length,
  ifIndex: item.if_index,
  gateway: item.gateway,
  dnsServers: item.dns_servers,
  metric: item.metric,
  automaticMetric: item.automatic_metric,
  selected: item.selected,
  weight: item.weight,
  kind: item.kind,
  operational: item.operational,
})));
