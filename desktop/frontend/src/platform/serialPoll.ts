export type SerialPollOptions = {
  immediate?: boolean;
  isActive?: () => boolean;
};

const pageIsVisible = () => typeof document === "undefined" || document.visibilityState !== "hidden";

/** Runs at most one asynchronous poll at a time. The next delay starts only
 * after the previous task settles, and hidden pages pause remote work. */
export const startSerialPoll = (
  task: () => Promise<void>,
  intervalMs: number,
  options: SerialPollOptions = {},
) => {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const schedule = () => {
    if (stopped) return;
    timer = globalThis.setTimeout(() => void run(), intervalMs);
  };
  const run = async () => {
    if (stopped) return;
    if (pageIsVisible() && (options.isActive?.() ?? true)) {
      await task().catch(() => undefined);
    }
    schedule();
  };

  if (options.immediate) void run();
  else schedule();

  return () => {
    stopped = true;
    if (timer !== undefined) globalThis.clearTimeout(timer);
  };
};
