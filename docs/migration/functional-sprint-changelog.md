# 功能完整性冲刺变更记录

## 2026-07-31

- 完整设置页已接入真实 `SettingsService`：语言、关闭行为、代理端口、DNS/DoH、TUN/WFP 偏好、域名隔离、开机自启、配置目录以及旧配置迁移/回滚均可读取；普通配置执行服务端校验和原子保存。
- 完整关于页已迁移 v2.2.0 原始图标、SignPath Logo、微信/支付宝二维码及介绍、合规、签名、赞助全文。
- 更新流程已接入 GitHub Release 检查、版本比较、Release Notes、字节进度、可用 SHA-256 校验、隔离临时目录下载和安全退出后的安装器交接。
- 域名隔离管理已接入持久化与 Core 运行时遥测，支持倒计时、永久隔离、单条移除、清空和页面可见期间自动刷新。
- 托盘状态、关闭行为、单实例唤醒、`--silent` 开机启动和 WebView2 Runtime 启动前检查已接入 Wails 平台层。
- v2.x 配置兼容迁移、迁移前备份与回滚已实现；旧配置与旧域名文件均不删除。
- 首页已接入 Steam 运行提示、5 秒真实网卡差异检测、TUN 启动连通性验证和周期看门狗。
- 旧版 `ui/i18n.py` 的 302 组中英文资源已迁移，全局 `LanguageProvider` 和语言持久化已接入。
- 分流、网络体检和支持日志页的标题、控件、空状态、确认框、Toast、辅助文字及无障碍标签现已完整响应中英文切换。
- 桌面 Core Client 主动事件丢失已修复；DoH/WFP 运行时兼容路径通过真实 Core 事件执行一次受控重启，并保留用户配置语义。

## 已知未验证项

- WFP“重新检测并修复”当前只有真实只读检测；BFE 修复和动态 DNS 豁免必须经独立高权限 Core 接口完成。
- 域名隔离已经完成真实服务接线和单元测试；双物理网卡、不同出口及受限域名的实机隔离矩阵尚待验收。
- 更新、开机自启、关闭到托盘、单实例和 WebView2 缺失路径尚未完成本轮实机异常矩阵。
- 安装/升级/卸载、TUN 联网验证、周期看门狗、DoH/WFP 兼容重启和崩溃恢复仍属于后续生产级验收范围。
- Vite Chunk 警告按冲刺策略暂不阻塞功能迁移。

## 2026-07-31 · Core Service / WFP / 窗口修复

- 修复无边框窗口无法拖动：标题栏补齐 Wails v3 `--wails-draggable: drag`，身份区和空白拖动区继续声明 Windows caption，按钮区域保持 no-drag。
- 新增正式 `HypoMuxCore` Windows Service 宿主；固定 Named Pipe 使用本机 ACL、拒绝远程客户端，并校验服务 PID、客户端 PID、活动控制台会话和客户端提升令牌。
- 安装器已接入 Core Service 的创建、更新、启动、停止和删除；Wails/WebView2 UI 清单继续保持 `asInvoker`。
- WFP 设置操作改为“只读探测优先、异常时才提权修复”；高权限 Core 只尝试启动 BFE 并重新执行 `FwpmEngineOpen0`，不会重置防火墙规则。
- 严格路由接入动态 WFP DNS 豁免：规则仅允许当前 Core、选中网卡 IPv4/接口索引、UDP/TCP 53；停止、异常退出、Service 停止和崩溃时均可回收。
- 修复 Service 在 UI 空闲连接期间收到 SCM Stop 时可能阻塞的问题；现在主动关闭管道并进入统一 TUN/WFP/代理清理。
- 更新辅助程序改用 `CREATE_NO_WINDOW`，避免安装交接时闪出 CMD；只接受 `HypoMuxUpdate-*` 临时目录中的正式命名安装包，失败及安装结束后清理本次目录。

### 本轮快速验证

- `engine: go test ./...`：通过。
- `desktop: go test ./...`：通过。
- `frontend: pnpm run build`：通过；仅保留不阻塞本冲刺的 Vite chunk 体积警告。
- Wails UI 与 Core 均已生成暂存生产二进制；当前旧实例仍在运行，因此未覆盖 `desktop/bin`。

### 仍需生产级验收

- 真实管理员环境安装、覆盖升级、卸载和 Service ACL/身份负向矩阵。
- 双物理网卡下 TUN、动态 WFP DNS 豁免、DoH 降级、看门狗和路由/Wintun 残留检查。
- 签名安装包、UAC 取消/同意、WebView2 缺失及 100%/125%/150% DPI 实机矩阵。

## 2026-07-31 · 壳层、首屏与控件一致性

- 取消 DWM 强调色窗口描边，Mica/Acrylic 与主题强调色继续保留。
- 标题栏、关于页、托盘、Windows EXE 和任务栏统一使用 v2.2.0 原始 HypoMux 图标；新增可复现的 Windows 资源生成辅助程序。
- 首页模式选择与轮询开关重排为同一紧凑控制组，减少按钮、标签和开关的视觉散乱。
- 设置页原生 `Select` 全部替换为 Fluent UI v9 `Dropdown` / `Option`，统一圆角、背景、焦点和弹出层表现。
- 首页关键内容不再等待 Core 诊断完成后才显示；分流页不再等待引擎快照才展示规则；网卡元数据从 PowerShell 改为 Windows IP Helper API 进程内读取。
- 关于页保留完整 SignPath 致谢和链接正文，移除额外的“打开官方发布页”按钮。
- 自定义标题栏关闭按钮改为原生 `Window.Close` 请求；“直接退出”进入统一清理生命周期，“最小化到托盘”仅在对应设置启用时执行。

### 本轮快速验证

- `frontend: pnpm run build`：通过；仅有暂不阻塞迁移的 Vite chunk 体积警告。
- `desktop: go test ./...`：通过。
- 带正式 HypoMux Windows 图标资源的生产 EXE 已重新生成并覆盖 `desktop/bin/hypomux.exe`。
- 新构建启动后 UI 与 Core 进程均能结束，未留下本轮测试进程。

### 已知未验证项

- 窗口拖动、图标缓存、关闭到托盘与直接退出仍需在安装版和 100%/125%/150% DPI 下做最终人工矩阵。
- 首屏延迟已移除 PowerShell 和非关键请求阻塞，双物理网卡、慢 Core 与 Core 不可用场景的量化启动耗时留到发布候选里程碑。

## 2026-07-31 · 活动连接与外观收敛

- 首页启动按钮和轮询/权重调度开关移动到当前模式说明下方，启动/停止按钮始终使用当前 Fluent 主题色。
- 删除标题栏“快速切换外观预设”，保留独立深浅色切换与设置页完整外观配置。
- 发布版窗口材质收敛为 Mica 与纯色；历史 Acrylic、Tabbed Mica、Wallpaper 值启动时自动回退到 Mica。
- 窗口材质与卡片材质完全解耦：窗口选择 Mica/纯色，卡片独立选择高斯磨砂/纯色卡片；磨砂强度为 0–40px，背景图片本身不模糊。内容卡片不透明度为 0–100%，且仍只在选择自定义背景图后启用。
- 普通用户“支持日志”页面替换为“活动连接”：真实读取 Core `active_connections`，展示进程、目标域名、远端 IP/端口、实际网卡、出口策略、上下行流量和连接时长，并支持搜索与 1.5 秒实时刷新。
- Windows 进程识别使用 IP Helper `GetExtendedTcpTable` 一次性解析本轮连接，不启动 PowerShell；无法查询受保护进程时明确显示“未识别进程”。
- 后端脱敏支持日志仍完整保留，但不再占用普通用户主导航。
- 关于页保持原有产品卡片与双栏信息布局；仅将 GitHub 和检查更新按钮调整为横向等宽并统一使用主题色。

### 本轮快速验证

- `engine: go test ./...`：通过。
- `desktop: go test ./...`：通过，含连接出口分类、端点解析和 Windows TCP 端口解码测试。
- `frontend: pnpm run build`：通过；仅保留 Vite chunk 体积警告。
- 1120×800 浏览器视觉检查完成：首页、活动连接、设置和关于页均无横向溢出；检查结束后已关闭本地 9245 预览进程。
- 最新 `hypomux.exe` 与 `hypomux-engine.exe` 已覆盖到 `desktop/bin`。

### 已知未验证项

- 聚合与指定网卡连接已真实接入 Core registry；TUN 的 `direct` 出口由 sing-box 在 Core 之外直接处理，当前不会出现在活动连接列表，需后续接入受保护的 sidecar telemetry 后才能标记 Wired。
- 进程识别需在系统代理、TUN、受保护系统进程和多用户会话下完成实机负向矩阵。

## 2026-07-31 · 功能代码尾项收口

- TUN `direct` 出口改为 Core 独立 SOCKS TCP/UDP 通道：不绑定物理网卡，继续遵循 Windows 默认路由，同时进入统一活动连接与会话流量遥测；不再存在 sing-box 绕过 registry 的可见性缺口。
- WFP 兼容失败记忆按 Windows 构建版本与 Core 可执行文件路径、大小、修改时间生成设备指纹；匹配失败状态时预检直接采用兼容模式，用户显式重试会清除记忆，系统或程序更新后旧记忆自动失效。
- 更新说明支持安全渲染 Markdown 链接与裸 HTTPS 链接，点击统一通过桌面平台适配层打开，不注入远程 HTML。
- Core 新增窄范围 `recover` 命令，仅清理 `HypoMux-Tun` 默认路由和 Wintun 设备；Wails Host 新增 `--recover-network` 无界面入口，用于恢复当前用户被 HypoMux 接管前的系统代理快照。
- NSIS 覆盖升级与卸载流程会先结束旧 UI、停止 Core Service，再调用上述两个恢复入口；卸载同时移除 Service、启动项、关联、快捷方式、WebView 数据和安装目录，用户配置按重装/回滚策略明确保留。

### 本轮快速验证

- `engine: go test ./internal/proxy ./cmd/hypomux-engine`：通过。
- `desktop: go test ./internal/services .`：通过。
- `frontend: pnpm run build`：通过；仅保留不阻塞本阶段的 Vite chunk 体积警告。

### 已知未验证项

- TUN 直连 TCP/UDP、WFP 失败记忆、Service 停止及升级/卸载恢复入口已经真实接线并有单元测试，但仍需管理员环境、双物理网卡和残留网络状态的实机矩阵。
- NSIS 脚本需要在正式签名安装包中完成覆盖升级、降级、取消、卸载与用户配置保留验证。
