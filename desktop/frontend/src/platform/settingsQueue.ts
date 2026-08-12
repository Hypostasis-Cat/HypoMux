// Serializes settings persistence and tracks which fields each queued
// operation owns. Operations never merge state themselves: they return a
// SaveOutcome (authoritative value on success, server truth to restore on
// failure), and the queue releases the operation's field ownership first and
// then merges. Because the merge always happens after ownership release,
// results are correct regardless of React scheduling, a failed operation can
// never overwrite a later success, and a full-replace operation's own
// authoritative value is never mistaken for a queued optimistic value.

export type SaveFields = string[] | null; // null = all fields

export type SaveOutcome<TValue, TState extends object> =
  | { ok: true; value: TValue; authoritative: TState }
  | { ok: false; error: Error; restore: TState | null }; // null = keep UI as-is

export class SettingsSaveQueue<TState extends object> {
  private chain: Promise<void> = Promise.resolve();
  // Reference counts per field: the same field can be owned by several queued
  // operations, and an earlier operation finishing must not drop the marker
  // for a later one.
  private pendingFieldCounts = new Map<string, number>();
  private allFieldsQueuedCount = 0;
  private applySink:
    | ((updater: (current: TState) => TState) => void)
    | null = null;

  /** React side registers its state setter here; the queue drives merges
   * through it after releasing ownership. */
  attach(applySink: (updater: (current: TState) => TState) => void) {
    this.applySink = applySink;
  }

  enqueue<TValue>(
    operation: () => Promise<SaveOutcome<TValue, TState>>,
    fields: SaveFields,
  ): Promise<TValue> {
    if (fields === null) {
      this.allFieldsQueuedCount += 1;
    } else {
      for (const field of fields) {
        this.pendingFieldCounts.set(
          field,
          (this.pendingFieldCounts.get(field) ?? 0) + 1,
        );
      }
    }
    const next = this.chain.then(async () => {
      let outcome: SaveOutcome<TValue, TState>;
      try {
        outcome = await operation();
      } finally {
        // Release ownership before merging: this operation's outcome (and
        // only this operation's) is now authoritative.
        this.releaseFields(fields);
      }
      if (outcome.ok) {
        this.applyMerged(outcome.authoritative);
        return outcome.value;
      }
      if (outcome.restore !== null) {
        this.applyMerged(outcome.restore);
      }
      throw outcome.error;
    });
    this.chain = next.then(
      () => undefined,
      () => undefined,
    );
    return next;
  }

  mergeAuthoritative(authoritative: TState, current: TState): TState {
    const merged = { ...authoritative } as Record<string, unknown>;
    if (this.allFieldsQueuedCount > 0) {
      // A queued full-replace operation will persist every field; keep all
      // optimistic values until it runs.
      return { ...current };
    }
    for (const field of this.pendingFieldCounts.keys()) {
      if (field in current) {
        merged[field] = (current as Record<string, unknown>)[field];
      }
    }
    return merged as TState;
  }

  private releaseFields(fields: SaveFields) {
    if (fields === null) {
      this.allFieldsQueuedCount -= 1;
    } else {
      for (const field of fields) {
        const count = (this.pendingFieldCounts.get(field) ?? 1) - 1;
        if (count <= 0) {
          this.pendingFieldCounts.delete(field);
        } else {
          this.pendingFieldCounts.set(field, count);
        }
      }
    }
  }

  private applyMerged(authoritative: TState) {
    this.applySink?.((current) => this.mergeAuthoritative(authoritative, current));
  }
}
