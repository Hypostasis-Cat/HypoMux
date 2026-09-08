// Stable UI strategy IDs; map the current engine boolean at the UI boundary.
// Add future strategies here only when the engine supports them.
export const schedulingStrategies = [
  {
    id: "maximum-speed",
    weighted: false,
    label: { zh: "最大速度优先", en: "Maximum speed first" },
    description: {
      zh: "无需设置权重，轮流向可用链路分配新连接。实际速度取决于链路和应用。",
      en: "No weights needed. New connections rotate across available links. Actual speed depends on your links and apps.",
    },
  },
  {
    id: "weighted",
    weighted: true,
    label: { zh: "按照权重调度", en: "Schedule by weight" },
    description: {
      zh: "按下方权重比例分配新连接；权重越大，分配越多，不代表带宽占比或限速。",
      en: "Distribute new connections by the weights below. Higher weights receive more connections; these are not bandwidth shares or speed limits.",
    },
  },
] as const;

export const getSchedulingStrategy = (weighted: boolean) =>
  schedulingStrategies.find((strategy) => strategy.weighted === weighted)!;
