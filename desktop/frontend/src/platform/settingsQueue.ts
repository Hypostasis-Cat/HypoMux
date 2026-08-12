// Serializes settings persistence and tracks which fields each queued
// operation owns. Authoritative responses (persisted state, or server truth
// re-fetched after a failure) are merged so that only optimistic values of
// operations still queued or running survive; fields of finished operations
// always come from the authoritative value. Without this, a failed earlier
// operation's recovery (which resets the UI to server truth) would let stale
// values overwrite a later operation's successful response.
export class SettingsSaveQueue {
  private chain: Promise<void> = Promise.resolve();
  // Reference counts per field: the same field can be owned by several queued
  // operations, and an earlier operation starting must not drop the marker
  // for a later one.
  private pendingFieldCounts = new Map<string, number>();
  private allFieldsQueuedCount = 0;

  enqueue<T>(operation: () => Promise<T>, fields: string[] | null): Promise<T> {
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
      try {
        return await operation();
      } finally {
        // Release field ownership when the operation finishes, success or
        // failure: while it is still running, its optimistic values must
        // survive concurrent merges.
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
    });
    this.chain = next.then(
      () => undefined,
      () => undefined,
    );
    return next;
  }

  mergeAuthoritative<T extends object>(authoritative: T, current: T): T {
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
    return merged as T;
  }

  /**
   * Merges an authoritative response for a failed operation whose own fields
   * must come from the authoritative value (its optimistic values are stale),
   * while optimistic values of other queued operations survive. Unlike
   * mergeAuthoritative, this does not depend on the caller having already
   * released the failed operation's field ownership, so it stays correct
   * regardless of when the recovery merge runs.
   */
  recoverAuthoritative<T extends object>(
    ownedFields: string[] | null,
    authoritative: T,
    current: T,
  ): T {
    const merged = { ...authoritative } as Record<string, unknown>;
    if (this.allFieldsQueuedCount > 0) {
      // A queued full-replace operation will persist every field; keep all
      // optimistic values until it runs.
      return { ...current };
    }
    const owned = new Set(ownedFields ?? []);
    for (const field of this.pendingFieldCounts.keys()) {
      if (!owned.has(field) && field in current) {
        merged[field] = (current as Record<string, unknown>)[field];
      }
    }
    return merged as T;
  }
}
