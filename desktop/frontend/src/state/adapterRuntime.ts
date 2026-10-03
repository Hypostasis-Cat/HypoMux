import type { AdapterView } from "../platform/services";

export const adapterListKey = (items: readonly AdapterView[]) => JSON.stringify(items.map((item) => ({
  id: item.id,
  name: item.name,
  description: item.description,
  address: item.address,
	 sourceIPv6: item.source_ipv6,
	 ipv6IfIndex: item.ipv6_if_index,
	 ipv6Gateway: item.ipv6_gateway,
	 ipv6Metric: item.ipv6_metric,
	 ipv6AutomaticMetric: item.ipv6_automatic_metric,
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
  isVirtual: item.is_virtual,
})));
