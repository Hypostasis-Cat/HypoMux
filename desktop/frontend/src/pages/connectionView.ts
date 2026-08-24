import type { ConnectionView } from "../platform/services";

export type ConnectionOutboundFilter = "all" | "aggregation" | "direct" | "adapter";
export type ConnectionDurationSort = "longest" | "shortest";

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

const startedAt = (connection: ConnectionView) => {
  const value = new Date(connection.started_at).getTime();
  return Number.isFinite(value) ? value : null;
};

export const selectConnections = (
  connections: ConnectionView[],
  query: string,
  outboundFilter: ConnectionOutboundFilter,
  durationSort: ConnectionDurationSort,
) => {
  const needle = query.trim().toLocaleLowerCase();
  return connections
    .filter((connection) => outboundFilter === "all" || connection.outbound === outboundFilter)
    .filter((connection) => !needle || searchableValues(connection)
      .some((value) => value?.toLocaleLowerCase().includes(needle)))
    .slice()
    .sort((left, right) => {
      const leftStartedAt = startedAt(left);
      const rightStartedAt = startedAt(right);
      if (leftStartedAt === null || rightStartedAt === null) {
        if (leftStartedAt === rightStartedAt) return left.id - right.id;
        return leftStartedAt === null ? 1 : -1;
      }
      const byStartedAt = durationSort === "longest"
        ? leftStartedAt - rightStartedAt
        : rightStartedAt - leftStartedAt;
      return byStartedAt || left.id - right.id;
    });
};
