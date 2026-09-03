import { describe, expect, it } from "vitest";
import { prepareNotificationMessage } from "./notificationMessage";

describe("prepareNotificationMessage", () => {
  it("keeps transport details out of the primary error message", () => {
    const raw = "stage=tun_data_path endpoint=http://www.msftconnecttest.com/connecttest.txt: curl (28) Resolving timed out after 4007 milliseconds";
    expect(prepareNotificationMessage(raw, "error", "zh")).toEqual({
      summary: "操作超时，请稍后重试。",
      detail: raw,
    });
  });

  it("keeps short success messages directly visible", () => {
    expect(prepareNotificationMessage("设置已保存", "success", "zh")).toEqual({
      summary: "设置已保存",
      detail: undefined,
    });
  });

  it("folds long informational text into details", () => {
    const raw = "a".repeat(140);
    const result = prepareNotificationMessage(raw, "info", "en");
    expect(result.summary).toHaveLength(70);
    expect(result.detail).toBe(raw);
  });
});
