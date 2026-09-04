import type { TunPreflightSnapshot } from "../platform/services";

const storageKey = "hypomux.startup-warnings.dismissed-date.v1";

const localDate = (now: Date) =>
  `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`;

export const canDismissStartupWarnings = (snapshot: TunPreflightSnapshot) =>
  snapshot.ready && (snapshot.issues ?? []).every((issue) =>
    issue.level === "warning" || issue.level === "info");

export function startupWarningsDismissedToday(now = new Date()): boolean {
  try {
    return localStorage.getItem(storageKey) === localDate(now);
  } catch {
    // An unavailable store must never silently bypass a warning.
    return false;
  }
}

export function dismissStartupWarningsToday(now = new Date()): boolean {
  try {
    localStorage.setItem(storageKey, localDate(now));
    return true;
  } catch {
    return false;
  }
}
