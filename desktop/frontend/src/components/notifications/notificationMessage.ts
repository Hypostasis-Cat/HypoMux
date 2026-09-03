export type NotificationLocale = "zh" | "en";
export type NotificationIntent = "success" | "error" | "warning" | "info";

const normalizeMessage = (message: string) => message
  .replace(/^error:\s*/i, "")
  .replace(/\s+/g, " ")
  .trim();

export function conciseDiagnosticMessage(message: string, locale: NotificationLocale) {
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
  if (
    lower.includes("permission") || lower.includes("access denied") ||
    lower.includes("administrator") || lower.includes("elevated") ||
    normalized.includes("权限") || normalized.includes("拒绝访问") || normalized.includes("管理员")
  ) {
    return locale === "en"
      ? "Permission was denied. Check the app's permissions and try again."
      : "权限不足，请检查应用权限后重试。";
  }
  if (
    lower.includes("dns") || lower.includes("resolve") || lower.includes("network") ||
    lower.includes("internet") || lower.includes("connectivity") ||
    normalized.includes("网络") || normalized.includes("联网") || normalized.includes("解析失败")
  ) {
    return locale === "en"
      ? "The network is unavailable. Check the connection and try again."
      : "网络暂时不可用，请检查连接后重试。";
  }

  const firstLine = normalized.split(/\r?\n|；|;\s/)[0].trim();
  const contextSeparators = firstLine.match(/[：:]/g)?.length ?? 0;
  if (firstLine.length <= 56 && contextSeparators <= 1) return firstLine;
  return locale === "en"
    ? "The operation did not complete. Try again, or export support logs if it keeps failing."
    : "操作未完成，请重试；如仍失败，请导出支持日志。";
}

export function prepareNotificationMessage(
  message: string,
  intent: NotificationIntent,
  locale: NotificationLocale,
) {
  const detail = normalizeMessage(message);
  if (!detail) return { summary: "", detail: undefined };

  const summarized = intent === "error"
    ? conciseDiagnosticMessage(detail, locale)
    : detail.length > 72
      ? `${detail.slice(0, 69).trimEnd()}…`
      : detail;

  return {
    summary: summarized,
    detail: summarized === detail ? undefined : detail,
  };
}
