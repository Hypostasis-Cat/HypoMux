import type { AppPage } from "../shell/CompactNavigation";

const pages: Record<AppPage, [string, string, string, string]> = {
  home: ["首页", "Home", "我可以帮你检查网卡、启动聚合，或看看网络状态。", "I can check adapters, start aggregation, or inspect network status."],
  routing: ["分流规则", "Routing rules", "需要调整分流吗？选中规则后，可以直接说“把这条改成直连”。", "Select a rule, then ask me to change its route."],
  connections: ["活动连接", "Connections", "想知道流量走了哪里？告诉我应用名称，我帮你检查出口。", "Tell me the app name and I can check where its traffic goes."],
  health: ["网络检测", "Network health", "我可以帮你运行网络检测，解释问题和下一步建议。", "I can run diagnostics and explain the findings."],
  settings: ["系统设置", "Settings", "不确定怎么配置？告诉我你想达到的效果。", "Tell me what you want to achieve and I can help with configuration."],
  tools: ["工具箱", "Toolbox", "不知道用哪个工具？说说你遇到的问题。", "Describe the problem and I can help choose the right tool."],
  assistant: ["AI 工作区", "AI workspace", "我在这里，随时可以继续我们的对话。", "I'm here whenever you want to continue."],
  "blocked-domains": ["屏蔽域名", "Blocked domains", "可以告诉我无法访问的网站，我帮你查找原因。", "Tell me which site is unreachable and I can investigate."],
  about: ["关于", "About", "想了解 HypoMux 能做什么？可以问我。", "Ask me what HypoMux can do."],
  appearance: ["外观", "Appearance", "我知道你正在调整外观，也可以随时帮你处理网络问题。", "You're adjusting appearance. I can also help with network questions."],
};
export function assistantPageContext(page: AppPage, locale: string) {
  const entry = pages[page];
  return { label: entry[locale === "en" ? 1 : 0], greeting: entry[locale === "en" ? 3 : 2] };
}
