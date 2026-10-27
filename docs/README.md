# HypoMux 文档

## 发布与版本

- [版本与发布渠道](RELEASE_VERSIONING.md)：正式版、Beta / RC、版本同步命令与更新渠道选择。

## 功能说明

- [AI 助手与外部 MCP](AI_ASSISTANT.md)：模型接入、API 密钥的 DPAPI 加密存储、外部 MCP 数据边界。
- [AI 功能开关](AI_FEATURE_SWITCH.md)：AI 助手相关功能的启用与关闭。
- [AI 提供方兼容性](AI_PROVIDER_COMPATIBILITY.md)：不同模型 API 的协议与鉴权差异。
- [热点聚合](AGGREGATION_HOTSPOT.md)：移动热点下的多网卡聚合行为与限制。
- [MTU 检测](MTU_DETECTION.md)：MTU 探测流程与恢复动作。
- [支持与诊断工具](../tools/support/README.md)：安装诊断、网络诊断与升级修复脚本的用法。

## 方案与验证

- [IPv6 适配与验收](ipv6-adaptation-plan.md)：双栈、IPv6-only、DNS64/NAT64、测试证据与实机验收条件。
- [算法与性能优化方案](algorithm-performance-plan.md)：分阶段路线、第一阶段实现与微基准结果。
- [Steam 下载节点优选](steam-cdn-optimization.md)：双模式实现方案、开关行为和验证结果。
- 其余 `steam-*`、`UI_REFINEMENT_PLAN_*`、`*_2026-*` 等文件是带日期的阶段性方案与评审记录，完成状态见各自文档头部。

- `architecture/`：Engine、TUN、DNS、双栈出口与 Windows 资格验证的架构记录。
- `migration/`：Wails 客户端迁移的功能清单、计划、UI 对照矩阵与变更记录。
- `validation/`：Wails 各批次验证报告及截图证据（`wails/`），以及安装器、更新器、IPv6 与发布评审记录。
