import type { AdapterView } from "./services";
import { appServices } from "./services";
import { LatestSaveQueue } from "./latestSaveQueue";

export type AdapterSaveInput = {
  mode: string;
  weighted: boolean;
  strategy?: string;
  adapters: AdapterView[];
};

type AdapterMutation = AdapterSaveInput | { selectedIDs: string[] };

export const adapterSaveQueue = new LatestSaveQueue<AdapterMutation, AdapterView[] | null>(
  input => "selectedIDs" in input ? appServices.adapters.saveSelected(input.selectedIDs)
    : appServices.adapters.save(input.mode, input.weighted, input.adapters, input.strategy),
  (previous, next) => "selectedIDs" in next && !("selectedIDs" in previous)
    ? { ...previous, adapters: previous.adapters.map(adapter => ({ ...adapter, selected: next.selectedIDs.includes(adapter.id) })) }
    : next,
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
