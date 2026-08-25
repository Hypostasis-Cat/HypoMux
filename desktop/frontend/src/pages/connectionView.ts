import type { ConnectionView } from "../platform/services";
import { sortConnections, type ConnectionSort } from "./connectionSort";

export type ConnectionOutboundFilter = "all" | "aggregation" | "direct" | "adapter";

const searchableValues = (connection: ConnectionView) => [
  connection.process,
  connection.domain,
  connection.remote_ip,
  connection.remote_port,
  connection.target,
  connection.adapter,
  connection.outbound,
  connection.outbound_detail,
  connection.protocol,
];

export const selectConnections = (
  connections: readonly ConnectionView[],
  query: string,
  outboundFilter: ConnectionOutboundFilter,
  adapterFilter: string,
  sort: ConnectionSort | null,
) => {
  const needle = query.trim().toLocaleLowerCase();
  const adapter = adapterFilter.trim();
  const filtered = connections
    .filter((connection) => outboundFilter === "all" || connection.outbound === outboundFilter)
    .filter((connection) => !adapter || connection.adapter === adapter)
    .filter((connection) => !needle || searchableValues(connection)
      .some((value) => value?.toLocaleLowerCase().includes(needle)));
  return sortConnections(filtered, sort);
};
