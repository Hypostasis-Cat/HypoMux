import type { AdapterView } from "./services";
import { appServices } from "./services";
import { LatestSaveQueue } from "./latestSaveQueue";

export type AdapterSaveInput = {
  mode: string;
  weighted: boolean;
  strategy?: string;
  adapters: AdapterView[];
};

export const adapterSaveQueue = new LatestSaveQueue<AdapterSaveInput, AdapterView[] | null>(
  ({ mode, weighted, adapters, strategy }) => appServices.adapters.save(mode, weighted, adapters, strategy),
);

export const adapterSaveInput = (
  mode: string,
  weighted: boolean,
  adapters: AdapterView[],
  strategy?: string,
): AdapterSaveInput => ({
  mode,
  weighted,
  strategy,
  adapters: adapters.map((adapter) => ({ ...adapter })),
});
