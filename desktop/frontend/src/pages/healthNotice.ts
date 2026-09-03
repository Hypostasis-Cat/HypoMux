export type HealthNoticeIntent = "success" | "error" | "warning";

const normalizeMessage = (message: string) => message
  .replace(/^error:\s*/i, "")
  .replace(/\s+/g, " ")
  .trim();

export function conciseDiagnosticMessage(message: string, locale: "zh" | "en") {
  const normalized = normalizeMessage(message);
  if (!normalized) {
    return locale === "en" ? "The operation did not complete. Please try again." : "操作未完成，请重试。";
  }

  const lower = normalized.toLowerCase();
  if (
    lower.includes("timeout") || lower.includes("timed out") ||
    lower.includes("deadline exceeded") || normalized.includes("超时")
  ) {
    return locale === "en" ? "The operation timed out. Please try again." : "操作超时，请稍后重试。";
  }
  if (
    lower.includes("cancelled") || lower.includes("canceled") ||
    normalized.includes("已取消") || normalized.includes("用户取消")
  ) {
    return locale === "en" ? "The operation was cancelled." : "操作已取消。";
  }
  if (
    lower.includes("core") || lower.includes("named pipe") ||
    lower.includes("connection refused") || lower.includes("failed to fetch") ||
    normalized.includes("核心") || normalized.includes("命名管道") ||
    normalized.includes("连接被拒绝") || normalized.includes("服务不可用")
  ) {
    return locale === "en"
      ? "The service is temporarily unavailable. Please try again."
      : "服务暂时不可用，请稍后重试。";
  }

  const firstLine = normalized.split(/\r?\n|；|;\s/)[0].trim();
  const contextSeparators = firstLine.match(/[：:]/g)?.length ?? 0;
  if (firstLine.length <= 56 && contextSeparators <= 1) return firstLine;
  return locale === "en"
    ? "The operation did not complete. Try again, or export support logs if it keeps failing."
    : "操作未完成，请重试；如仍失败，请导出支持日志。";
}
