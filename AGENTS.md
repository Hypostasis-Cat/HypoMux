# HypoMux 开发规范

## 语言

- 代码注释、文档、commit message 使用中文
- UI 文案需同步维护 `ui/i18n.py` 的中英文翻译

## UI 规范

### 组件选择
- 优先使用 `qfluentwidgets` 提供的组件（`SpinBox`、`SwitchSettingCard` 等）
- 避免混用原生 Qt 控件（如 `QSpinBox`、`QCheckBox`），确保深浅色主题一致
- 不手写硬编码 QSS 样式（如 `"QSpinBox QToolTip { ... }"`），不同主题下可能异常

### 布局
- 首页（`home_page.py`）只放核心运行状态与操作（引擎开关、网卡列表、遥测指标）
- 高级/低频功能适合放在设置页（`settings_page.py`）或独立弹窗

### 文案
- 控件文案需准确表达实际行为
- “权重”是连接分配比例，不是网卡速度或限速，避免使用 `Mbps` 等单位
- 悬停提示（`ToolTip`）与页面常驻说明（`BodyLabel`）各司其职，不重复描述同一件事

## 架构要点

### 调度器
- `RoundRobinBalancer`：轮询均分
- `WeightedBalancer`：按相对权重加权随机分配，数值越大分配越多连接，与带宽/限速无关
- 两个 balancer 均实现同一接口：`get_next_nic` / `get_next_nic_for_domain` / `on_connect` / `on_disconnect` / `active_connections`
- `_MergedBalancerView` 仅提供 `active_connections()` 聚合视图，不参与调度
- 0 值权重会被钳位到 1（`WeightedBalancer` 初始化时）

### 被墙域名追踪器
- `BlockedDomainTracker`：模块级单例（`get_tracker()`），线程安全
- `is_blocked(nic, domain)` → 调度时查询黑名单
- `on_connect_failure(...)` → 连接失败时触发后台验证任务，在 asyncio loop 中跑
- 验证任务严格 5 次尝试，≥4 次成功即确认，确认后写入黑名单 30 分钟自动过期
- `_shutdown_event_loop` 会 cancel+gather 所有 pending 任务，验证任务的 `finally` 清理 `_pending_verifications` 保证不残留
- `clear_all()` 的写回竞态已通过 pending 校验修复:写入前检查 domain 是否仍在 pending 中
- 默认关闭（`blocked_domain_bypass: False`），仅建议学校/企业有特殊需求的用户使用

### 配置管理
- `config_manager.py`：`default_config` / `_coerce_config` / `load_config` / `save_config`
- 原子写入（tmp + replace），`_coerce_config` 容忍手改 config.json 的字符串 `"false"`/`"0"` 等
- 网卡扫描完成前 `_cards` 为空，`_collect_config` 中对 `nic_bandwidth_limits` 做了空值回退，避免扫描期间持久化覆盖已有权重

### proxy_worker 生命周期
- `run()` → `_serve()` → `_shutdown_event_loop(loop)` 在 finally 中执行
- `_shutdown_event_loop` 收集 `asyncio.all_tasks(loop)` 并 cancel+gather，确保验证任务被正确清理
- `stop()` 从主线程通过 `loop.call_soon_threadsafe` 安全设置停止信号

## 已修复的历史 Bug（备忘）

1. `use_expiry` 状态不持久化 → 已添加 `blocked_domain_expiry` 配置项，写入 `_collect_config`
2. `http_port` 硬编码 → `_start_proxy` 改为读取 config
3. `nic_bandwidth_limits` 扫描前空覆盖竞态 → `_collect_config` 中空值时回退已存值
4. IP 被墙检测 SOCKS/HTTP 路径不一致 → 统一用 `dst_domain or dst_addr`
5. 验证次数超过 5 次 → 统一 `attempts` 计数，while 条件严格限制
6. `clear_all()` 验证任务写回竞态 → pending 校验
7. “10 分钟冷却”文案未实现 → 已移除
