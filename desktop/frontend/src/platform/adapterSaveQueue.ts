import type { AdapterView } from "./services";
import { appServices } from "./services";
import { LatestSaveQueue } from "./latestSaveQueue";

export type AdapterSaveInput = {
  mode: string;
  weighted: boolean;
  adapters: AdapterView[];
};

export const adapterSaveQueue = new LatestSaveQueue<AdapterSaveInput, AdapterView[] | null>(
  ({ mode, weighted, adapters }) => appServices.adapters.save(mode, weighted, adapters),
);

export const adapterSaveInput = (
  mode: string,
  weighted: boolean,
  adapters: AdapterView[],
): AdapterSaveInput => ({
  mode,
  weighted,
  adapters: adapters.map((adapter) => ({ ...adapter })),
});
