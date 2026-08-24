import type { ConnectionView } from "../platform/services";

export type ConnectionSortKey = "process" | "destination" | "policy" | "traffic" | "duration";

export type ConnectionSort = {
  key: ConnectionSortKey;
  direction: "ascending" | "descending";
};

export const sortConnections = (
  connections: readonly ConnectionView[],
  sort: ConnectionSort | null,
): readonly ConnectionView[] => {
  if (!sort) return connections;

  const value = (connection: ConnectionView): string | number => {
    switch (sort.key) {
      case "process":
        return connection.process || connection.domain || connection.remote_ip || connection.target || "";
      case "destination":
        return connection.domain || connection.remote_ip || connection.target || "";
      case "policy":
        return [connection.outbound, connection.outbound_detail, connection.adapter].filter(Boolean).join(" ");
      case "traffic":
        return connection.bytes_up + connection.bytes_down;
      case "duration": {
        const startedAt = Date.parse(connection.started_at);
        return Number.isFinite(startedAt) ? -startedAt : Number.MAX_SAFE_INTEGER;
      }
    }
  };

  const direction = sort.direction === "ascending" ? 1 : -1;
  return [...connections].sort((left, right) => {
    const leftValue = value(left);
    const rightValue = value(right);
    const comparison = typeof leftValue === "number" && typeof rightValue === "number"
      ? leftValue - rightValue
      : String(leftValue).localeCompare(String(rightValue), undefined, { numeric: true, sensitivity: "base" });
    return comparison * direction;
  });
};
