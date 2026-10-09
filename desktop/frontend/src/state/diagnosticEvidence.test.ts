import { expect, it } from "vitest";
import { currentDiagnostic } from "./diagnosticEvidence";

it("only exposes fresh quality samples for the same active source", () => {
  const adapter: any = { operational: true, address: "192.0.2.10" };
  const now = Date.now();
  const result: any = { address: adapter.address, status: "available", completed_at: new Date(now - 1000).toISOString() };
  expect(currentDiagnostic(adapter, result, now)).toBe(result);
  expect(currentDiagnostic(adapter, result, now + 60_000)).toBeUndefined();
  expect(currentDiagnostic({ ...adapter, address: "192.0.2.11" }, result, now)).toBeUndefined();
  expect(currentDiagnostic({ ...adapter, operational: false }, result, now)).toBeUndefined();
  expect(currentDiagnostic(adapter, { ...result, status: "unverified" }, now)).toBeUndefined();
  expect(currentDiagnostic(adapter, { ...result, completed_at: "invalid" }, now)).toBeUndefined();
});
