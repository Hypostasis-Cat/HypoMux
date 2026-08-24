type AdapterConnection = {
  adapter?: string;
};

type AdapterThroughput = {
  id: string;
  download_bps: number;
  upload_bps: number;
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
  const throughput = new Map(adapters.map((adapter) => [adapter.id, adapter]));
  const groups = new Map<string, AdapterConnectionGroup<T>>();
  for (const connection of connections) {
    const adapter = connection.adapter?.trim() ?? "";
    let group = groups.get(adapter);
    if (!group) {
      const speed = throughput.get(adapter);
      group = {
        adapter,
        downloadBPS: speed?.download_bps ?? 0,
        uploadBPS: speed?.upload_bps ?? 0,
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
