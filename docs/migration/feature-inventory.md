# HypoMux v2.2.0 功能清单

> 扫描基线：归档的 WPF 项目快照（只读）
>
> 扫描日期：2026-07-30
>
> 本文档记录 v2.2.0 的功能、配置、状态、提示、权限和生命周期语义。旧版是迁移真值来源，但不是新版视觉模板。

## 1. 扫描范围

已扫描以下入口与实现：

- 应用入口与生命周期：`main.py`
- 主窗口、页面协调、托盘、启动/停止与回滚：`ui/main_window.py`
- 页面：`ui/pages/*.py`
- UI 文案与双语映射：`ui/i18n.py`
- 代理与 TUN 出站池：`proxy_worker.py`
- 配置、网卡、诊断、路由、TUN、WFP、更新与日志：`utils/*.py`
- 安装与发布：`desktop/build/windows/nsis/project.nsi`、`.github/workflows/*.yml`
- 回归测试：`tests/*.py`

当前重构仓库中的 Go Core 和协议也已对照扫描：

- 协议清单：`protocol/v1/manifest.json`
- Go Core API：`engine/internal/api/v1/types.go`
- 当前客户端契约：`protocol/v1/` 与 Wails 生成 bindings

## 2. 页面与功能入口

### 2.1 应用壳层

源码：`main.py:24-286`、`ui/main_window.py:650-1179`、`ui/main_window.py:3082-3139`

- 1120×800 初始窗口，960×680 最小窗口。
- 左侧导航：首页、路由规则、网络体检、系统设置、关于。
- 单网卡被墙域名以独立 780×580 管理窗口打开。
- 浅色、深色、跟随系统主题。
- 自定义强调色、Mica 开关、自定义背景图、内容表面透明度。
- 中文/英文即时切换。
- 单实例：第二次启动向已有实例发送 `WAKE_UP` 并唤醒主窗口。
- 托盘：显示主界面、退出程序、双击唤醒。
- 关闭行为：隐藏到托盘或直接退出。
- `--silent` 静默托盘启动。
- Windows AppUserModelID 与应用图标。
- 右上角 InfoBar：信息、成功、警告、错误。

### 2.2 首页

源码：`ui/pages/home_page.py:39-663`、`ui/main_window.py:1379-1684`、`ui/main_window.py:2711-2755`

- 聚合引擎总开关。
- 系统代理模式 / 虚拟网卡模式切换。
- 启动、停止、切换中和启动阶段状态。
- SOCKS 与 HTTP/HTTPS 端口摘要。
- 合并下载速度。
- 合并上传速度和总连接数。
- 网卡列表：
  - 勾选/取消勾选；
  - 全选/取消全选；
  - 手动刷新；
  - 网卡图标、别名、IPv4、PPP 标识；
  - 单网卡实时速度和连接数；
  - 调度权重 1-100；
  - 未体检、可用、不稳定、不可用状态。
- 权重调度开关；关闭时使用轮询。
- 底部指标：
  - 活动连接；
  - 本期已加速流量；
  - 节点抖动；
  - 核心状态（未托管 / 系统代理托管 / TUN 驱动级托管）。
- 网卡每 5 秒自动重扫；仅列表变化时重建。
- 加速运行中禁止手动刷新和修改影响网络栈的控件。
- 首页与体检页的网卡勾选状态双向同步。

### 2.3 高级分流规则

源码：`ui/pages/routing_page.py:49-753`、`utils/routing_rules.py:1-236`、`ui/main_window.py:1685-1759`

- 三类规则：进程、域名、IP/CIDR。
- 规则优先级：进程 > 域名 > IP；未命中默认走聚合。
- 出口：
  - 多卡聚合；
  - 任一已扫描真实网卡；
  - 直连。
- 添加规则、删除选中规则。
- 读取运行中进程并搜索选择。
- 重复进程规则定位到已有行。
- 进程名、域名、IP/CIDR 规范化和合法性校验。
- 重复规则与非法规则行内错误状态和汇总提示。
- 有效规则稳定排序；输入过程中不跳行，编辑完成后排序。
- 导出 JSON 备份。
- 导入 JSON，校验格式与版本，确认后整体替换。
- 兼容旧列表格式和 v1/v2 备份格式。
- TUN 运行中可继续编辑，但明确提示“重启加速后生效”。
- 系统代理模式不能完整按进程/域名/IP 承载规则；规则的完整执行路径是 sing-box TUN。

### 2.4 网络体检

源码：`ui/pages/tools_page.py:40-354`、`ui/main_window.py:409-482`、`ui/main_window.py:2860-2953`、`utils/diagnostic_runner.py`

- 选择网卡、全选、取消全选、刷新。
- 体检运行态和进度环。
- 逐网卡 ICMP 探针。
- 指定源地址/接口的 TCP 连通性复核。
- 结果：可用、不稳定、不可用。
- 指标：丢包率、平均延迟、抖动、发送/接收包数。
- 配置检查：
  - 源地址绑定；
  - 默认网关；
  - DNS 配置；
  - 自动/固定路由跃点。
- 结果同步回首页网卡健康徽标。
- 体检过程写入诊断日志会话。

### 2.5 系统设置

源码：`ui/pages/settings_page.py:48-511`

全局设置：

- 中文 / English。
- 关闭到托盘 / 直接退出。
- SOCKS5 端口，范围 1-65534。
- HTTP 端口，范围 1-65534。

网络与 DNS：

- 传统 DNS IPv4。
- DoH 策略：自动、关闭、阿里 DNS、DNSPod、Google DNS。
- DNS 非法格式回滚到已保存值并提示。
- DNS 保存失败提示配置权限问题。

高级网络：

- 强制启动虚拟网卡：
  - 跳过外部联网验证；
  - 跳过运行期自动停机；
  - 明确提示风险。
- WFP 严格路由与 DNS 防泄漏。
- WFP 兼容状态。
- 重新检测并修复 Base Filtering Engine。
- 单网卡被墙域名自动规避开关。
- 黑名单 30 分钟自动过期开关。
- 打开被墙域名管理窗口。

配置与启动：

- 开机自动启动。
- 打开 `~/.hypomux` 配置目录。

### 2.6 个性化

源码：`ui/pages/personalization_group.py:39-326`、`ui/components.py`

- 跟随系统 / 浅色 / 深色。
- 默认蓝 / 自定义强调色。
- Mica 即时开关。
- 自定义背景图：
  - PNG、JPG、JPEG、BMP、WebP；
  - 校验图片可读性；
  - 复制到本地配置目录；
  - 清除背景；
  - 删除旧缓存背景。
- 内容表面不透明度 0-100%，仅背景图存在时生效。
- 主题变化后刷新页面高亮、边框、弹窗材质和系统主题状态。

### 2.7 单网卡被墙域名

源码：`ui/pages/blocked_domains_page.py:19-203`、`utils/blocked_domain_tracker.py:1-424`

- 独立管理窗口。
- 按网卡展示已确认域名。
- 展示永久状态或剩余过期时间。
- 删除单条记录。
- 清空全部。
- 手动刷新。
- 加速期间每 3 秒自动刷新。
- 连接失败后使用其他网卡最多验证 5 次，至少 4 次成功才确认。
- 前 3 次全部失败时快速否决，避免把目标整体故障误判成单网卡问题。
- 默认 30 分钟过期。
- 后续调度跳过相应网卡/域名组合。
- 持久化到 `~/.hypomux/blocked_domains.json`。

当前 Go Core 已改用“真实请求比较证据”的 domain quarantine；迁移 UI 时必须保留用户可见语义，同时以 Go Core 的权威状态为准，不能在 React 中复刻探测算法。

### 2.8 关于、更新与支持

源码：`ui/pages/about_page.py:39-458`、`utils/update_checker.py:1-222`

- 应用名称与版本。
- 项目介绍。
- GitHub 入口。
- 检查 GitHub 最新正式 Release。
- 语义版本比较；开发版本可升级到正式版本。
- 更新可用弹窗：
  - 当前/最新版本；
  - 可滚动 Markdown Release Notes；
  - 立即更新 / 稍后。
- 后台下载安装包并显示进度。
- 只接受 `HypoMux_Setup_*.exe`。
- 校验下载大小。
- Release 提供摘要时校验 SHA-256。
- 下载到隔离临时目录。
- 主程序完成网络清理并退出后再启动 Inno Setup。
- 安装完成后清除安装包和临时启动脚本。
- 网络与合规声明。
- SignPath 代码签名致谢。
- 微信与支付宝赞助二维码、赞助说明。

## 3. 配置与持久化

### 3.1 `~/.hypomux/config.json`

源码：`utils/config_manager.py:1-252`

| 键 | 默认值 | 语义 |
| --- | --- | --- |
| `version` | `1` | 配置结构版本 |
| `selected_adapters` | `[]` | 已选网卡别名 |
| `socks_port` | `10800` | SOCKS5 端口 |
| `http_port` | `10801` | HTTP/HTTPS 端口 |
| `run_mode` | `tun` | `proxy` / `tun` |
| `routing_rules` | `[]` | 分流规则 |
| `dns_server` | `223.5.5.5` | 传统 DNS 兜底 |
| `doh_provider` | `auto` | `auto/off/alidns/dnspod/google` |
| `wfp_strict_route` | `true` | 用户的严格路由偏好 |
| `wfp_compatibility_state` | `{}` | 设备本地 WFP 状态、环境指纹和详情 |
| `force_tun_connectivity_bypass` | `false` | 强制模式 |
| `blocked_domain_bypass` | `false` | 域名自动规避 |
| `blocked_domain_expiry` | `true` | 自动过期 |
| `weighted_scheduler` | `false` | 权重调度 |
| `nic_bandwidth_limits` | `{}` | 历史字段名；实际存 1-100 相对调度权重 |

读取时逐字段校验；损坏、缺失或权限异常均回退默认值。写入使用临时文件原子替换。

### 3.2 Qt `QSettings`

组织/应用：`Hypostasis-Cat/HypoMux`

| 键 | 默认值 | 语义 |
| --- | --- | --- |
| `language`、`ui/language` | `zh` | 界面语言 |
| `close_behavior` | `tray` | `tray` / `exit` |
| `socks_port` | `10800` | 历史 UI 端口副本 |
| `http_port` | `10801` | 历史 UI 端口副本 |
| `autostart` | `false` | UI 显示副本，权威值为计划任务是否存在 |
| `theme` | `auto` | `auto/light/dark` |
| `theme_color` | `#0078d4` | 强调色 |
| `theme_color_mode` | 推断 | `default/custom` |
| `mica_enabled` | `true` | Mica |
| `background_image` | 空 | 本地背景图路径 |
| `content_card_opacity` | `88` | 内容表面不透明度 |

新版必须合并重复的端口和语言存储，提供一次性迁移，但保留原语义和默认值。

### 3.3 其他持久化

- `~/.hypomux/blocked_domains.json`：被墙域名/过期时间。
- `~/.hypomux/logs/app.log`：最多最近 3 次会话、最大 5 MiB。
- `~/.hypomux/sing-box/config.json`：运行时生成的 TUN 配置。
- Windows 计划任务 `HypoMuxAutoStart`：登录时最高权限、`--silent`。
- `hypomux-version.txt`：发布工作流写入版本；缺失回退 2.1.0。

## 4. 运行状态

### 4.1 应用状态

- 启动前清理。
- 单实例探测。
- 普通启动 / 静默托盘启动。
- 可见、最小化、隐藏到托盘、从托盘恢复。
- 正常退出、托盘退出、更新退出、异常启动失败。

### 4.2 首页/引擎状态

- `stopped`
- `starting`
- `running`
- `stopping`
- `failed`
- `transitioning`

当前 Go 协议还定义 `degraded`，新版 UI 必须显式呈现，不能折叠成“运行中”。

### 4.3 TUN 子状态

- 预检第三方 TUN。
- 检查同子网/同网关风险。
- 启动出站池。
- 生成并校验 sing-box 配置。
- 启动 TUN 内核。
- 验证 DNS/HTTPS/真实上游。
- 正常运行。
- 周期健康检查。
- DoH 兼容重启。
- WFP `strict_route=false` 兼容重启。
- 停止、回滚、意外停止、验证失败、验证超时、看门狗回滚。

### 4.4 其他状态

- 网卡：未扫描、扫描中、可用列表、空列表、扫描失败。
- 单网卡健康：未体检、可用、不稳定、不可用；Go Core 另有 `healthy/cooldown/probing`。
- 诊断：idle/running/result/error/completed。
- 更新：idle/checking/current/available/downloading/installing/error。
- WFP：unknown/healthy/failed/user-disabled/not-elevated。

## 5. 权限与安全边界

旧版：

- 打包程序无管理员权限时阻止进入主界面。
- 源码模式可请求 UAC 提权重启。
- TUN 切换和启动时再次检查管理员权限。
- WFP 检测/修复需要管理员权限。
- 开机任务使用最高权限。

新版：

- 安装程序仅在安装、升级、卸载 Core Service 时提升。
- 不保留“WebView2 UI 长期管理员运行”的实现。
- Wails Host 以 `asInvoker` 普通用户运行。
- 正式版由 `HypoMuxCore` Windows Service 执行代理、DNS、TUN、路由、WFP 和恢复。
- 开发版未安装服务时才使用按需 `runas` Core。
- 权限请求直接使用 Windows 原生 UAC，不显示重复自定义确认。
- UAC 取消在首页显示 Toast，且不修改网络。
- React 不执行 PowerShell、注册表、路由、WFP、TUN 或进程管理。

## 6. 启动、停止、退出与网络恢复

### 6.1 启动前

- 纠正工作目录，避免计划任务从 System32 启动。
- 注册 AppUserModelID。
- 单实例唤醒。
- 清理遗留 sing-box。
- 仅删除精确名称 `HypoMux-Tun` 的 Wintun 设备和默认路由。
- 主窗口构造时先关闭当前用户系统代理。
- 无副作用 WFP 预检；失败时记忆设备环境指纹并准备兼容模式。

### 6.2 系统代理模式

启动：

1. 校验已选网卡。
2. Steam 运行时提示。
3. 启动 SOCKS5 与 HTTP/HTTPS 代理。
4. 监听成功后写入当前用户 WinINet：
   `http=<http>;https=<http>;socks=<socks>`。
5. 调用 `InternetSetOptionW` 广播刷新。

停止/失败：

- 先写 `ProxyEnable=0` 和空 `ProxyServer`。
- 停止监听和活动连接。
- 6 秒超时后释放 UI，但继续保证系统代理关闭。
- 启动失败、运行错误、正常停止、退出都重复执行幂等关闭。

### 6.3 TUN 模式

启动是事务：

1. 权限检查。
2. 已选网卡检查。
3. WFP 预检和设备兼容策略。
4. 第三方默认 TUN 路由检查。
5. 同子网/同网关风险提示。
6. 启动多端口出站池。
7. DNS/DoH 预检。
8. 原子生成 sing-box 配置。
9. 配置检查通过后才清理旧资源并启动 sing-box。
10. 获取真实上游响应或通过外部联网验证后才宣告成功。

运行中：

- 30 秒周期健康检查。
- 外部探测失败但近期有真实上游响应时忽略误判。
- 物理网络正常而 TUN 连续失败时回滚。
- DoH 持续失败只允许一次受控兼容重启。
- `FwpmEngineOpen0` 失败只允许一次 `strict_route=false` 重启。
- 两次 TUN 会话不得重叠。

停止/失败：

1. 终止自有 sing-box 进程树。
2. 删除精确 `HypoMux-Tun` 默认路由和 Wintun 设备。
3. 停止出站池和所有连接。
4. 清空遥测、解锁 UI。
5. 结束诊断日志会话。

### 6.4 退出

- 关闭到托盘时不停止网络核心。
- 直接退出/托盘退出/更新退出：
  - 先隐藏 UI；
  - 保存路由规则与配置；
  - 停止扫描、诊断、更新和健康检查；
  - 停止 TUN/代理；
  - 关闭系统代理；
  - 清理精确 TUN 资源；
  - 隐藏并销毁托盘；
  - 结束日志会话。
- `aboutToQuit` 再执行一次幂等清理。

## 7. 提示与错误处理

### 7.1 非阻塞反馈

- 信息：语言保存、规则重启生效、开机自启关闭。
- 成功：代理/TUN 启动、DNS 保存、WFP 修复、规则导入导出、开机自启开启。
- 警告：未选择网卡、无网卡、运行中刷新、Steam、第三方 TUN、同网关风险、强制模式、DoH/WFP 兼容降级。
- 错误：网卡扫描、代理监听/系统代理写入、TUN 配置/出站池/内核/验证、诊断、更新。

### 7.2 阻塞确认

- 管理员权限不足。
- WFP 检测失败：尝试修复 / 使用兼容模式。
- 导入规则替换确认。
- 发现新版本：立即更新 / 稍后。

### 7.3 错误恢复原则

- 输入非法时不覆盖已保存有效值。
- 后台扫描失败不打断已有网卡列表。
- 旧工作线程的迟到事件不得覆盖新会话或停止后的 0 值。
- 系统代理和 TUN 清理均为幂等。
- 兼容重启必须等旧会话完全退出；超时则取消重启。
- 更新安装前必须完成网络清理。
- 日志不可写不能影响加速功能。

## 8. 页面与后端调用关系

| 页面/入口 | 用户动作 | v2.2.0 调用链 | 后端效果 |
| --- | --- | --- | --- |
| 首页 | 扫描网卡 | `HomePage.refresh_clicked` → `MainWindow.load_adapters` → `scan_network_adapters` | PowerShell + IPHLPAPI 枚举 |
| 首页 | 选择网卡 | `adapter_checked` → `on_adapter_checked` → `save_config` | 更新会话输入 |
| 首页 | 修改权重 | `adapter_weight_changed` → `_persist_config` | 下次启动调度器读取 |
| 首页 | 启动代理 | `engine_toggled` → `_start_proxy` → `ProxyWorker` → `set_system_proxy` | 代理监听 + WinINet |
| 首页 | 启动 TUN | `engine_toggled` → `_start_tun_mode` → `MultiPortProxyWorker` → `singbox_config` → `TunManager` | 出站池 + sing-box + WFP/Wintun |
| 首页 | 停止 | `engine_toggled` → `_stop_proxy/_stop_tun_mode` | 事务式网络恢复 |
| 分流 | 编辑规则 | `rules_changed` → `on_routing_rules_changed` → `save_config` | 下次 TUN 配置生成 |
| 分流 | 选择进程 | `ProcessListWorker` → `tasklist` → `ProcessSelectDialog` | 添加进程匹配 |
| 分流 | 导入/导出 | `export_requested/import_requested` → 主窗口文件对话框 | JSON 备份 |
| 体检 | 开始 | `start_clicked` → `DiagnosticWorker` → `run_diagnostic` + `probe_bound_tcp` | 每网卡健康结果 |
| 设置 | 端口/DNS | 页面信号 → 主窗口 → `save_config` | 下次启动；TUN 活动时重生配置 |
| 设置 | WFP | 页面信号 → `probe_wfp_engine/try_repair_wfp_services` | 无副作用检测 / BFE 修复 |
| 设置 | 开机自启 | `set_autostart` → `schtasks` | 最高权限登录任务 |
| 被墙域名 | 管理 | 页面 → `BlockedDomainTracker` | 删除/清空/保存 |
| 关于 | 检查更新 | `ReleaseCheckWorker` → GitHub API | 版本与 Release Notes |
| 关于 | 安装更新 | `ReleaseDownloadWorker` → 主窗口清理退出 → Inno Setup | 安全升级 |
| 托盘 | 显示/退出 | 托盘回调 → 窗口/生命周期协调器 | 唤醒或完整清理 |

## 9. 当前 Go Core 对齐情况

已具备：

- JSONL 协议、能力协商和结构化错误。
- 普通代理启动/停止/遥测。
- TUN TCP/UDP 出站池。
- Go 托管 sing-box 生命周期和精确资源清理。
- DNS/DoH 与源网卡绑定。
- 轮询/权重调度。
- 自适应网卡健康。
- 普通代理域名隔离遥测。
- 诊断接口。
- 引擎状态、日志与事件。

仍需补齐或明确契约：

- 网卡枚举作为正式协议方法。
- 用户配置 CRUD 与迁移。
- 分流规则/生成配置的权威服务边界。
- WFP 预检、修复和兼容状态 DTO。
- 系统代理的事务式控制与恢复 DTO。
- 托盘、单实例、开机自启和关闭行为的平台适配。
- 更新器、安装器与签名校验的统一接口。
- 支持日志查看/导出入口。
- 被墙域名管理接口与 Go domain quarantine 的语义映射。
- 进程列表接口。
- TUN 外部联网验证和看门狗策略的最终所有权。

这些缺口必须先扩展 Go/平台服务契约，不能在 React 页面中补业务逻辑。
