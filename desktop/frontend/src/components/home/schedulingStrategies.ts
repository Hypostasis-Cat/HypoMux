// Stable UI strategy IDs; map the current engine boolean at the UI boundary.
// Add future strategies here only when the engine supports them.
export const schedulingStrategies = [
  {
    id: "maximum-speed",
    weighted: false,
    strategy: "round-robin",
    label: { zh: "最大速度优先", en: "Maximum speed first" },
    description: {
      zh: "无需设置权重，轮流向可用链路分配新连接。实际速度取决于链路和应用。",
      en: "No weights needed. New connections rotate across available links. Actual speed depends on your links and apps.",
    },
  },
  {
    id: "weighted",
    weighted: true,
    strategy: "weighted",
    label: { zh: "按照权重调度", en: "Schedule by weight" },
    description: {
      zh: "按下方权重比例分配新连接；权重越大，分配越多，不代表带宽占比或限速。",
      en: "Distribute new connections by the weights below. Higher weights receive more connections; these are not bandwidth shares or speed limits.",
    },
  },
  {
    id: "adaptive-throughput", strategy: "adaptive-throughput", weighted: false,
    label: { zh: "自适应速度（实验）", en: "Adaptive speed (experimental)" },
    description: {
      zh: "根据近期 TCP 下载表现和传输负载分配新连接，保留少量探索。UDP 使用轮询；不迁移已有连接。",
      en: "Assign new TCP connections using recent download performance and load, with limited exploration. UDP rotates; established connections stay bound.",
    },
  },
] as const;

export const getSchedulingStrategy = (weighted: boolean, selected?: string) =>
  schedulingStrategies.find((item) => item.strategy === selected) ?? schedulingStrategies.find((item) => item.weighted === weighted)!;
