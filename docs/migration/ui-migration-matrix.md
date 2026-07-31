# HypoMux UI 迁移矩阵

> 功能基线：归档的 WPF 项目快照（只读）
>
> 状态枚举：
>
> - `Not Started`：尚未实现。
> - `In Progress`：正在实现，尚未形成完整界面或服务能力。
> - `Implemented`：界面、数据模型和基本逻辑已实现。
> - `Wired`：已连接真实 Go/平台服务，但尚未完成全部设备和异常矩阵。
> - `Verified`：已完成对应真实路径验收。

## 应用壳层与平台行为

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 应用入口 | 1120×800、最小 960×680 | `ui/main_window.py:967-971` | App Shell | CSS Grid、Wails Window | `Platform.Window.Configure` | Implemented | 初始/最小尺寸已配置；100/125/150% DPI 无裁切留到里程碑 |
| 应用壳层 | 自定义标题栏与窗口拖动 | `FluentWindow` 基类 | App Shell | Fluent Button、Fluent Icons、CSS drag region | `Platform.Window.*` | Wired | 同时声明 Wails v3 `--wails-draggable: drag` 与 Windows caption 区域，交互按钮显式 no-drag；暂存生产构建通过，运行中旧实例退出后做实窗拖动复验 |
| 应用壳层 | 产品图标与窗口标识 | `support/icon.ico` | App Shell / EXE / 任务栏 / 托盘 | 原始 HypoMux 图标资源 | Wails Windows Resources | Implemented | 标题栏、关于页、托盘、EXE 与任务栏统一使用旧版原始 256px 图标；生产 EXE 资源提取复核通过 |
| 应用壳层 | 紧凑侧边导航 | `ui/main_window.py:984-1037` | App Shell | Tooltip、Button、CSS Grid | 无 | Implemented | 首页/分流/体检/日志/设置/关于均可达；键盘与焦点态完整验收待里程碑 |
| 应用入口 | 单实例与唤醒 | `main.py:133-159,235-258` | 平台层 | 无前端组件 | `Platform.Lifecycle.SingleInstance` | Wired | Wails SingleInstance 已接入；二次普通启动唤醒窗口，`--silent` 不抢焦点；待实机重复启动验收 |
| 托盘 | 显示主界面 | `ui/main_window.py:1097-1133` | 系统托盘 | Wails v3 SystemTray | `Platform.Tray.ShowWindow` | Wired | 菜单/单击均调用真实窗口 Show/Focus；待本轮里程碑复验 |
| 托盘 | 退出程序 | `ui/main_window.py:1134-1146` | 系统托盘 | Wails v3 Menu | `Application.QuitSafely` | Wired | 退出菜单先执行诊断与引擎 Shutdown，再退出应用；待残留矩阵 |
| 关闭 | 隐藏到托盘 | `ui/main_window.py:3082-3104` | App Shell | Dialog/Toast | `Platform.Lifecycle.HandleClose` | Wired | 关闭 Hook 读取真实 `close_to_tray` 配置并隐藏窗口 |
| 关闭 | 直接退出 | `ui/main_window.py:3106-3113` | 设置/平台层 | Dropdown | `Window.Close` / `Application.QuitSafely` | Wired | 自定义标题栏不再直接隐藏窗口；关闭请求统一进入后端 Hook，直接退出时隐藏窗口、恢复网络并结束 UI/Core；异常恢复矩阵待验 |
| 启动 | `--silent` 托盘启动 | `main.py:186-189,271-274` | 平台层 | 无 | `Platform.Lifecycle.StartHidden` | Wired | 用户级开机启动项传入 `--silent`，ApplicationStarted 后隐藏；待无闪现复验 |
| 壳层 | 中英文切换 | `ui/main_window.py:1162-1178`、`ui/i18n.py` | 全局 | FluentProvider、i18n | `SettingsService.Update` / `LanguageProvider` | Implemented | 旧版 302 组中英文资源已原样迁移；导航、标题栏、首页、分流、体检、日志、设置、关于、更新与域名管理均即时响应语言切换 |
| 壳层 | Toast/InfoBar | `ui/main_window.py:3116-3139` | 全局反馈层 | Toaster、Toast | `ApplicationEvent` | Implemented | 首页/分流错误、成功与重试动作已接入；全局覆盖随后续页面扩展 |

## 首页

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 首页 | 聚合引擎总开关 | `ui/pages/home_page.py:259-375` | 首页 | Button、Badge、Spinner、Toast | `EngineService.Start/Stop/Snapshot` | Verified | 四态按钮、错误恢复/重试；真实 Core 启停与系统代理恢复测试通过；启动与调度开关已移至模式说明下方的统一控制行 |
| 首页 | 系统代理/TUN 模式 | `home_page.py:268-280` | 首页 | TabList | `EngineService.Start` | Implemented | 运行中锁定；代理已真实验证，TUN 独立提权实机验证待完成 |
| 首页 | 端口摘要 | `home_page.py:333-342,521-542` | 首页 | Text | `SettingsService.Get` | Verified | 首页读取并显示真实 HTTP/SOCKS5 设置值 |
| 首页 | 合并下载速度 | `home_page.py:344-369,481-496` | 首页 | 大号文本、CSS transition | `EngineService.Snapshot` | Implemented | protocol-v1 累计字节差分为速率；真实负载吞吐对照待完成 |
| 首页 | 上传速度/总连接 | `home_page.py:364-369` | 首页 | Text、Badge | `EngineService.Snapshot` | Implemented | 接入真实 telemetry total；真实负载对照待完成 |
| 首页 | 网卡选择 | `home_page.py:39-163,376-419` | 首页 | Checkbox、Tooltip | `AdapterService.List/SaveSelection` | Verified | 真实网卡枚举、选择持久化、启动参数读取通过；网关/DNS/跃点改由 Windows IP Helper API 进程内读取，移除首屏 PowerShell 阻塞 |
| 首页 | 全选/取消全选 | `home_page.py:385-396` | 首页 | Button | `AdapterService.SaveSelection` | Implemented | 任意数量网卡同步保存；0/1/2/4/8/16/32 视觉容量通过 |
| 首页 | 刷新网卡 | `main_window.py:1465-1533` | 首页 | Button、Spinner | `AdapterService.Refresh` | Implemented | 重新枚举真实活动 IPv4 网卡；失败 Toast/重试已接入 |
| 首页 | 网卡速度与连接数 | `home_page.py:135-140,497-504` | 首页 | Text、Badge | `EngineService.Snapshot` | Implemented | 按真实网卡名合并 Core telemetry；负载对照待完成 |
| 首页 | 调度权重 1-100 | `home_page.py:69-109` | 首页 | Button、Input、Tooltip | `AdapterService.SaveSelection` | Implemented | 官方控件 Stepper、键盘上下键、滚轮保护、1–100 服务端拒绝非法值 |
| 首页 | 权重/轮询调度 | `home_page.py:310-330,650-663` | 首页 | Switch、Tooltip | `EngineService.Start(weighted)` | Verified | 默认轮询；真实 weighted=true Core 启动测试通过 |
| 首页 | 体检状态 | `home_page.py:143-151,516-519` | 首页 | Badge | `EngineService.Snapshot/DiagnosticsService.Latest` | Implemented | 首页读取最近体检结果并显示可用/不稳定/不可用；真实探测与页面截图通过 |
| 首页 | 活动连接 | `home_page.py:420-430` | 首页底部状态带 / 活动连接页 | Badge、SearchBox、实时连接列表 | `EngineService.Snapshot.connections` / `EngineService.Connections` | Wired | 总数与逐连接遥测均接真实 Core；展示进程、域名、远端 IP、实际网卡、聚合/指定网卡/直连策略、流量和时长；TUN 直连经 Core 独立 SOCKS TCP/UDP 通道使用 Windows 默认路由并进入同一 registry，双物理网卡实机待验 |
| 首页 | 会话流量 | `home_page.py:420-430,601-608` | 首页底部状态带 | Text | `EngineService.Snapshot.session_bytes` | Implemented | 真实会话累计上下行字节与单位格式已接入 |
| 首页 | 节点抖动 | `home_page.py:420-430` | 首页底部状态带 | Text | `DiagnosticsService.Latest` | Implemented | 体检结果中的延迟、丢包与抖动保留并回写首页状态 |
| 首页 | 核心状态 | `home_page.py:188-207` | 首页底部状态带 | Badge | `EngineService.Snapshot` | Verified | 真实 protocol-v1 握手、状态与版本显示通过 |
| 首页 | 5 秒网卡变化检测 | `main_window.py:744-749,1465-1533` | 后台协调层 | 无 | `AdapterService.List` | Wired | 页面可见期间每 5 秒读取真实网卡并按 ID/IP/状态差异更新；待热插拔实机验收 |
| 首页 | 运行时控件锁定 | `home_page.py:543-569` | 首页 | disabled/busy props | `EngineSnapshot.phase` | Implemented | starting/running/stopping 锁定模式、网卡与权重，禁止重复操作 |
| 首页 | Steam 提示 | `main_window.py:102-137,2620-2634` | 首页反馈层 | Toast | `RoutingRuleService.ListProcesses` | Wired | 代理启动前读取真实进程；检测到 steam.exe 显示旧版完整提示且不阻止启动 |

## 分流规则

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 路由规则 | 进程/域名/IP 三类规则 | `routing_page.py:226-299` | 分流规则 | TabList、Input、Dropdown、DataGrid | `RoutingRuleService.Snapshot/Save` | Verified | 三类规则恢复、60 条容量截图、配置持久化回读测试通过 |
| 路由规则 | 聚合/网卡/直连出口 | `routing_page.py:352-389` | 分流规则 | Dropdown | `AdapterService.List` | Implemented | 动态真实网卡出口保留；Core 新增独立 `nic_<别名>` 通道，TUN 实机待验证 |
| 路由规则 | 添加/删除 | `routing_page.py:599-612` | 分流规则 | Button、DataGrid、Dialog | `RoutingRuleService.Save` | Implemented | Enter 添加、批量选择、删除确认、空表与自动持久化已实现 |
| 路由规则 | 运行中进程选择 | `routing_page.py:116-199,614-655` | 进程选择 Dialog | Dialog、SearchBox、List | `RoutingRuleService.ListProcesses` | Implemented | tasklist 后台读取、GBK 解码、搜索/双击/重复服务端校验已实现 |
| 路由规则 | 格式规范化 | `utils/routing_rules.py:20-87` | 服务层 | Field validation | `RoutingRuleService.Validate` | Verified | IDN、CIDR 网络地址、旧别名、非法字符与整批拒绝单测通过 |
| 路由规则 | 重复/非法提示 | `routing_page.py:465-508` | 分流规则 | Field、Badge、Toast | `RoutingValidation` | Implemented | 重复添加 Toast、错误行标记、保存拒绝已接入 |
| 路由规则 | 稳定排序 | `routing_page.py:516-576` | 分流规则 | DataGrid | `RoutingRuleService.Save` | Implemented | 编辑期间不跳行；成功保存后按进程→域名→CIDR 精度稳定排序 |
| 路由规则 | JSON 导出 | `main_window.py:1696-1725` | 分流规则 | Button、Save Dialog | `RoutingRuleService.Export` | Implemented | UTF-8、format/version/exported_at、v2 结构与原子写入已实现 |
| 路由规则 | JSON 导入替换 | `main_window.py:1727-1759` | 分流规则 | Button、Open Dialog、Dialog | `RoutingRuleService.Import/Save` | Verified | v1/v2/旧列表兼容、全量校验、确认后原子替换、失败不修改测试通过 |
| 路由规则 | TUN 运行中编辑 | `main_window.py:1685-1694` | 分流规则 | MessageBar | `EngineService.Snapshot` | Implemented | 规则立即保存；TUN 运行中明确提示重启聚合后生效 |

## 网络体检

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 网络体检 | 选择/同步网卡 | `tools_page.py:40-96,278-306` | 网络体检 | Checkbox、Button | `AdapterService.SaveSelection` | Implemented | 与首页共用持久化选择；运行中锁定；全选/取消/刷新可用 |
| 网络体检 | 开始/运行态 | `tools_page.py:183-267,317-336` | 网络体检 | Button、Spinner | `DiagnosticsService.Run/Cancel/Latest` | Verified | 防重复、轮询恢复、取消单测、浏览器交互和真实 Windows 运行通过 |
| 网络体检 | ICMP + 绑定 TCP | `main_window.py:409-482`、`diagnostic_runner.py` | 网络体检 | 无直接组件 | `DiagnosticsService.Run` | Implemented | WinAPI 10 次源绑定 ICMP 与 `IP_UNICAST_IF` TCP 已真实运行；仍需双物理网卡矩阵 |
| 网络体检 | 丢包/延迟/抖动 | `tools_page.py:98-162` | 网络体检 | Badge、Text | `DiagnosticResult` | Verified | 旧版阈值、单位及“绑定 TCP 为最终可用性真值”单测和真实探测通过 |
| 网络体检 | 配置检查 | `diagnostic_runner.py` | 网络体检 | Accordion 展开区 | `DiagnosticResult.Checks` | Verified | 源绑定、网关、DNS、跃点及自动/固定模式已由真实网卡返回 |
| 网络体检 | 状态回写首页 | `main_window.py:2890-2929` | 首页与体检页 | Badge、Text | `DiagnosticsService.Latest` | Implemented | 页面切换后读取服务快照，首页显示最近一次延迟/丢包/抖动与健康状态 |

## 设置与个性化

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 设置 | 语言 | `settings_page.py:118-134` | 设置 | Select | `SettingsService.Update` / `LanguageProvider` | Wired | 中/英选项即时更新全部正式页面并真实保存，重启后从设置服务回读；开发环境外观实验室不属于发布导航 |
| 设置 | 关闭行为 | `settings_page.py:137-156` | 设置 | Select | `SettingsService.Update` / Close Hook | Wired | tray/exit 保存后由真实窗口关闭 Hook 执行 |
| 设置 | SOCKS/HTTP 端口 | `settings_page.py:159-181` | 设置 | Input、Field | `SettingsService.Update` | Wired | 1–65534、端口冲突及服务端错误提示已接入原子保存 |
| 设置 | 传统 DNS | `settings_page.py:187-202` | 设置 | Input、Field | `SettingsService.Update` | Wired | IPv4 服务端校验、失败回读回滚与 Toast 已接入 |
| 设置 | DoH 策略 | `settings_page.py:204-224` | 设置 | Select | `SettingsService.Update` | Wired | auto/off/alidns/dnspod/google 五种策略完整并真实保存 |
| 设置 | 强制 TUN | `settings_page.py:227-240` | 设置 | Switch | `SettingsService.Update` | Implemented | 完整风险文案、默认值与配置键已实现；连通性旁路消费端待 TUN 深度验收 |
| 设置 | WFP 严格路由 | `settings_page.py:242-254` | 设置、TUN 启动预检 | Switch、Badge | `TunService.Preflight` / `SettingsService.Update` | Wired | 用户偏好真实保存并由 TUN 预检读取；失败按 Windows 版本与 Core 可执行文件指纹记忆，显式重试或环境更新后自动清除/失效，异常设备实机待验 |
| 设置 | WFP 检测/修复 | `main_window.py:1242-1332` | 设置、TUN 启动预检 | Button、Dialog、Badge | `TunService.Preflight` / `WFP.Repair` | Wired | 先执行 `FwpmEngineOpen0` 只读探测，仅异常时由独立高权限 Core 查询/启动 BFE 并复检；不重置防火墙策略，异常设备矩阵待验 |
| 设置 | 被墙域名开关 | `settings_page.py:267-275` | 设置 | Switch | `SettingsService.Update` / `EngineService.Start` / Core health | Wired | 开关真实保存并传入 Core；关闭时停止学习和消费域名隔离，启用时加载已有清单并回写运行时遥测；双物理网卡实机隔离待验 |
| 设置 | 黑名单过期 | `settings_page.py:277-285` | 设置 | Switch | `SettingsService.Update` / `BlockedDomainService.List` / Core health | Wired | 开关真实保存；开启按 Core 隔离到期时间清理，关闭时以永久语义载入并生成新隔离 |
| 设置 | 管理黑名单 | `settings_page.py:287-295` | 设置/管理页面 | Button | `BlockedDomainService.List` | Wired | 打开独立管理视图并读取真实持久化数据 |
| 设置 | 开机自启 | `settings_page.py:299-312`、`autostart.py` | 设置 | Switch | `Platform.Autostart.*` | Wired | 当前普通权限架构使用 HKCU 启动项与 `--silent`，失败回滚 Toast；待登录实机验收 |
| 设置 | 打开配置目录 | `settings_page.py:314-325` | 设置 | Button | `DesktopHost.OpenDirectory` | Wired | 读取真实配置路径并用 Explorer 打开已创建目录 |
| 设置 | 旧配置迁移与回滚 | `utils/config_manager.py` | 设置 | Button、Dialog | `SettingsService.MigrateLegacy/RollbackLegacyMigration` | Wired | 兼容 v2.x 配置键，迁移前备份，新旧文件均不删除；单测通过，用户数据实机待验 |
| 个性化 | 深/浅/系统 | `personalization_group.py:54-68` | 设置/外观 | TabList、FluentProvider | `Appearance.Theme` | Wired | 设置页即时应用并持久化；系统切换由 matchMedia 联动 |
| 个性化 | 强调色 | `personalization_group.py:70-127` | 设置/外观 | 原生颜色输入、预设按钮 | `Appearance.Accent` | Implemented | 六种预设与自定义色即时生效并持久化；对比度全矩阵待验 |
| 个性化 | Mica | `personalization_group.py:129-136` | 设置/外观 | Dropdown | `DesktopHost.SetWindowMaterial` | Wired | 发布界面仅保留 Mica 与纯色；旧版持久化的 Acrylic/Tabbed/Wallpaper 值自动归一为 Mica |
| 个性化 | 背景图 | `personalization_group.py:138-156,232-286` | 设置/外观 | Button、文件输入 | `Appearance.Wallpaper` | Implemented | PNG/JPG/BMP/WebP 读取、持久化与清除已实现；超大图片容量待验 |
| 个性化 | 独立卡片材质与高斯模糊 | `personalization_group.py:138-196` | 设置/外观 | Dropdown、Slider | `Appearance.PanelMaterial/SurfaceBlur` | Implemented | 窗口 Mica/纯色与卡片高斯磨砂/纯色完全解耦；磨砂强度 0–40px 即时应用并持久化，不模糊背景图片本身 |
| 个性化 | 内容不透明度 | `personalization_group.py:157-196` | 设置/外观 | Slider | `Appearance.SurfaceOpacity` | Implemented | 仅在自定义背景图下启用，0–100% 即时应用并持久化 |

## 被墙域名、关于、日志与更新

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 被墙域名 | 列表与剩余时间 | `blocked_domains_page.py:101-152` | 域名隔离管理 | CSS Grid、Badge | `BlockedDomainService.List/ReplaceRuntime` | Wired | 新旧字典/列表格式真实读取；Core 运行时比较失败隔离遥测会原子同步，永久/分钟/秒状态已实现 |
| 被墙域名 | 删除单条 | `blocked_domains_page.py:154-157` | 域名隔离管理 | Button | `BlockedDomainService.Remove` | Wired | 原子持久化并即时刷新 |
| 被墙域名 | 清空全部 | `blocked_domains_page.py:159-162` | 域名隔离管理 | Button、Dialog | `BlockedDomainService.Clear` | Wired | 二次确认后原子清空 |
| 被墙域名 | 运行中自动刷新 | `blocked_domains_page.py:164-188` | 域名隔离管理 | 无 | `BlockedDomainService.List` | Wired | 页面挂载期间 3 秒刷新，离开页面即卸载计时器 |
| 关于 | 版本/介绍/GitHub | `about_page.py:172-320` | 关于 | Button、Text | `Browser.OpenURL` | Implemented | 版本、构建信息、介绍全文和 GitHub 链接完整 |
| 关于 | 合规声明 | `about_page.py:220-248` | 关于 | Text | 无 | Implemented | 旧版三段文案逐字迁移 |
| 关于 | SignPath 致谢 | `about_page.py:250-283` | 关于 | Image、Text、Link | 无 | Implemented | 原始 SignPath Logo、SignPath.io/Foundation 信息和签名支持说明已迁移；按新版界面要求移除“打开官方发布页”按钮 |
| 关于 | 赞助说明/二维码 | `about_page.py:122-155,285-318` | 关于 | Image、Text | `Assets` | Implemented | 赞助全文、免费使用承诺及原始微信/支付宝二维码已迁移 |
| 关于 | 检查更新 | `about_page.py:384-414` | 关于 | Button、Toast | `UpdaterService.Check` | Wired | GitHub Release 实查、当前/可用/失败状态已接入 |
| 关于 | Release Notes | `about_page.py:78-115` | 更新 Dialog | Dialog、Link、Text | `UpdaterService.Check` | Wired | 真实 Release Notes 的换行、Markdown 链接和裸 HTTPS 链接均安全渲染并通过平台层打开；复杂 Markdown 仍按纯文本显示 |
| 关于 | 下载与进度 | `about_page.py:415-444` | 关于 | Spinner、进度文本 | `UpdaterService.Download/Progress` | Wired | 真实字节进度、大小和可用 SHA-256 校验已接入；断网/大文件实机待验 |
| 更新 | 清理后安装 | `update_checker.py:178-222`、`main_window.py:1148-1159` | 生命周期层 | Dialog | `UpdaterService.LaunchInstaller` / `DesktopHost.Quit` | Wired | 临时目录约束、等待 UI 进程退出、引擎 Shutdown 后启动安装器；升级实机待验 |
| 日志 | 会话诊断日志 | `acceleration_log.py` | 后端支持日志 | 无发布导航入口 | `DiagnosticsService.Logs/ExportLogs/OpenLogDirectory` | Verified | 最近 3 次、5 MiB、用户目录/密钥脱敏测试通过；保留给开发者诊断，不再作为普通用户主页面 |
| 日志 | 关键事件过滤/聚合 | `acceleration_log.py:94-313` | 后端日志服务 | 无 | `SupportLogStore.Record/RecordEvent` | Verified | 关键事件过滤、10 秒重复聚合、会话结束刷新及大小裁剪测试通过 |

## 安装、升级与卸载

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 安装程序 | 普通权限 UI 清单 | `setup.iss` / 应用 manifest | Windows 构建 | asInvoker manifest | `Platform.Packaging` | Implemented | UI manifest 保持 asInvoker；签名安装包令牌级别待里程碑复验 |
| 安装程序 | WebView2 Runtime 安装 | `setup.iss` | NSIS | Wails WebView2 bootstrapper | `Platform.Packaging.WebView2` | Implemented | NSIS 已生成/调用官方 bootstrapper；离线、取消与失败矩阵待验 |
| 安装程序 | Core 与 TUN 运行资源 | `setup.iss:60-73` | Windows 构建 | 无 | `Platform.Packaging.RuntimeAssets` | Wired | 构建任务暂存 engine、sing-box、wintun、libcronet；NSIS 真实创建、更新、自动启动 `HypoMuxCore` Service，安装包实机待验 |
| 升级 | 清理后覆盖安装与配置保留 | `update_checker.py:178-222`、`setup.iss` | 更新 Dialog / NSIS | Dialog | `UpdaterService.LaunchInstaller` / `Core.Recover` | Wired | 覆盖前停止 UI/Service，并调用旧版 Core TUN 恢复与当前用户代理恢复入口；用户配置明确保留，覆盖安装与降级实机矩阵待验 |
| 卸载 | 停止核心与恢复网络 | `setup.iss`、`main.py:83-111` | NSIS 卸载 | 无 | `Core.Recover` / `SystemProxy.Recover` | Wired | 卸载脚本停止 UI/Service 后显式执行窄范围 TUN/路由/Wintun 与当前用户代理恢复，再删除 Service；代理/TUN/WFP/DNS/路由实机残留矩阵待验 |
| 卸载 | 清理程序、托盘与启动项 | `setup.iss`、`autostart.py` | NSIS 卸载 | 无 | `Platform.Autostart` / `Platform.Packaging` | Implemented | 删除 HKCU 启动项、快捷方式、关联、WebView 数据、Service 与安装目录；`%AppData%\HypoMux` 用户配置按重装/回滚策略保留，安装包实机待验 |

## 网络恢复、权限与异常

| 旧版页面 | 旧版功能 | 对应源码位置 | 新版目标页面 | 新版组件 | 后端接口 | 当前迁移状态 | 验收方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 启动 | 管理员权限提示/UAC | `main.py:203-229` | 首页 Toast / 原生 UAC | Toast | `CoreLauncher.EnsureElevated` | Wired | 已删除重复权限 Dialog；优先连接带 ACL、客户端 PID/会话/令牌校验的 `HypoMuxCore` Service，开发版未安装时回退原生 `runas`；取消 Toast 已接入，签名包真实 UAC 矩阵待验 |
| TUN | 第三方 TUN 检测 | `main_window.py:72-99,1918-1949` | 首页启动反馈 | Dialog | `TunService.Preflight` | Implemented | 默认路由只读扫描、第三方隧道阻断及无网络修改测试通过；待有第三方 TUN 的实机矩阵 |
| TUN | 同子网/同网关警告 | `main_window.py:1950-1964` | 首页启动反馈 | Dialog | `TunService.Preflight` | Verified | 仅检测到真实风险时展示；提供继续、返回修改、查看详情，CIDR + 默认网关双条件单测与截图通过 |
| TUN | DoH 兼容重启 | `main_window.py:2083-2230` | 启动反馈 | Toast、状态文本 | `dns.fallback_required` / `EngineService` | Wired | Core 主动事件已真实送达；旧会话先停止、本次改用传统 DNS、只重启一次且不覆盖用户偏好；真实 DoH 故障设备待验 |
| TUN | WFP 兼容重启 | `main_window.py:2488-2521` | 启动反馈 | Toast、状态文本 | `tun.state_changed` / `EngineService` | Wired | 仅对 WFP/BFE/Fwpm 等明确错误执行一次兼容 TUN 重启，不把降级写回偏好；普通故障误判单测通过，真实设备待验 |
| TUN | 启动联网验证 | `main_window.py:2271-2335` | 首页状态 | Spinner、Toast | `EngineService.Start` / `Tun.ConnectivityValidation` | Wired | TUN 激活后通过无代理 HTTP/HTTPS 端点验证；失败调用 deactivate/stop 并恢复代理，强制旁路按配置生效；实机矩阵待验 |
| TUN | 周期看门狗 | `main_window.py:2354-2487` | 后台服务 | Toast | `EngineService.Snapshot` / `DiagnosticProbe.BoundTCP` | Wired | 每 30 秒探测 TUN，物理绑定出口正常且连续三次失败才异步安全停止；断网/误判矩阵待验 |
| 代理 | WinINet 写入与关闭 | `main_window.py:139-157,2756-2848` | 后台服务 | Toast | `SystemProxy.Start/Stop/Recover` | Wired | 启动写入、停止/退出恢复与启动前残留恢复已接真实 Windows 注册表；异常矩阵待验 |
| 退出 | 精确 TUN 资源清理 | `main.py:83-111`、`tun_manager.py` | Core/Broker | 无 | `Tun.Stop/Recover` | Wired | Core 停止、客户端断开、Service SCM Stop 与 TUN 异常退出均进入统一清理；sing-box Job/进程和动态 WFP 会话显式关闭，动态过滤器亦随 BFE 会话崩溃回收；路由/Wintun 残留实机矩阵待验 |
| 退出 | 更新前安全退出 | `main_window.py:1148-1159` | 生命周期层 | 无 | `DesktopHost.Quit` | Wired | 更新 helper 等待 UI PID；UI 退出前执行引擎/诊断清理，待真实更新验收 |
| WebView2 | 缺失时提示/引导 | 旧版无 | 启动器 | 原生 MessageBox | `Platform.WebView2.Check` | Wired | 启动前检查官方 Runtime 注册键，缺失显示原生中文引导并退出；缺失环境待验 |

## 功能完整性冲刺状态说明

- `Implemented`：界面、数据模型和基本逻辑已经存在，但不能据此声称通过实机或异常矩阵。
- `Wired`：已经连接真实 Go/平台服务；本表验收栏明确记录仍需补做的设备和异常验证。
- `Verified`：沿用已有真实实机或异常路径证据，不因本轮快速迁移自动升级。
- 本阶段不使用 Mock 冒充 `Wired`，Vite Chunk 警告与完整安装/DPI/网络矩阵留到里程碑统一验证。
