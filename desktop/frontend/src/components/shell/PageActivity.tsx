import { createContext, useContext, useRef } from "react";

// Retained pages keep drafts and scroll positions, but pause display-only work.
export const PageActivity = createContext(true);
export const usePageActive = () => useContext(PageActivity);

export function usePageActiveRef() {
  const active = usePageActive();
  const ref = useRef(active);
  ref.current = active;
  return ref;
}
