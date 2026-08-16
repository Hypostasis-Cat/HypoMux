export type SaveHandle<T> = {
  revision: number;
  done: Promise<T>;
};

type PendingSave<TInput> = {
  revision: number;
  input: TInput;
};

type Waiter<TOutput> = {
  revision: number;
  resolve: (value: TOutput) => void;
  reject: (error: unknown) => void;
};

type TailOutcome = { ok: true } | { ok: false; error: unknown };

/** Serialises full-state saves and coalesces queued work to the newest input.
 * Callers can use isCurrent() before applying a response so an older request
 * can never replace newer optimistic state. */
export class LatestSaveQueue<TInput, TOutput> {
  private revision = 0;
  private running = false;
  private pending: PendingSave<TInput> | undefined;
  private waiters: Waiter<TOutput>[] = [];
  private tail: Promise<TailOutcome> = Promise.resolve({ ok: true });

  constructor(private readonly persist: (input: TInput) => Promise<TOutput>) {}

  enqueue(input: TInput): SaveHandle<TOutput> {
    const revision = ++this.revision;
    this.pending = { revision, input };
    const done = new Promise<TOutput>((resolve, reject) => {
      this.waiters.push({ revision, resolve, reject });
    });
    this.tail = done.then<TailOutcome, TailOutcome>(
      () => ({ ok: true }),
      (error: unknown) => ({ ok: false, error }),
    );
    void this.pump();
    return { revision, done };
  }

  isCurrent(revision: number) {
    return revision === this.revision;
  }

  isPending() {
    return this.running || this.pending !== undefined;
  }

  async flush(): Promise<void> {
    const outcome = await this.tail;
    if (!outcome.ok) throw outcome.error;
  }

  private async pump() {
    if (this.running) return;
    this.running = true;
    try {
      while (this.pending) {
        const job = this.pending;
        this.pending = undefined;
        try {
          const result = await this.persist(job.input);
          this.settleThrough(job.revision, (waiter) => waiter.resolve(result));
        } catch (error) {
          this.settleThrough(job.revision, (waiter) => waiter.reject(error));
        }
      }
    } finally {
      this.running = false;
      if (this.pending) void this.pump();
    }
  }

  private settleThrough(revision: number, settle: (waiter: Waiter<TOutput>) => void) {
    const settled = this.waiters.filter((waiter) => waiter.revision <= revision);
    this.waiters = this.waiters.filter((waiter) => waiter.revision > revision);
    settled.forEach(settle);
  }
}
