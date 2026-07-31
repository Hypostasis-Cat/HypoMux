# HypoMux Wails 正式迁移：批次 A / B 验证报告

验证日期：2026-07-31

功能真值：归档的 WPF 项目快照（只读）

Wails 工程：`desktop`

## 批次 A：首页与真实 Core

### 已完成

- 使用 `EngineService` 托管 `hypomux-engine.exe` protocol-v1 JSONL 会话。
- 首页从真实 `AdapterService` 枚举活动 IPv4 网卡，不再使用模拟 Hook。
- 启停按钮使用 Fluent `Button`、`Play`/`Stop` 图标和 `Spinner`，覆盖
  stopped、starting、running、stopping 与失败恢复/重试。
- 系统代理启动采用恢复点后写入；停止时恢复启动前注册表值。
- 权重严格使用旧版 1–100、步进 1、默认 1 的相对权重语义。
- 权重控件使用 Fluent `Button` + `Input` Stepper，支持键盘上下键并阻止滚轮误改。
- 页面采用固定三段布局；网卡列表独立滚动，底部状态栏固定。
- 0、1、2、4、8、16、32 张网卡容量截图已生成。
- 端口、选择状态、权重/轮询、Core 版本、连接、累计会话流量与实时速率
  均来自 Go 服务或真实 Core DTO。

### 验证

- 前端生产构建：通过。
- `go test ./...`（Wails）：通过。
- `go test ./...`（Go Core）：通过。
- 真实 Core `engine.hello` / `engine.status`：通过。
- 真实系统代理启停与恢复集成测试：通过。
- Wails v3 生产构建：通过，输出 `bin/hypomux.exe`。
- 实际运行：`hypomux.exe` 与 `hypomux-engine.exe` 均响应。
- 实机截图：`qa/batch-b/wails-actual-final-foreground.png`。
- 最终首页截图：`qa/batch-a/home-final.png`。

### 未完成 / 已知差异

- TUN 的 Go + sing-box 启动路径已实现，但普通权限 Core 的独立 UAC 提权与
  完整 TUN 实机启动尚未验证，因此矩阵状态为 `Implemented`，不是 `Verified`。
- 首页完整体检结果、延迟、丢包、抖动属于批次 C；当前仅显示 Core health 状态。
- 5 秒网卡变化差异监听和 Steam 一次性提示尚未迁移。
- 普通负载下的真实吞吐/连接 telemetry 已接通；仍需在产生下载流量的环境中做
  UI 数值与 Core 快照对照。

## 批次 B：高级分流规则

### 已完成

- 进程、域名、IP/CIDR 三类规则使用 Fluent `TabList`。
- 使用高密度 Fluent `DataGrid`；表头固定，内容区域独立滚动。
- 支持添加、Enter 提交、编辑、筛选、多选、批量删除与删除确认。
- 支持运行中进程读取、搜索、单击选择与双击添加。
- 草稿保存在页面总状态中，切换 Tab 不会丢失。
- Go 服务逐条校验并在保存时整批再校验；非法数据不会覆盖现有配置。
- 同一规则连续编辑使用按规则 ID 隔离的校验序号，旧异步响应不会覆盖新输入。
- 保留旧版格式规范化：
  - 进程名路径/冒号限制；
  - 域名去通配前缀、去尾点、小写与 IDNA；
  - 单 IP 转 `/32` 或 `/128`；
  - CIDR 归一到网络地址；
  - 重复规则按类型和规范值去重；
  - 进程 → 域名 → IP/CIDR 固定优先级；
  - IP 同类型按前缀长度优先。
- 导入兼容原始列表、`hypomux-routing-rules` v1 和 v2。
- 导入先完整解析/校验，再显示替换确认，确认后原子保存。
- 导出使用 UTF-8 v2 格式，包含 `format`、`version`、`exported_at` 和 `rules`。
- 动态单网卡出口保持旧版语义。Go Core 新增 additive
  `dynamic_nic_channels` 能力，为每个已选网卡提供独立 `nic_<别名>` 通道。
- TUN 运行中编辑会立即保存，并明确提示重启聚合后生效。

### 验证

- 60 条规则 1120×800 容量截图：通过。
- IDN、CIDR、非法整批拒绝：单测通过。
- v1 / 旧列表导入兼容：单测通过。
- 配置保存与重新加载：单测通过。
- 动态单网卡 Core 通道：Core 单测通过。
- 真实 Wails 空配置页与 Go 服务连接：实机截图通过。
- 前端生产构建、Wails Go 测试、Core 测试、Wails v3 构建：全部通过。
- 最终 Wails 产物：`bin/hypomux.exe`，2026-07-31 09:00 重建。

### 未完成 / 已知差异

- 动态单网卡通道已通过 Core 配置测试，但依赖 TUN 独立提权，完整实机流量
  选择仍与批次 A 的 TUN 提权验证一起保持 `Implemented`。
- Native Open/Save Dialog 已接入并有格式/持久化自动测试；仍需一次人工选择真实
  文件路径的桌面交互验收，之后才能把相关交互单独提升为完全人工 Verified。
- 当前前端包有大于 500 kB 的 Vite chunk 警告，不影响构建或运行；后续页面增加时
  应按路由拆分代码。

## 迁移矩阵

`docs/migration/ui-migration-matrix.md` 已更新。状态列现统一只使用：

- `Not Started`
- `In Progress`
- `Implemented`
- `Verified`

未因静态页面、浏览器容量夹具或仅能编译而标记为 `Verified`。
