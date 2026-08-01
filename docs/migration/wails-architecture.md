# HypoMux Wails v3 架构

> 正式候选栈：Wails v3 + React + TypeScript + Vite + Fluent UI React v9。
>
> Wails v3 当前仍为 Alpha。采用它是明确的产品决策，因此必须通过版本锁定、适配层和可替换边界控制风险。

## 1. 架构目标

- Wails 只负责桌面 Host 和平台适配。
- React 只负责视图、局部交互和展示状态。
- 网络、TUN、代理、DNS、WFP、路由、分流和恢复全部留在 Go Core/服务层。
- Wails Host 不长期以管理员权限运行。
- 业务代码不直接散落调用 Wails API。
- v2.2.0 的功能、配置语义、工作流、提示、权限和恢复保障全部保留。
- 新 UI 不复制 Qt 视觉，也不采用通用 Dashboard 模板。

## 2. 进程与权限模型

```text
┌───────────────────────────────────────────────────────────┐
│ HypoMux Desktop（普通用户）                               │
│                                                           │
│  React UI                                                 │
│      │ typed commands / view events                       │
│  Application Services                                     │
│      │                                                    │
│  Platform Facade ── Wails v3 Adapter                      │
│      │ Window / Tray / Dialog / Events / Updater          │
└──────┼────────────────────────────────────────────────────┘
       │ 本地、鉴权、版本协商的 IPC
┌──────▼────────────────────────────────────────────────────┐
│ HypoMux Go Core / Privileged Broker                       │
│                                                           │
│  Engine │ DNS │ Proxy │ TUN │ WFP │ Routing │ Recovery   │
│  Logs   │ Diagnostics │ Adapter health │ sing-box owner   │
└───────────────────────────────────────────────────────────┘
```

权限规则：

- Wails/React 进程始终普通权限。
- 正式版由安装时注册的高权限 Core Windows Service 执行代理、TUN、WFP、DNS、路由和恢复。
- 开发版在服务未安装时，才按需使用 `runas` Core。
- React 只收到“需要权限”“用户取消”“权限获取失败”等结构化结果。
- 禁止在前端使用 PowerShell、注册表、Win32 路由、`taskkill` 或 `schtasks`。

### 2.1 高权限 Core 连接

正式版与开发版共用 `CoreLauncher`，但启动来源不同：

1. 安装程序只在安装、升级和卸载服务时提升一次，并注册 `HypoMuxCore` Windows Service。
2. UI 始终使用 `asInvoker` 普通权限启动。
3. UI 先查询 SCM；若 `HypoMuxCore` 已安装且运行，连接固定服务管道 `\\.\pipe\HypoMux-Core-Service`。
4. UI 通过 SCM 获取服务 PID，再用 `GetNamedPipeServerProcessId` 验证管道服务端身份；服务端还必须通过 ACL、客户端 PID、用户 SID/会话和安装身份校验 UI。
5. 服务已安装但停止、管道缺失或身份不匹配时必须报错，禁止静默回退到临时进程。
6. 仅在开发环境未安装服务时，Host 生成一次性随机令牌，通过 `ShellExecuteExW` + `runas` 启动临时管理员 Core。
7. 开发管道使用当前用户 SID、Administrators 与 SYSTEM ACL，并执行双向 PID、协议版本和 256 位一次性令牌校验。
8. Core/Service 拥有 sing-box Job、代理、DNS、路由、Wintun、WFP 和回滚；React 不直接管理任何网络资源。

`CoreLauncher` 已实现“Windows Service 优先、服务未安装时开发版 `runas` 回退”的客户端选择边界。`HypoMuxCore` 服务宿主、固定 Named Pipe、ACL、服务 PID 校验、活动控制台会话与客户端令牌校验及安装器注册均已接线；签名身份强化、安装升级和异常恢复矩阵仍属于发布前验收。

### 2.2 P7 第一批已落地的安全门

当前桌面端已增加 `TunService.Preflight`，并在 `EngineService.Start("tun")` 内再次执行同一权威检查。检查发生在 `engine.start`、出站池、sing-box、Wintun、路由和 WFP 接管之前。

当前只读检查包括：

- 活动网卡与 IPv4 前缀；
- hypomux-engine / sing-box 资源；
- 第三方 TUN 默认路由；
- 同 IPv4 子网且共用默认网关风险；
- `FwpmEngineOpen0` 打开后立即关闭的 WFP 可用性；
- WebView2 Host 令牌和独立权限 Broker 状态。

若独立 Core 启动器不可用，TUN 必须返回结构化阻断结果，不能退回“让整个 Wails/WebView2 以管理员身份运行”。WFP 探测失败只生成本次兼容模式决策，不覆盖用户的严格路由偏好。

### 2.3 P7 第二批已落地的权限边界

- `engineclient.Client` 通过 `CoreLauncher` 区分普通 stdio Core 与按需管理员 Core。
- `EngineService.Start("tun")` 只调用 `EnsureElevated`；代理模式保持普通权限。
- 权限请求不再显示自定义确认 Dialog。无风险时只读预检后直接进入 Core Service 或 Windows 原生 UAC。
- 只有检测到共享网关、第三方 TUN、WFP 异常、资源缺失等真实问题时才显示风险 Dialog。
- 可继续的 warning 提供“继续 / 返回修改 / 查看详情”；blocker 保留相同入口，但“继续”禁用。
- Windows UAC 取消映射为 `ErrElevationCancelled`，不会遗留已协商 Core。
- 首页将 UAC 取消映射为 Toast：“未获得管理员权限，聚合未启动”。
- 命名管道实现双向 PID 校验、受限 ACL、一次性令牌和协议握手；错误令牌、错误客户端 PID、错误 Host PID 均有负向测试。
- Windows 构建把 Core、sing-box、wintun 和 libcronet 放入 `bin`，NSIS 模板安装到 `$INSTDIR\bin`。

UI 仍为 `asInvoker`，普通主题、日志、规则编辑和状态查看不请求管理员权限。本批没有自动点击真实 UAC，也没有在当前运行实例中启动 sing-box、Wintun 或写路由。严格路由已接入按 TUN 生命周期持有的动态 WFP DNS 豁免，BFE 检测/最小修复由高权限 Core 执行；真实服务安装、原生 UAC、签名安装包和网络恢复仍属于后续受控实机验收。

### 2.4 管理员入口自动纠正与兼容边界

`asInvoker` 只继承启动者令牌，不能保证由旧版最高权限计划任务、管理员终端或“以管理员身份运行”启动的 UI 自动降权。因此桌面入口在任何启动清理、单实例注册和 WebView2 初始化之前建立以下边界：

1. 若当前令牌未提升，直接进入普通启动流程。
2. 若当前令牌已提升，先尝试删除应用自有的旧 `\HypoMuxAutoStart` 计划任务。
3. 从当前交互式 Windows Shell 获取同一会话的桌面用户令牌，使用该令牌创建暂停态替代 UI。
4. 验证替代 UI 确实未提升后再恢复其线程，随后退出短生命周期的管理员入口进程。
5. 替代进程携带内部重启标记；若系统策略再次把它提升，则停止自动重试以避免循环。
6. 若无法取得普通桌面令牌，但管理员身份与交互式桌面用户相同，进入显式兼容模式：系统代理可用，TUN 继续由现有高权限 Host 阻断器禁用。
7. 若无法验证为同一用户，系统代理和 TUN 均禁用，避免写入错误账户的 `HKCU`；设置和诊断仍可使用。

安装、升级和卸载还会无条件尝试删除旧计划任务，使升级后的下一次登录不再重复进入管理员入口。Core Service 继续拒绝提升的 UI 客户端；自动纠正不会放宽既有 IPC 身份边界。

## 3. 分层

### 3.1 React Presentation

职责：

- 页面结构、Fluent 控件、图标、排版、主题和动效。
- 仅做展示级校验；权威校验来自 Go。
- 根据 ViewModel 渲染 loading/empty/error/disabled 状态。
- 订阅应用级语义事件。

禁止：

- 直接管理子进程。
- 生成 sing-box 配置。
- 写系统代理、路由、WFP、计划任务或注册表。
- 实现调度、DNS、黑名单学习、恢复或更新安装算法。
- 在任意组件中直接散用 `@wailsio/runtime`。

### 3.2 Application Services

建议服务：

- `HomeService`
- `RoutingService`
- `DiagnosticsService`
- `SettingsService`
- `DomainIsolationService`
- `UpdateService`
- `SupportLogService`
- `LifecycleService`

职责：

- 将页面意图转换为稳定用例。
- 组合 Core、Settings 和 Platform。
- 提供页面无关的 DTO。
- 管理状态机、请求取消、超时和事件合并。
- 保证停止/退出/更新的调用顺序。

### 3.3 Core Client

职责：

- 协议协商和能力检查。
- 有界消息、请求超时和取消。
- 结构化错误。
- 事件顺序和迟到事件隔离。
- Core 进程/服务连接监督。
- 重连后通过 `engine.status` 重建状态。

现有协议 v1 可继续扩展，不能返回页面名、颜色、Toast 文案或组件状态。

需要新增的领域接口：

```text
adapters.list / adapters.watch
settings.get / settings.update / settings.migrate
routing.validate / routing.import / routing.export
processes.list
wfp.status / wfp.probe / wfp.repair
system_proxy.status / system_proxy.recover
domain_isolation.list / remove / clear
support_log.list / export / open_folder
```

已有接口继续使用：

```text
engine.hello / status / start / stop / telemetry
tun.activate / status / deactivate
dns.resolve / status
diagnostic.run
health.check
host.shutdown
```

### 3.4 Platform Facade

业务层只依赖以下接口，不依赖 Wails 类型：

```go
type WindowHost interface {
    Show()
    Hide()
    Focus()
    Minimise()
    ToggleMaximise()
    Close()
    SetTheme(ThemeMode)
}

type TrayHost interface {
    Show()
    Hide()
    SetState(TrayState)
    UpdateLabels(TrayLabels)
    Destroy()
}

type DialogHost interface {
    OpenFile(OpenFileRequest) (string, error)
    SaveFile(SaveFileRequest) (string, error)
    OpenFolder(string) error
    Confirm(ConfirmRequest) (bool, error)
}

type EventBus interface {
    Publish(string, any)
}

type Updater interface {
    Check(context.Context) (ReleaseInfo, error)
    Download(context.Context, ReleaseInfo, ProgressSink) (Package, error)
    InstallAfterExit(Package) error
}

type Lifecycle interface {
    RequestQuit(QuitReason) error
    HandleWindowClose() CloseDecision
    StartHidden() bool
}
```

Wails v3 的 Window、SystemTray、Dialog、Event 和 Updater 只在 `platform/wails` 内实现这些接口。

Packaging 是构建时适配器，不能由页面调用：

```text
build/package/windows
build/signing
build/webview2
build/installer
```

## 4. 建议目录

```text
desktop/
├─ cmd/hypomux-desktop/
│  └─ main.go
├─ internal/
│  ├─ app/
│  │  ├─ home_service.go
│  │  ├─ lifecycle_service.go
│  │  └─ ...
│  ├─ coreclient/
│  ├─ settings/
│  ├─ platform/
│  │  ├─ contracts.go
│  │  └─ wails/
│  │     ├─ window.go
│  │     ├─ tray.go
│  │     ├─ dialog.go
│  │     ├─ events.go
│  │     ├─ updater.go
│  │     └─ lifecycle.go
│  └─ model/
├─ frontend/
│  ├─ src/
│  │  ├─ app/
│  │  ├─ components/
│  │  ├─ features/
│  │  │  ├─ home/
│  │  │  ├─ routing/
│  │  │  ├─ diagnostics/
│  │  │  ├─ settings/
│  │  │  ├─ domain-isolation/
│  │  │  └─ about/
│  │  ├─ platform/
│  │  │  └─ client.ts
│  │  ├─ theme/
│  │  └─ styles/
│  ├─ bindings/
│  ├─ package.json
│  └─ vite.config.ts
├─ build/
└─ wails.json / Taskfile.yml
```

旧 WPF 和旧 Python 前端已在 Wails 完整迁移后从仓库移除。

## 5. 前端设计系统

### 5.1 官方组件优先

使用：

- `FluentProvider`
- `Button`
- `Switch`
- `Checkbox`
- `Input`
- `SpinButton`
- `Field`
- `Tooltip`
- `Menu`
- `Dialog`
- `Toast` / `Toaster`
- `Badge`
- `TabList`
- `Popover`
- `ProgressBar` / `Spinner`
- `@fluentui/react-icons`

布局：

- CSS Grid 负责应用壳层、首页主区域、网卡行和响应式变化。
- Flex 负责局部工具栏、按钮组和状态带。
- Fluent tokens 负责颜色、字体、圆角、间距、焦点和交互状态。

允许自写：

- 页面布局容器。
- 标题栏命中区域。
- 紧凑导航布局。
- 首页速度视觉中心。
- 网卡遥测行。
- 少量 160-220ms 淡入、位移和数值过渡。

不自写已有成熟行为控件，不自写 Switch、Dialog、Toast、Menu、Tooltip、Input 或焦点管理。

### 5.2 视觉约束

- 不是后台 Dashboard。
- 不使用“标题 + 四统计卡”的套版。
- 首页只有一个主任务：选择模式/网卡并控制聚合引擎。
- 速度是唯一强视觉中心。
- 网卡以紧凑设备行呈现，不为每张网卡堆大卡片。
- 底部运行指标使用连续状态带。
- 浅色、深色都保持明确层级。
- 不直接复制 Microsoft 365/Teams 导航和页面。
- 不滥用玻璃、渐变、阴影和大面积留白。

## 6. 状态模型

前端使用领域状态，不使用控件状态作为真值：

```ts
type EnginePhase =
  | "stopped"
  | "starting"
  | "running"
  | "degraded"
  | "stopping"
  | "failed";

type RunMode = "proxy" | "tun";

interface HomeViewModel {
  phase: EnginePhase;
  mode: RunMode;
  message?: string;
  selectedAdapterIds: string[];
  adapters: AdapterViewModel[];
  totals: TrafficTotals;
  sessionBytes: number;
  jitterMs?: number;
  coreStatus: CoreStatus;
  canEdit: boolean;
  canStart: boolean;
  canStop: boolean;
}
```

要求：

- 启动/停止是显式异步状态，不用单一 boolean。
- `degraded` 有独立视觉。
- 停止后速度和连接立即归零，迟到遥测按会话 ID 丢弃。
- Core 断开后展示可恢复错误并重新查询状态。
- UI 不猜测 TUN 是否已清理；以 Core 状态为准。

## 7. 事件边界

Go → 前端只发布语义事件：

```text
app.engine.state
app.engine.telemetry
app.adapters.changed
app.diagnostic.progress
app.diagnostic.result
app.domain_isolation.changed
app.update.progress
app.notice
```

每个事件包含：

- schema/version；
- session ID 或 sequence；
- UTC 时间；
- 可选错误码与安全展示信息。

高频遥测可合并；生命周期、错误和回滚事件必须有序且不可丢。

## 8. 生命周期顺序

### 8.1 关闭到托盘

1. `Lifecycle.HandleWindowClose` 读取设置。
2. 返回 `HideToTray`。
3. 隐藏窗口，不停止 Core。
4. 第一次显示托盘提示。

### 8.2 完整退出

1. 禁止新操作。
2. 隐藏窗口。
3. 持久化 UI 设置。
4. 请求 Core 正常停止。
5. 等待 Core 报告网络资源已恢复。
6. 超时进入 Core 自有恢复路径，不由 React 强杀。
7. 销毁托盘。
8. 退出 Wails Host。

### 8.3 更新

1. 校验 Release 和安装包。
2. 请求 `QuitSafely(Update)`。
3. Core 完成网络恢复。
4. Host 退出。
5. 外部安装器替换文件。
6. 安装失败保留可诊断信息。

## 9. Wails v3 风险控制

- 在 `go.mod` 锁定精确 Alpha 版本，不使用浮动 `latest`。
- Wails API 只出现在 `internal/platform/wails` 和启动入口。
- 记录所用 Alpha 的提交和已知问题。
- 每次升级运行：
  - Go 编译；
  - bindings 生成；
  - 前端构建；
  - dev；
  - package；
  - 托盘/窗口/关闭；
  - DPI；
  - WebView2 缺失；
  - 安装/升级/卸载。
- 不在同一阶段同时升级 Wails、React、Fluent UI 和 Core 协议。
- `desktop/`、`engine/` 与 `protocol/` 保持独立模块边界，任一模块均可单独验证。

## 10. WebView2

- 启动器在创建 WebView 前检查运行时。
- 缺失时显示原生中文/英文提示，不允许白屏。
- 提供打开微软官方安装页或运行受信任 Bootstrapper 的明确选择。
- 安装失败返回可复制错误。
- 打包验收覆盖 Evergreen Runtime 缺失环境。

## 11. 原型边界

第二阶段只实现：

- Wails v3 工程壳。
- React/TypeScript/Vite。
- Fluent 主题。
- 标题栏和导航。
- 首页静态原型。
- 两张模拟网卡。
- 模拟启停。
- 深浅色。
- 托盘显示、隐藏和退出。

原型不得：

- 启动 `hypomux-engine.exe`。
- 写系统代理或路由。
- 请求管理员权限。
- 创建 TUN/WFP。
- 读取真实网卡。
- 实现其他页面。
- 修改现有发布入口。

## 12. 参考

- Wails v3 API：https://v3.wails.io/reference/overview/
- Wails v3 System Tray：https://v3.wails.io/features/menus/systray/
- Wails v3 Window Options：https://v3.wails.io/features/windows/options/
- Fluent UI React：https://github.com/microsoft/fluentui
