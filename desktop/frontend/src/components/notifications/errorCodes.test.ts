import { describe, expect, it } from "vitest";
import { resolveNotificationErrorCode } from "./errorCodes";

describe("resolveNotificationErrorCode", () => {
  it("prefers a concrete cause over the page fallback", () => {
    expect(resolveNotificationErrorCode({
      title: "保存失败",
      message: "operation timed out",
      dedupeKey: "settings:error:保存失败",
    })).toBe("HM-E1001");
  });

  it("uses a stable subsystem code when the raw cause is unknown", () => {
    expect(resolveNotificationErrorCode({
      title: "保存失败",
      message: "unexpected response",
      dedupeKey: "routing:error:保存失败",
    })).toBe("HM-E1303");
  });

  it("always returns a documented fallback", () => {
    expect(resolveNotificationErrorCode({ title: "失败", message: "unknown" })).toBe("HM-E1900");
  });
});
