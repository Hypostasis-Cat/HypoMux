import { useSyncExternalStore } from "react";

type Operation = "start" | "stop" | "save" | undefined;
let operation: Operation;
const running: Exclude<Operation, undefined>[] = [];
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
const publish = () => { operation = running[running.length - 1]; listeners.forEach(listener => listener()); };
export const useHotspotOperation = () => useSyncExternalStore(subscribe, () => operation);

// The backend cancels an in-flight start when stop is requested.
export const isHotspotStartCancelable = () => running.length === 1 && running[0] === "start";
export const isHotspotStartCanceled = (reason: unknown) => String(reason).includes("已取消开启热点");

// A page unmount must not release an in-flight operation. Both toolkit controls
// share this lock and remain disabled until the actual backend call finishes;
// the only exception is one stop that cancels a pending start.
export async function runHotspotOperation<T>(next: Exclude<Operation, undefined>, work: () => Promise<T>): Promise<T> {
  if (running.length && !(next === "stop" && isHotspotStartCancelable())) throw new Error("热点操作尚未完成，请稍候。");
  running.push(next); publish();
  try { return await work(); }
  finally { running.splice(running.indexOf(next), 1); publish(); }
}
