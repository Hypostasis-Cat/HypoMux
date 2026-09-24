import type { ConnectionView } from "../platform/services";
import { sortConnections, type ConnectionSort } from "./connectionSort";

export const groupConnectionsByProcess = (connections: readonly ConnectionView[], sort: ConnectionSort) => {
  const groups = new Map<string, { key: string; summary: ConnectionView; connections: ConnectionView[] }>();
  for (const connection of connections) {
    const process = connection.process?.trim();
    // Telemetry exposes a process name, not a PID. Unidentified flows stay separate.
    const key = process ? `process:${process.toLowerCase()}` : `connection:${connection.id}`;
    const group = groups.get(key);
    if (group) {
      group.connections.push(connection);
      group.summary.bytes_up += connection.bytes_up;
      group.summary.bytes_down += connection.bytes_down;
      if (Date.parse(connection.started_at) < Date.parse(group.summary.started_at)) {
        group.summary.started_at = connection.started_at;
      }
    } else {
      groups.set(key, { key, summary: { ...connection, process }, connections: [connection] });
    }
  }
  const bySummary = new Map([...groups.values()].map((group) => [group.summary, group]));
  return sortConnections([...bySummary.keys()], sort).map((summary) => bySummary.get(summary)!);
};

type AdapterConnection = {
  adapter?: string;
};

type AdapterThroughput = {
  name: string;
  downloadBPS: number;
  uploadBPS: number;
};

export type AdapterConnectionGroup<T> = {
  adapter: string;
  downloadBPS: number;
  uploadBPS: number;
  connections: T[];
};

export const groupConnectionsByAdapter = <T extends AdapterConnection>(
  connections: readonly T[],
  adapters: readonly AdapterThroughput[],
): AdapterConnectionGroup<T>[] => {
  const throughput = new Map(adapters.map((adapter) => [adapter.name, adapter]));
  const groups = new Map<string, AdapterConnectionGroup<T>>();
  for (const connection of connections) {
    const adapter = connection.adapter?.trim() ?? "";
    let group = groups.get(adapter);
    if (!group) {
      const speed = throughput.get(adapter);
      group = {
        adapter,
        downloadBPS: speed?.downloadBPS ?? 0,
        uploadBPS: speed?.uploadBPS ?? 0,
        connections: [],
      };
      groups.set(adapter, group);
    }
    group.connections.push(connection);
  }
  return [...groups.values()].sort(
    (left, right) => right.downloadBPS + right.uploadBPS - left.downloadBPS - left.uploadBPS,
  );
};
