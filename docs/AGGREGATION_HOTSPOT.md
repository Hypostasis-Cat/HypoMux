# 聚合热点（Windows 实验性功能）

入口：桌面客户端 → 工具箱 → 聚合热点。

先在首页选好物理网卡并以 TUN 模式启动聚合，再设置热点名称、密码与频段并开启。手机正常连接 Wi-Fi，不需要代理配置或客户端。Windows 自动管理热点、地址分配与共享；不开放 HypoMux 的本机代理端口。

## 实现范围

数据路径：手机 → Windows 移动热点/ICS → HypoMux-Tun → 现有 sing-box 路由 → 现有多网卡聚合引擎。此功能共享现有引擎的多连接分流能力；没有新增远端汇合服务器，不承诺单连接带宽叠加。

桌面 `EngineService` 提供 `HotspotStatus`、`StartHotspot`、`StopHotspot`。固定的 PowerShell 工作进程在普通用户的交互会话中使用 WinRT 热点 API；TUN 的管理员操作继续由原有 Core 承担。共享上游必须是按 GUID 找到的 `HypoMux-Tun` 连接配置，绝不回退到默认物理网卡。ICS 枚举由管理员 Core 的只读 `hotspot.inspect` 接口执行，桌面通过 IPC 将结果交给热点工作进程。只有公共接口 GUID 匹配且存在一个私有共享接口，才报告共享出口已校验。这证明共享配置正确，不等于已经证明手机联网或吞吐量提升。

共享枚举使用 `EnumEveryConnection()` 方法调用，并开启严格模式，避免误读不存在的属性。用户实测确认：WinRT 启动成功后，传统 ICS 枚举仍可能为空；空列表不能证明出口配置失败。因此启动时最多等待 20 秒，检查 WinRT 为 On、本次启动后就绪的唯一热点无线网卡及其有效 IPv4 网关。网卡识别兼容 Wi-Fi Direct 描述、InterfaceType=71，以及 NdisPhysicalMedium=1/9，不依赖厂商是否在描述中标注 Wi-Fi Direct（部分 Wi-Fi 7 驱动使用 MediaTek/Qualcomm 名称）。在 ConfigureAccessPointAsync 前记录无线候选的 GUID、状态、Preferred 有效 IPv4 和默认路由；允许未 Up 的接口变为 Up，或原本 Up 但没有有效 IPv4 的接口获得有效地址。启动前已有有效地址的 Up 接口、启动前或当前带默认路由的接口均排除；先排除没有 Preferred 有效 IPv4 的候选（包括 APIPA、回环和尚在地址检测中的接口），再要求就绪候选唯一且 GUID 与地址连续三个采样（每次间隔 500ms）保持稳定；多个就绪候选时拒绝猜测，运行中固定已识别的 GUID。传统 ICS 列表为空时保留已就绪的热点，显示“热点已开启 · 出口待验证”，不声明手机经过聚合。列表非空时仅完整匹配 TUN 公共接口与本次热点私有接口才标记已校验。明确错误 GUID 立即关闭；只缺一侧或匹配 GUID 重复则按过渡状态重试，连续三次异常后关闭。此兼容分支只确认热点和本地网关就绪，不能保证 DHCP、DNS、互联网转发或聚合已经可用。

详情页显示 WinRT 状态、热点网卡 GUID、网关、就绪检测候选网卡及实际共享接口用于诊断。升级需同时更新桌面和 Core，旧 Core 缺少 `hotspot.inspect` 时会在启动热点前明确报错。

已存在的热点/ICS 会阻止启动，避免覆盖其他共享。工作进程约每三秒检查 TUN 和热点网卡；正常 ICS 检查间隔至少 15 秒，异常时约每三秒复查（检查耗时会延长间隔）。本地 CIM 和已收到回复的 ICS 查询异常允许连续重试三次，期间撤销已校验标记；管道中断或超时仍立即结束，避免请求回复错位。明确发现出口不匹配、TUN 消失、本地网关失效或父进程管道关闭时停止热点。传统 ICS 从有记录变为空记录时只撤销“出口已验证”标记。正常停止聚合时先关闭热点，再停止 TUN。退出和启动失败时尝试恢复原热点配置，清理结果分别记录热点关闭确认、原配置恢复和清理错误；关闭未确认时不修改可能仍在运行的热点配置。工作进程退出后，页面引导用户检查 Windows 移动热点设置，不提供无效的“重试关闭”按钮。密码仅通过标准输入传给工作进程，不放入命令行、通用设置或日志；启动时以 Windows 当前用户 DPAPI 加密保存到 hotspot.dat，重新打开软件可恢复，未提交的页面编辑保留在内存中；Windows 在运行期间会持久化热点配置，正常关闭后恢复原配置。工作进程被强制结束或系统断电时不能保证恢复完成，应在 Windows 设置中检查。

## 兼容性边界

Windows 10 22H2（19045）实测：`Get-NetConnectionProfile` 可见 TUN，但 WinRT `GetConnectionProfiles()` 不返回它；`GetHostNames()` 的本地 IP 信息仍暴露同一 GUID 的网卡。发现流程现增加最多约 10 秒重试，并通过该网卡的 `GetConnectedProfileAsync()` 补充查询，再校验返回配置的网卡 GUID。配置名称可能沿用上游 Wi-Fi 名称，不作为身份依据。群友已确认此直接查询返回 TUN GUID 且共享能力为 Enabled；尚待新构建的完整热点启停与手机联网复测。未取得配置时输出发现诊断，不改用默认物理上游。启动总等待预算为 150 秒，以容纳各阶段独立超时。

频段 API 使用运行时能力检测：Windows 10 2004 之前缺少 Band/IsBandSupported 时，自动频段沿用系统选择；指定 2.4/5 GHz 则明确提示改选自动或升级系统，不再因访问不存在的成员而失败。频段 API 版本依据：[Microsoft Band 文档](https://learn.microsoft.com/en-us/uwp/api/windows.networking.networkoperators.networkoperatortetheringaccesspointconfiguration.band)。此分支已通过模拟回归，仍需旧版 Windows 实机确认。

`WiFiDeviceOff` 是 Windows 返回的启动状态，不能单独证明 Wi-Fi 开关关闭。AX201 实机反馈：指定 5 GHz 时失败，自动频段下设备可获取 IP 并上网；尚未确定具体驱动或信道原因。指定 5 GHz 时对 `WiFiDeviceOff`、`RadioRestriction`、`BandInterference` 给出频段相关提示，并记录 `requested_band` 和 `start_status`；不暗中改为自动后声称 5 GHz 成功。需要强制 5 GHz 时，应对照测试 Windows 移动热点，并检查驱动与当地信道限制。Intel 的说明见 https://www.intel.com/content/www/us/en/support/articles/000029083/wireless.html 。失败后若工作进程已经退出，下一次启动会重新执行实时热点关闭状态和 ICS 检查，不再被旧的“清理未完成”标记永久锁住。系统仍有热点或共享时依旧拒绝接管。停止接口报错但 Windows 已确认 Off 时，按已经关闭处理；未确认关闭仍保留清理失败提示。

- 需要 Windows 10/11、支持移动热点的无线网卡，以及系统暴露的可共享 TUN 连接配置。系统策略、无线驱动或 WinRT 权限拒绝会明确报错，不会伪造成功。
- Windows 热点 API 本机只读能力检查已通过；自动测试会在没有 TUN 时验证真实脚本拒绝启动，测试不修改网络。
- 已收到实际手机多网卡吞吐验证；ICS DNS 与严格路由兼容性、IPv6、UDP/QUIC、MTU、频段与无线驱动组合仍需继续验收。该功能暂保留实验性标记。
- 继承现有域名/IP 分流规则；手机应用没有电脑上的进程身份，不能用桌面进程名规则识别手机应用。
- 同一无线网卡同时上网和发热点会争用无线资源。无远端稳定出口时，原有连接跨物理线路切换的连续性不受本功能保证。
- 当前没有自动启动聚合、开机恢复热点或远端带宽汇合。需要先运行 TUN，热点不在后台自动开启。

## 验收步骤

### 自动频段与吞吐诊断（2026-09-23）

- 自动模式先通过 Windows `IsBandSupported` 检查 5 GHz，支持时优先尝试；旧系统或不支持时保留 Windows 自动选择。仅在 `WiFiDeviceOff`、`RadioRestriction`、`BandInterference` 且热点明确为 Off 时回退自动并重试一次。手动 5 GHz 不回退，运行中不切换频段。配置恢复仍使用启动前的原始配置。
- 共享诊断显示 `configured_band` 和回退原因。它表示提交给 Windows 的配置；`auto` 不代表已经测得实际无线频段。发送/接收链路 Mbps 来自热点网卡驱动，不是手机实测吞吐，也不一定代表每台客户端的协商速率；驱动不提供时保持未知。
- 支持日志新增去重的 `hotspot/status_changed`，吞吐采样同时记录热点状态、策略和 `rate_unit=bytes_per_second`。原有 `download_bps` / `upload_bps` 字段为字节/秒，换算 Mbps 需乘 8 再除 1,000,000。日志不记录热点密码、SSID 或设备身份。
- 本次旧日志的两个下载采样约为 564.4 Mbps 和 316.8 Mbps，两个出口均有流量；这是引擎总吞吐，包含电脑自身流量，不能等同于手机测速结果。旧日志缺少热点状态及链路记录，无法据此确认无线或测速服务端瓶颈。
- 性能验收应在电脑和手机分别使用同一下载源、相同并发方式，关闭其他下载，记录持续吞吐。电脑清华镜像下载与手机全球网测不能直接作为同条件对照；另可对比只用有线上游与双上游，观察同一无线网卡收发争用的影响。自动化测试验证启动及回退行为，不证明手机吞吐提升。

API 依据：[Windows TetheringWiFiBand](https://learn.microsoft.com/en-us/uwp/api/windows.networking.networkoperators.tetheringwifiband)。

1. 无 TUN、代理模式、无支持的无线网卡：拒绝启动，保留现有网络设置。
2. 已有 Windows 热点或其他 ICS：拒绝接管；原热点名称、密码及出口不变。
3. TUN 运行后开启热点：手机自动获得地址、网关与 DNS；ICS 公共接口为 HypoMux-Tun。手机不用代理即可浏览 HTTPS、播放视频和下载。
4. 从手机分别测试 TCP、UDP/QUIC、DNS 和 IPv6。检查 Core 连接/字节统计，确认流量进入预期线路，而非仅验证 Windows 热点开关。
5. 比较单线路与多线路的多连接吞吐量，记录热点、上游与 CPU 瓶颈。单连接作为对照，不把多线程测速当作单连接叠加证据。
6. 停止热点、停止聚合、退出客户端、结束客户端进程、拔掉上游网卡、睡眠恢复：检查手机状态、共享出口和原热点配置。单独终止热点工作进程时确认界面显示失败并进行系统设置检查。

自动验证：桌面 `go test ./...`；前端 `pnpm run build`；`pnpm exec vitest run src/components/HotspotPanel.test.tsx src/pages/SettingsPage.test.tsx`。

## 2026-10-11 性能优化

- `HotspotConfig.adapter_id` 为可选的物理无线网卡 GUID；省略时沿用 Windows 自动选择。`HotspotAdapters` 只读枚举物理无线网卡，包括未连接的备用网卡，并标记首页选择的上游。手动选择后使用 `CreateFromConnectionProfile(TUNProfile, NetworkAdapter)`；公共接口仍是原 GUID 校验过的 TUN。网卡缺失、禁用、访问失败或驱动不支持时明确拒绝，不暗中换卡。旧加密配置无需迁移，运行中不能修改发射网卡。
- SOCKS UDP 收包及回包使用 `netip.AddrPort` / `ReadFromUDPAddrPort` / `WriteToUDPAddrPort`，消除客户端地址的逐包堆分配。现有源 IP、端口锁定、IPv6、流上限和流绑定语义保留。
- 自适应速度策略用独立的 UDP 性能表，只采集成功转发给客户端的下载负载，不将 UDP 训练结果混入 TCP。每秒采样，仍保留 15 秒预热、五个有效窗口、新流数量门槛、最低份额、5 个百分点调整上限和吞吐下降冷却；一条已有 QUIC 流不会被拆分或重新分配。
- 默认只为多网卡聚合 UDP 套接字请求突发缓冲：每个 association 的中继收发各 1 MiB，每条上游 UDP 流收发各 256 KiB。设置为 best effort，不因系统拒绝缓冲选项而中断连接。显式单网卡和 direct 保持系统默认。`HYPOMUX_UDP_TUNING=off/auto/force` 可用于开发对照，必须设置在启动 Core 的进程环境中；TCP 保留原 `HYPOMUX_TCP_TUNING` 行为。
- Core 遥测新增 `udp_profile` 与独立 UDP 策略/状态/份额；桌面性能日志保存 UDP profile、有效 TCP/UDP 策略与 UDP 状态，不加入密码或设备身份。

自动回归覆盖协议反馈隔离、空闲与无回复回退、受限候选、绑定变化、UDP lease 回收、旧策略序列、配置加密保存、网卡选择失败与前端保存/启停。`BenchmarkUDPRelayAddressRead` 对比真实环回套接字的两个收包 API：本机 Windows/amd64 测得旧 API 每包 52 B、2 次分配，新 API 为 0 B、0 次分配。该基准含发送和收包系统调用，不代表无线吞吐；运行时间波动不用于宣布手机提速。

复现：在 `engine` 运行 `go test ./internal/proxy -run '^$' -bench BenchmarkUDPRelayAddressRead -benchmem -count=3`。实机需分开比较：相同源和连接数下的电脑 TUN、自动发射热点、专用备用网卡热点；分别测试 TCP 多连接与 UDP/QUIC，同一时段只运行一个测试，记录持续吞吐、错误、频段、信号与调度策略。比较缓冲 profile 时重启对应 Core 并确认日志中的 profile；不要将重启前后改变的上游、信号或下载源视为代码收益。当前未完成这轮改动的手机实机吞吐验收。

API 参考：[指定私有发射网卡](https://learn.microsoft.com/en-us/uwp/api/windows.networking.networkoperators.networkoperatortetheringmanager.createfromconnectionprofile)、[Wi-Fi 设备枚举](https://learn.microsoft.com/en-us/uwp/api/windows.devices.wifi.wifiadapter.findalladaptersasync)。未连接的网卡通过 WinRT 枚举设备访问，可能受 Windows Wi-Fi 访问权限影响；不扫描附近网络或修改位置权限。

本轮验证：前端 60 个测试文件、415 项测试、TypeScript 检查与生产构建通过；Core 和桌面 Go 全量回归在跳过本机受限的 6 项原有 IPv6 专项后通过，Go 静态检查通过。跳过的是 Core 的 IPv6 ICMP、DNS UDP/TCP、DoH TLS、DNSPod IPv6 winner，以及桌面的 IPv6 ICMP、Core IPv6 环回恢复；首次运行与单独复查均出现系统访问拒绝、超时或 ICMP unavailable，用户确认当前无 IPv6。代理包全量回归没有跳过测试。真实 WinRT 类型投影和只读设备枚举通过，没有开启真实热点。Race 检测未运行：本机 CGO_ENABLED=0 且无 C 编译器；不将普通回归视作 race 检测结果。手机实机吞吐仍待对照。

## 完整版使用体验（测试分支）

- 工具箱卡片提供快捷开关与实时设备数量；使用已保存配置启动，没有密码时进入详情设置。
- 详情页可在聚合未启动时独立保存配置、生成 16 位随机密码、复制密码。运行时禁止修改配置，避免手机连接信息与实际热点不一致。
- 已连接设备详情来自 Windows GetTetheringClients；设备名可能缺失，MAC 可能是手机的随机地址。读取失败保留热点运行并标记明细不可用，不推算每台设备流量。
- 共享诊断提供状态采样时间与复制按钮；复制内容不包含热点密码或设备列表。
- 启动中的会话即时公开，返回页面能读到 starting 状态；显式关闭成功后清除旧失败状态。正常运行的未校验出口提示作为状态展示，不冒充网络错误。
- 用户已在手机端验证多网卡同时有流量；更换校园网上游为手机热点后速度显著提升。此反馈支持当前环境下的可用性，不代表已完成所有驱动、IPv6 和睡眠恢复组合的兼容测试。

设备列表接口参考：https://learn.microsoft.com/en-us/uwp/api/windows.networking.networkoperators.networkoperatortetheringmanager.gettetheringclients

## 正式版候选：连接与异常恢复

- 首次打开详情页自动生成 16 位随机密码，不会自动开启网络；保存或开启时沿用 DPAPI 加密保存。
- 运行中的热点可以展开 Wi-Fi 二维码。二维码在本机生成，使用 ZXing Wi-Fi 格式并转义分隔符；关闭热点或状态查询失败时隐藏。未运行时不展示尚未生效的配置码。
- 详情页与卡片共享启停/保存操作锁，离开页面不会释放正在运行的操作；另一个入口不会排队提交第二次启停。
- 关闭请求立即显示 stopping；迟到的运行状态不会把它改回 running。关闭期间管道结束但清理已确认完成时显示正常关闭；实际清理失败仍报错。
- 不自动迁移已有连接或恢复意外关闭的热点。TUN 消失、出口明确不匹配、热点网卡失效时沿用关闭策略，避免错误地共享其他上游。

### 自动测试与实机验收边界

已加入自动回归：特殊 SSID/密码的二维码转义、初始密码、扫码展示与隐藏、跨页面互斥、失败后重试、连续五次子进程启停、关闭期间管道中断、保留真实清理失败。原有 TUN 消失、私网网关失效、错误出口、控制进程崩溃测试继续保留。自动测试不修改测试机器的热点和上游网络。

以下需要测试者在安装候选包后逐项记录，不得以模拟通过替代实机结果：

| 场景 | 预期 | 验收状态 |
|---|---|---|
| Android / iPhone 扫码，含中文热点名及特殊字符密码 | 正确连接同一热点；不开代理 | 待实机 |
| 2.4 / 5 GHz，不同无线驱动 | 支持的组合正常开启；不支持时明确提示 | 待实机 |
| 电脑同时连 Wi-Fi 和开热点 | 持续联网，吞吐与上游能力相符 | 待扩大样本 |
| 连续开关及启动中切换卡片/详情 | 无重复启动，关闭后可再次开启 | 已模拟，待实机 |
| 睡眠/唤醒、TUN 重启 | 正确关闭或报告实际状态，能重新开启 | 待实机 |
| 上游拔插或校园网断线 | 不伪报热点出口已验证，不错误共享其他网卡 | 待实机 |
| 退出、强制结束桌面、强制结束控制进程 | 正常退出清理；异常结束明确提示系统检查 | 已模拟，待实机 |
| 长时间视频、下载、游戏与 UDP/QUIC、IPv6 | 无持续掉线；记录网卡、系统版本及失败组合 | 待实机 |

已报告的手机多网卡吞吐测试保持有效，但在上述项目完成前保留实验性标记。候选开发完成不等于所有设备兼容性验收完成。

## 2026-10-22 系统自动关闭与启动取消

- Windows 移动热点默认开启“节能”：一段时间没有设备连接会自动关闭热点。启动前若 `NetworkOperatorTetheringManager.IsNoConnectionsTimeoutEnabled()` 为真，工作进程调用 `DisableNoConnectionsTimeout()`，并在清理时调用 `EnableNoConnectionsTimeout()` 恢复；用户原本已关闭节能时不做修改，也不会在退出时开启。API 不存在（运行时检测）或调用失败时继续开启热点，只在 `idle_timeout` 记录 `unsupported` / `error`。这是系统级设置，工作进程被强制结束或断电时可能停留在已关闭状态，需要在 Windows 移动热点设置中检查“节能”。
- 运行中检测到 Windows 将热点关闭（非 HypoMux 请求）时，状态为 `stopped` 并带 `stop_reason=windows_off` 与说明：节能已禁用时提示系统设置、Wi-Fi 开关或睡眠等原因，否则提示节能自动关闭。聚合不受影响，可直接重新开启。原热点配置照常恢复。
- 启动最长占用 150 秒生命周期锁。现在关闭热点、停止聚合和退出客户端会先取消正在进行的启动：工作进程收到 stdin 关闭后按原有路径撤销热点和共享，再由关闭操作等待清理结果。等待生命周期锁或检查前置条件阶段被取消时不会创建热点会话。前端在启动中允许关闭开关一次，用于取消；取消不显示为错误。
- 支持日志的 `hotspot/status_changed` 新增 `idle_timeout` 与 `stop_reason`；复制诊断同样包含这两项。

自动回归：取消挂起中的工作进程后状态为 stopped；关闭请求可取消等待生命周期锁的启动；节能禁用、恢复、用户已关闭节能和旧系统不支持四种路径；前端启动中取消与系统关闭提示。真实 WinRT 节能 API 仅在本机（Windows 26300）做过只读方法存在性检查，未在测试中修改系统设置；空闲 5 分钟不自动关闭、旧版 Windows 10 行为及取消时序仍需实机验证。热点脚本通过命令行传入，当前约 28.7K/32.7K UTF-16 单位，后续扩展需注意余量。

## 2026-10-22 监控开销优化

- 运行中仍每 3 秒检查热点状态、发布状态，ICS 校验节奏不变。网卡存活改用 .NET `NetworkInterface` 轻量检查 TUN 与热点网卡是否 Up；需要 CIM 的完整检查（网卡、默认路由、有效 IPv4、链路速率）每 10 秒一次。
- 轻量检查只能让完整检查提前：任一网卡不是 Up、或查询异常时，本轮立即执行完整检查，是否关闭热点仍只由完整检查决定，判定规则与此前一致。TUN 消失、地址失效等异常的发现时间最长从约 3 秒变为约 12 秒；网卡掉线通常仍在 3 秒内由轻量检查触发完整检查。
- 一次完整检查中 TUN 与热点网卡判断共用一次 `Get-NetAdapter -IncludeHidden`（此前每轮两次）。链路速率随完整检查更新。
- 本机实测（只读）：旧每轮约 198 ms；轻量检查约 11 ms，完整检查约 115 ms。按 12 秒窗口，监控耗时从约 790 ms 降至约 150 ms。不影响热点数据转发路径，不代表手机吞吐变化。

自动回归：完整检查只查询一次网卡且快照不跨检查复用、TUN 停止仍立即拒绝、10 秒节流与存活异常提前检查、真实系统的存活探测。热点脚本约 30.2K/32.7K UTF-16 单位，余量已不多。
