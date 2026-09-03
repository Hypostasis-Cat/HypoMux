export type ErrorCodeContext = {
  title: string;
  message: string;
  dedupeKey?: string;
};

type ErrorCodeRule = {
  code: string;
  matches: (text: string, key: string) => boolean;
};

const includesAny = (value: string, terms: string[]) => terms.some((term) => value.includes(term));

const rules: ErrorCodeRule[] = [
  { code: "HM-E1001", matches: (text) => includesAny(text, ["timeout", "timed out", "deadline exceeded", "超时"]) },
  { code: "HM-E1002", matches: (text) => includesAny(text, ["named pipe", "connection refused", "failed to fetch", "服务不可用", "命名管道", "连接被拒绝"]) },
  { code: "HM-E1003", matches: (text) => includesAny(text, ["permission", "access denied", "administrator", "elevated", "权限", "拒绝访问", "管理员"]) },
  { code: "HM-E1004", matches: (text) => includesAny(text, ["dns", "resolve", "network", "internet", "connectivity", "网络", "联网", "解析失败"]) },
  { code: "HM-E1201", matches: (text, key) => key.startsWith("settings:") && includesAny(text, ["读取", "load", "read"]) },
  { code: "HM-E1202", matches: (_text, key) => key.startsWith("settings:") },
  { code: "HM-E1301", matches: (text, key) => key.startsWith("routing:") && includesAny(text, ["读取", "load", "read", "进程", "process"]) },
  { code: "HM-E1302", matches: (text, key) => key.startsWith("routing:") && includesAny(text, ["格式", "校验", "检查", "invalid", "validate", "check"]) },
  { code: "HM-E1303", matches: (_text, key) => key.startsWith("routing:") },
  { code: "HM-E1401", matches: (_text, key) => key.startsWith("health:") },
  { code: "HM-E1501", matches: (_text, key) => key.startsWith("connections:") },
  { code: "HM-E1601", matches: (text, key) => key.startsWith("about:") && includesAny(text, ["检查更新", "update check", "check for update"]) },
  { code: "HM-E1602", matches: (_text, key) => key.startsWith("about:") },
  { code: "HM-E1701", matches: (_text, key) => key.startsWith("blocked-domains:") },
  { code: "HM-E1101", matches: (_text, key) => key.startsWith("home:") },
];

export function resolveNotificationErrorCode({ title, message, dedupeKey }: ErrorCodeContext) {
  const text = `${title} ${message}`.toLowerCase();
  const key = (dedupeKey ?? "").toLowerCase();
  return rules.find((rule) => rule.matches(text, key))?.code ?? "HM-E1900";
}
