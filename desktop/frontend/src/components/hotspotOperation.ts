import { useSyncExternalStore } from "react";

type Operation = "start" | "stop" | "save" | undefined;
let operation: Operation;
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
export const useHotspotOperation = () => useSyncExternalStore(subscribe, () => operation);

// A page unmount must not release an in-flight operation. Both toolkit controls
// share this lock and remain disabled until the actual backend call finishes.
export async function runHotspotOperation<T>(next: Exclude<Operation, undefined>, work: () => Promise<T>): Promise<T> {
  if (operation) throw new Error("热点操作尚未完成，请稍候。");
  operation = next; listeners.forEach(listener => listener());
  try { return await work(); }
  finally { operation = undefined; listeners.forEach(listener => listener()); }
}
