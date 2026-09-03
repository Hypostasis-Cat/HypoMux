import { describe, expect, it } from "vitest";
import { conciseDiagnosticMessage } from "./healthNotice";

describe("conciseDiagnosticMessage", () => {
  it("turns nested timeout errors into one actionable sentence", () => {
    expect(conciseDiagnosticMessage(
      "读取网络体检数据：调用 diagnostics.latest 失败：context deadline exceeded；named pipe request timeout",
      "zh",
    )).toBe("操作超时，请稍后重试。");
  });

  it("does not expose long transport errors in the primary message", () => {
    const raw = "启动失败：" + "底层传输错误 ".repeat(20);
    expect(conciseDiagnosticMessage(raw, "zh")).toBe("操作未完成，请重试；如仍失败，请导出支持日志。");
  });

  it("keeps a short useful explanation", () => {
    expect(conciseDiagnosticMessage("请至少选择一张活动网卡", "zh")).toBe("请至少选择一张活动网卡");
  });

  it("collapses nested service context even when the raw message is not very long", () => {
    expect(conciseDiagnosticMessage(
      "读取体检失败：连接服务失败：请求没有返回有效响应",
      "zh",
    )).toBe("操作未完成，请重试；如仍失败，请导出支持日志。");
  });
});
