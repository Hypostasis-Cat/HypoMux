import type { AdapterView, DiagnosticResult } from "../platform/services";

// A saved probe is a short-lived sample, never an override of Core health.
export function currentDiagnostic(adapter: AdapterView, result: DiagnosticResult | undefined, now = Date.now()) {
  if (!result || !adapter.operational || result.status === "unverified") return undefined;
  const age = now - Date.parse(String(result.completed_at));
  if (!Number.isFinite(age) || age < 0 || age > 60_000) return undefined;
  if (result.address !== (adapter.address || adapter.source_ipv6)) return undefined;
  return result;
}
