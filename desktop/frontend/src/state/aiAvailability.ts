import { useEffect, useState } from "react";
import { appServices } from "../platform/services";

export const AI_AVAILABILITY_EVENT = "hypomux:ai-availability-changed";

// Publish only backend-confirmed settings, never an optimistic switch value.
export function publishAIAvailability(enabled: boolean) {
  window.dispatchEvent(new CustomEvent(AI_AVAILABILITY_EVENT, { detail: enabled }));
}

export function useAIEnabled() {
  // Fail closed until persisted settings are known: no companion flash or AI
  // polling on startup when AI was disabled in a previous session.
  const [enabled, setEnabled] = useState<boolean | null>(null);
  useEffect(() => {
    let disposed = false;
    let revision = 0;
    const changed = (event: Event) => {
      const value = (event as CustomEvent<unknown>).detail;
      if (typeof value !== "boolean") return;
      revision++;
      setEnabled(value);
    };
    window.addEventListener(AI_AVAILABILITY_EVENT, changed);
    const requestedRevision = revision;
    void appServices.settings.get().then(settings => {
      if (!disposed && revision === requestedRevision) setEnabled(settings.ai_enabled !== false);
    }).catch(() => { /* A failed load must not start AI. Settings offers retry. */ });
    return () => {
      disposed = true;
      window.removeEventListener(AI_AVAILABILITY_EVENT, changed);
    };
  }, []);
  return enabled;
}
