export type ConnectionsNavigation = {
  adapter: string;
  revision: number;
};

export const advanceConnectionsNavigation = (
  current: ConnectionsNavigation,
  adapterName?: string,
): ConnectionsNavigation => adapterName === undefined ? current : ({
  adapter: adapterName,
  revision: current.revision + 1,
});
