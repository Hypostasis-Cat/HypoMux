# HypoMux IPv6 适配与验收方案

本次适配将现有 IPv6 TCP 和 UDP 出站能力扩展为双栈与 IPv6-only 出口支持。内部回环端口继续使用 IPv4；公网出站、DNS、Windows 路由与状态按地址族处理。验收以实际行为和测试证据为准，跳过的实机测试不能计为通过。

**2026-10-04 状态：公网 IPv6 HTTPS/DoH、普通代理、严格系统 TUN 的 TCP/UDP、MTU 1280 下的 4 MiB HTTPS 传输、正常停止、侧车及 Core 崩溃清理已通过；完整实机验收仍未通过。** WLAN 已有真实 IPv6 出口。现有 Mihomo 的严格路由会阻断直连 IPv6 TCP/UDP 53；经用户明确授权的临时测试窗口内，HypoMux UDP DNS、原生 WFP TCP/UDP DNS 与删除检查通过，随后确认严格路由恢复。该网络 DNS 对 `ipv4only.arpa` 返回有效响应但没有 AAAA，当前接入方式未提供可用 DNS64 前缀。用户确认目前没有额外 IPv6-only/NAT64 网络或可控制的 IPv6 服务端；这些场景及睡眠、下游 ICMPv6 PMTU、大 UDP 仍待验收。验收使用国内阿里、腾讯与清华 TUNA 端点。

## 实施顺序

1. 网卡与配置：IPv4 或 IPv6 至少一种有效；IPv6 接口索引、地址状态、DNS、默认路由和跃点独立读取；地址变化参与运行时刷新。
2. DNS：传统 UDP/TCP 和 DoH 支持匹配的 IPv4/IPv6 源地址与接口；DNS 缓存与连接池隔离完整绑定；IPv6-only 自动策略优先网络 DNS 以保留 DNS64；显式加密 DNS 策略保持原语义。
3. 连接：按可用源地址查询 A/AAAA，保留多个候选，错峰尝试并取消失败竞速；网络连接失败能够跨协议回退，所有尝试保持网卡绑定与资源上限。
4. TUN/WFP：DNS 出口、源地址和严格路由放行支持 IPv6；选择无对应协议能力的线路时明确失败；IPv4 回退状态对用户可见，清理包含两种协议路由与过滤器。
5. 调度与诊断：地址族故障独立记录；IPv6 延迟和连接诊断、状态展示与地址变化刷新；验证大包、UDP 和取消行为。
6. NAT64：只使用所选网络 DNS 提供的实际 DNS64 前缀，不假定固定前缀；覆盖 IPv6-only 上的 IPv4 字面目标与 IPv4-only 域名。

实现细节：每种协议最多保留 8 个目标地址，每张网卡最多同时尝试 2 个 TCP 连接，间隔 250 ms；另一种协议仍在解析时为它保留连接名额。连接总预算包含 DNS 阶段，取消后关闭落败的数据连接。DNS 查询按源地址、IPv4/IPv6 接口索引及网络 DNS 隔离；共享解析仅在最后一个等待者离开后取消。DoH 连接池中的建连仍受查询超时限制。

IPv6-only 域名优先尝试原生 AAAA；必要时在 2 秒后或原生候选全部失败后，查询 A 并使用真实网络前缀合成地址。DNSPod 没有可依赖的固定 IPv6 接入地址，使用绑定到所选网卡的传统 DNS 解析 `doh.pub` 后进行证书校验的 HTTPS 查询；用户域名仍使用加密查询。双栈 DNSPod 允许 IPv4 或 IPv6 路径胜出。未指定记录类型的 `dns.resolve` 在 IPv6-only 出口优先 AAAA，必要时返回 A 供连接层转换。

现有 IPv4 MTU 探测/设置工具继续保留其明确的 IPv4 范围。IPv6 数据路径使用操作系统 PMTU 处理；真实链路的大包与 ICMPv6 Packet Too Big 行为属于下面的实机验收项。

## 自动化验收标准

| 项目 | 必须满足的行为 |
| --- | --- |
| 配置兼容 | 原有 IPv4 配置通过；IPv6-only 配置通过；空、重复、不安全地址拒绝 |
| DNS | IPv6 UDP/TCP 与 TLS DoH 实际回环查询通过，TCP 回退、取消、源地址绑定及缓存隔离通过 |
| TCP | IPv4 连接失败而 IPv6 正常时成功；反向回退成功；多个地址重试；慢 DNS 不阻塞另一协议；失败竞速连接关闭 |
| UDP | IPv6 流稳定、源地址绑定、请求回复头正确；能力不匹配不走未选择网卡 |
| NAT64 | RFC 6052 支持的前缀长度合成正确；RFC 7050 前缀发现正确；未发现前缀时明确失败 |
| Windows/TUN | IPv6 默认路由、作用域 DNS、源地址绑定配置、WFP V6 过滤规则、两种协议恢复和降级状态测试通过 |
| 界面 | IPv6 地址/状态可见；仅 IPv6 地址变化会刷新；IPv4 回退提示可见 |
| 回归 | engine 与 desktop 全量 Go 测试、vet、前端测试与生产构建通过；竞态检查在支持的编译环境通过 |

## 实机验收标准

必须记录 Windows 版本、网卡、可用地址族与实际运行方式。分别验证公网 IPv4、IPv6、双栈不对称故障、IPv6-only、DNS64/NAT64、TUN 严格路由、睡眠/网络切换及退出后的路由/过滤器恢复。不能用回环或模拟测试替代公网、真实 NAT64 和系统恢复验收。当前机器缺少的网络条件作为未验收项保留，不宣称完整验收通过。

| 场景 | 操作与验收结果 | 当前状态 |
| --- | --- | --- |
| 公网 IPv4 基线 | 普通代理和 TUN 下访问 A-only、双栈域名及 IPv4 字面目标，TCP/UDP 正常且选择的出口一致 | 普通代理/TUN 池与真实系统 TUN TCP/UDP、双栈及官方 IPv4 专用域名、正常及崩溃清理通过 |
| 公网 IPv6 | 通过指定网卡的 IPv6 源地址访问 AAAA 域名、IPv6 字面目标、DoH；证书和实际源地址验证通过 | TCP/TLS/DoH、SOCKS/HTTP CONNECT/TUN 池及公网 UDP NTP 通过；授权临时窗口内 UDP DNS 53 也通过 |
| 双栈不对称故障 | 分别断开一张网卡的 IPv4/IPv6 路径，另一协议成功；故障状态仅影响对应协议，恢复后可重新使用 | 真实 WFP 测试进程内双向 TCP 阻断/回退/解除阻断后恢复通过；全接口变化与睡眠仍待验收 |
| IPv6-only，无 NAT64 | 原生 IPv6 TCP/UDP 与 DNS 正常；IPv4 目标明确失败，不逃逸到其他网卡 | 待验收 |
| IPv6-only，DNS64/NAT64 | 使用该网络 DNS 发现前缀；IPv4 字面 TCP/UDP、受控 A-only HTTPS 域名均成功；UDP 回复保留原 IPv4 目标 | 当前网络 DNS 有效回复无合成 AAAA；缺少可用 DNS64/NAT64 网络，待验收 |
| TUN/WFP 严格模式 | IPv6 DNS、TCP/UDP 只能走指定出口；IPv4 回退提示持续可见；IPv6-only 线路不生成 IPv4-only 回退配置 | 严格系统 TUN IPv6 HTTPS/UDP、DoH 通过；授权临时窗口内原生 WFP DNS 的注册、TCP/UDP 请求与清理通过；Mihomo 严格路由正常设置下仍阻断直连 DNS 53 |
| 地址变化与睡眠 | 休眠唤醒、接口索引/源地址变化后重建绑定，缓存和连接不复用旧出口 | 已补齐运行时自动重绑定；真实 Core 的 IPv6 回环恢复、旧连接清理通过；物理睡眠/接口重编号及新恢复路径的公网补测仍待验收 |
| IPv6 PMTU/大包 | 在有 MTU 瓶颈的真实链路上传输大文件和 UDP，允许必要 ICMPv6，观察无持续黑洞或错误切换出口 | 真实系统 TUN IPv6 MTU 1280 下 4 MiB HTTPS 传输通过；下游瓶颈触发 ICMPv6 Packet Too Big 和大 UDP 仍待验收 |
| 退出、崩溃与竞争 VPN | 记录前后两种协议的路由和 WFP 状态；正常退出及异常终止清除 HypoMux 资源，其他 VPN 资源保留 | 严格系统 TUN 正常停止、侧车崩溃、Core 崩溃及其 Job 自动结束侧车/恢复路由通过；原生动态 WFP 关闭/拥有进程崩溃清理通过 |

每项保存执行时间、运行模式、Windows 版本、接口名称/两种协议索引、源地址、DNS、连接结果与前后路由/过滤器快照。完整验收要求上述项目全部通过；测试代码通过和网络脚本返回 0 均不能单独替代系统矩阵。TUN/WFP 系统验收通过单独提权的测试 Core 执行；独立客户端使用真实系统路由。当前验证不覆盖已安装服务客户端的签名/信任路径。

### 公网网络验收入口

在项目根目录使用 PowerShell，安装项目要求的 Go 版本后执行。脚本只启动回环测试服务和源地址绑定的外部连接，不修改系统代理、路由或 WFP。

```powershell
# 双栈网卡也会强制只使用 IPv6 源地址进行这组公网检查。
.\tools\ipv6-acceptance.ps1 -Adapter "网卡名" -DNSPolicy alidns

# 同时要求真实 SOCKS IPv6 UDP DNS 请求/回复通过。
.\tools\ipv6-acceptance.ps1 -Adapter "网卡名" -RequireUDP

# 使用国内双栈 NTP 服务验证 UDP 123，避开其他软件对 DNS 53 的阻断。
.\tools\ipv6-acceptance.ps1 -Adapter "网卡名" -RequireUDP `
  -IPv6UDPProtocol ntp -IPv6UDP "ntp.tuna.tsinghua.edu.cn:123"

# 在真实 DNS64/NAT64 网络中提供该网络的 IPv6 DNS，以及受控 A-only HTTPS 域名。
.\tools\ipv6-acceptance.ps1 -Adapter "网卡名" -RequireNAT64 `
  -DNSServers "该网络的IPv6-DNS地址" -IPv4OnlyDomain "受控的A-only域名" `
  -Output ".go-cache-local/ipv6-nat64-acceptance.json"
```

默认 NAT64 TCP/UDP 目标分别为 `223.5.5.5:443` 和 `223.5.5.5:53`，TCP 校验 `dns.alidns.com` 证书（可用 `-NAT64TCP`、`-NAT64UDP`、`-NAT64ServerName` 替换），UDP 验证 DNS 回复和 SOCKS 原目标。受控域名须有 A、没有 AAAA，且 HTTPS 证书有效。自动选择只考虑活动以太网/Wi-Fi；不要用 Teredo 或 HypoMux 自身的 TUN 代替目标物理网络。脚本返回 `0` 表示本次指定网络检查通过，`1` 表示失败/跳过/未执行，`2` 表示缺少前提。报告始终保留 `full_acceptance=pending_system_matrix`，直到独立系统矩阵完成。

NTP 模式通过所选网卡的 IPv6 DoH 动态解析 AAAA，实际 UDP 连接保持物理源地址绑定；核对 NTP 服务端模式、有效层级、请求时间戳关联及 SOCKS 原目标。只发送授时查询，不修改系统时钟。[TUNA 服务说明](https://tuna.moe/help/ntp/)确认该端点支持 IPv4/IPv6 双栈。

## 标准依据

- [RFC 8305 双栈连接与 NAT64](https://www.rfc-editor.org/rfc/rfc8305.html)
- [RFC 6052 IPv4 地址嵌入](https://www.rfc-editor.org/rfc/rfc6052.html)
- [RFC 7050 NAT64 前缀发现](https://www.rfc-editor.org/rfc/rfc7050.html)
- [RFC 8201 IPv6 路径 MTU](https://www.rfc-editor.org/rfc/rfc8201.html)
- [Windows 网卡地址元数据](https://learn.microsoft.com/en-us/windows/win32/api/iptypes/ns-iptypes-ip_adapter_addresses_lh)

## 2026-10-03 原始验收记录

测试日期为 2026-10-03，Windows `10.0.26300.0` / AMD64，Go `1.26.6`。竞态检测使用仓库已有 LLVM MinGW Clang，通过 `CGO_ENABLED=1` 和 `CC` 指向其 `x86_64-w64-mingw32-clang.exe`。验证时基于提交 `e66016e129c321557b48935629521e917f17b76e` 的工作区改动，经过验证的实现对应本组 IPv6 适配提交；发布到主分支不改变上文“完整实机验收尚未通过”的状态。

| 检查 | 最终结果 | 本地证据 |
| --- | --- | --- |
| engine 全量 `go test -race ./... -count=1 -timeout 120s` | 283 个顶层测试通过，0 失败，2 个公网网络测试因缺少显式环境跳过 | `.go-cache-local/ipv6-engine-tests.jsonl` |
| desktop 全量 `go test -race ./... -count=1 -timeout 180s` | 显式启用本机可用集成检查后，423 个顶层测试通过，0 失败，3 个安装环境检查跳过 | `.go-cache-local/ipv6-desktop-tests.jsonl` |
| Windows 原生只读检查 | 默认 DNS 出口、45 条活动路由快照、TUN 只读预检，3 项通过 | `.go-cache-local/ipv6-native-readonly-tests.txt` |
| 可用实机集成检查 | 真实引擎握手、选定以太网诊断、IPv4 MTU、原生 WLAN API、普通代理启停恢复，5 项通过；也已纳入最终全量竞态运行 | `.go-cache-local/ipv6-available-integration-tests.txt`、`ipv6-real-proxy-lifecycle.jsonl` |
| Windows 代理实际恢复 | 普通代理测试及最终全量运行前后，`ProxyEnable` / `ProxyServer` / `ProxyOverride` 的存在性、值和类型全部一致 | `.go-cache-local/ipv6-real-proxy-restoration.json`、`ipv6-final-proxy-restoration.json` |
| 公网 IPv4 数据路径 | 真实引擎 IPC、绑定网卡的 DoH、普通 SOCKS 域名/字面目标、HTTP CONNECT、TUN TCP/UDP 池；11 条检查记录通过，TLS 证书和 DNS 载荷验证有效 | `.go-cache-local/ipv4-public-regression.json` |
| 前端全量测试 | 340 个测试通过，0 失败，0 跳过 | `.go-cache-local/ipv6-frontend-tests.json` |
| 静态检查与构建 | 两个模块 `go vet ./...`、engine/desktop 构建、Wails 绑定生成、前端 TypeScript/生产构建通过 | 本地命令均退出 0；验证二进制在 `.go-cache-local` |
| 公网 IPv6 网络脚本 | 返回 2，`network_status=blocked`，没有执行公网测试 | `.go-cache-local/ipv6-network-acceptance.json` |
| 真实 DNS64/NAT64 网络脚本 | 返回 2，`network_status=blocked`，没有执行 NAT64 测试 | `.go-cache-local/ipv6-nat64-acceptance.json` |

Go 数量按不含 `/` 的顶层测试统计，子测试未重复计数。前端按具体测试用例统计。desktop 最终全量运行已显式启用 8 项环境集成检查；剩余跳过项目仅为 NSIS 安装目录、已安装服务的信任客户端路径、官方签名安装器校验，不能算通过。公网 IPv6/NAT64 的 2 项 engine 测试仍因缺少对应网络跳过。

以太网实机诊断收到全部 10 次 ICMP 回复，绑定网卡的 `223.5.5.5:443` TCP 连接成功；只读 IPv4 MTU 检查得到 1500，WLAN 只读 API 枚举到 1 个接口。公网 IPv4 数据检查通过引擎回环代理访问 `dns.alidns.com`，校验证书与 DoH DNS 响应；UDP 通过 TUN 池访问 `223.5.5.5:53` 并核对原目标回复头。此检查使用池端口，未激活系统 TUN，因此不替代系统路由/WFP 验收。本机一次性验证脚本与接口参数保存在 `.go-cache-local/verify_ipv4_regression.py` 和 `ipv4-acceptance-adapter.json`。

机器可读汇总位于 `.go-cache-local/ipv6-local-verification.json`，记录基线提交、最终测试数量及两个验证二进制的 SHA-256。前端共 57 个测试文件。

真实 IPv6 回环覆盖 UDP/TCP DNS、证书校验的 TLS DoH、ICMPv6、SOCKS TCP/UDP 和源地址绑定；TCP 256 KiB、UDP 1200/8192 字节通过。NAT64 算法使用独立 RFC 前缀向量覆盖全部 6 种允许长度，转换拨号、取消、缓存隔离和协议回复身份通过自动化测试。这些证据验证实现行为，不证明公网翻译设备或 IPv6 系统路由已经验收。

复现本地回归：

```powershell
# 竞态检查需先设置可用的 C 编译器 CC，并将其 bin 加入 PATH。
$env:CGO_ENABLED = "1"
Push-Location engine
go test -race ./... -count=1 -timeout 120s
go vet ./...
go build -trimpath -o ..\.go-cache-local\ipv6-hypomux-engine.exe ./cmd/hypomux-engine
Pop-Location

Push-Location desktop
wails3 generate bindings -clean=true -ts -i
Pop-Location

Push-Location desktop/frontend
npm run test
npm run build
Pop-Location

Push-Location desktop
go test -race ./... -count=1 -timeout 180s
go vet ./...
go build -trimpath -o ..\.go-cache-local\ipv6-HypoMux.exe .
$env:HYPOMUX_RUN_TUN_PREFLIGHT_TEST = "1"
go test ./internal/services -run '^(TestReadOnlyWindowsDefaultDNSEgress|TestReadOnlyNetworkRouteSnapshot|TestRealWindowsTunPreflightIsReadOnly)$' -count=1 -v
Remove-Item Env:HYPOMUX_RUN_TUN_PREFLIGHT_TEST
Pop-Location
```

本机最终全量运行还设置了以下开关，使用本次构建的真实引擎和选定以太网。该组检查包含短暂启用系统代理并恢复；IPv4 MTU 开关需要对应网卡有 IPv4 地址。公网 IPv6 检查仍使用前文独立脚本。

```powershell
$env:HYPOMUX_ENGINE_PATH = (Resolve-Path .go-cache-local/ipv6-hypomux-engine.exe).Path
$env:HYPOMUX_NETWORK_TEST_ENGINE = $env:HYPOMUX_ENGINE_PATH
$env:HYPOMUX_NETWORK_TEST_ADAPTER = "以太网"
$env:HYPOMUX_RUN_DIAGNOSTIC_TEST = "1"
$env:HYPOMUX_RUN_NETWORK_TEST = "1"
$env:HYPOMUX_MTU_SMOKE_ADAPTER = "以太网"
$env:HYPOMUX_TEST_WLAN_READONLY = "1"
$env:HYPOMUX_RUN_TUN_PREFLIGHT_TEST = "1"
Push-Location desktop
go test -race ./... -count=1 -timeout 180s
Pop-Location
```

`.go-cache-local` 属于忽略的本地验证产物；其中的可执行文件没有安装、签名或发布。对外归档网络证据前应核对其中的本机地址和接口信息。下一步是取得上述真实网络条件，运行公网脚本与系统矩阵；在此之前保留“完整验收未通过”的状态。

## 2026-10-04 IPv6 实网补充验收

Windows 10.0.26300.0 / AMD64、Go 1.26.6；使用活动 WLAN 的首选公网 IPv6，保留已有 Mihomo TUN。绑定配置故意不提供 IPv4 源地址，以验证应用的 IPv6-only 出口选择；这不等同于物理网络已经是 IPv6-only。

| 检查 | 结果 | 本地证据 |
| --- | --- | --- |
| 国内公网 IPv6 | 阿里 IPv6 DoH、域名/字面 TLS、源地址绑定通过 | `.go-cache-local/ipv6-wifi-final-public.json`（同次 UDP 未通过，报告整体失败） |
| 公网 IPv6 UDP | 清华 TUNA UDP 123 返回有效 48 字节 NTP 响应，物理 IPv6 源地址、请求时间戳关联和 SOCKS 原目标通过；同次 TLS/DoH 也通过 | `.go-cache-local/ipv6-public-ntp-acceptance.json`，本次指定检查整体通过 |
| 真实 Core 代理数据路径 | 普通 SOCKS、HTTP CONNECT、TUN TCP 池 10 项检查通过；UDP 未通过，整体保留失败 | `.go-cache-local/ipv6-public-proxy-regression.json` |
| 真实严格系统 TUN | 独立 OS 路由客户端从实际 HypoMux IPv6 TUN 地址访问腾讯 HTTPS，证书、所选 WLAN 的 IPv6 连接及约 122 KiB 正文通过 | `.go-cache-local/ipv6-system-acceptance/ipv6-system-tun-acceptance.json` |
| IPv4 系统回归 | 同一生产配置生成器强制 IPv4 出口；独立客户端从实际 IPv4 TUN 地址访问腾讯 HTTPS（130,802 字节）和阿里 UDP NTP；正常停止、侧车与 Core 崩溃清理通过 | `.go-cache-local/ipv4-system-baseline/ipv6-system-tun-acceptance.json`，14 项检查通过，报告明确 `address_family=IPv4` |
| IPv4-only 域名系统回归 | 对提供方声明的 `mirrors4.tuna.tsinghua.edu.cn` 独立验证 A 有效/AAAA 为 NOERROR 无记录，证书校验 HTTPS 经 IPv4 系统 TUN 返回 200/22,488 字节，UDP 与正常/侧车恢复同时通过 | `.go-cache-local/ipv4-only-domain-evidence.json`、`ipv4-system-a-only-acceptance/ipv6-system-tun-acceptance.json`，11 项检查通过 |
| IPv4-only 域名普通代理回归 | 真实 Core 普通 SOCKS 域名请求及 HTTP CONNECT 域名请求均完成证书校验、200/22,488 字节；Core 遥测显示实际 IPv4 目标与所选 WLAN，正文 SHA-256 与系统 TUN 同端点记录一致 | `.go-cache-local/ipv4-a-only-proxy-acceptance.json`，两个实际请求通过 |
| 严格系统 TUN UDP | 独立未绑定出口/未设置代理的 OS UDP 客户端从实际 HypoMux IPv6 TUN 地址访问清华 NTP；Core 遥测确认 UDP 经过所选 WLAN IPv6 池，48 字节请求/回复通过；正常与异常恢复同时通过 | `.go-cache-local/ipv6-system-udp-acceptance/ipv6-system-tun-acceptance.json`，11 项检查通过 |
| IPv6 最小 MTU 与大文件 | 仅将本次拥有的临时 HypoMux TUN IPv6 MTU 调整到 1280；独立客户端经所选 WLAN 下载清华镜像站 4,194,304 字节 HTTPS Range，证书、206/Content-Range、精确长度通过并记录 SHA-256；最终复核同时包含 UDP、正常停止、侧车和 Core 崩溃清理 | `.go-cache-local/ipv6-system-mtu-acceptance/ipv6-system-tun-acceptance.json`，最终 15 项检查通过；原 12 项报告保存在 `ipv6-system-mtu-before-core-crash.json` |
| 停止与异常恢复 | 同一系统测试内正常停止、再次启动、终止其拥有的 sing-box 侧车、检测失败状态、清理通过；物理/竞争 VPN 两族路由保留 | 同上，10 条检查记录 |
| Core 崩溃清理 | 侧车异常后显式重启池，重新激活严格 TUN，再强制结束本次测试 Core；Windows Job 自动结束其侧车，临时网卡消失，物理/竞争 VPN 两族策略路由恢复 | `.go-cache-local/ipv6-system-core-crash-acceptance/ipv6-system-tun-acceptance.json`，14 项检查通过 |
| 原生 WFP | 真实 IPv6 TCP/UDP 放行过滤器注册、源地址/接口约束、系统事件匹配、Close 后删除通过；53 端口连接仍被外部规则拒绝 | `.go-cache-local/ipv6-wfp-latest-acceptance.txt`、`ipv6-wfp-latest-dns-events.xml` |
| WFP 异常清理 | 仅终止拥有的辅助测试进程，OS 移除其实际动态 IPv6 过滤器通过；此项使用回环资源，不是公网连通证明 | `.go-cache-local/ipv6-wfp-crash-cleanup.txt` |
| 真实双栈故障回退 | 原生 IPv4/IPv6 TLS 基线通过；分别只阻断测试进程在所选 WLAN 的 TCP 443，另一协议完成普通 SOCKS TLS；关闭拥有的动态规则后原协议立即恢复 | `.go-cache-local/ipv6-wfp-dualstack-acceptance.txt`，两种故障子测试通过 |
| 授权传统 DNS/WFP 验收 | Mihomo TUN 保持开启，临时关闭其严格路由；HypoMux 公网 UDP DNS 和原生 WFP 的 UDP/TCP DNS、源地址/接口约束、Close 后删除全部通过；finally、外层控制与独立辅助进程保护恢复，最终确认严格路由开启 | `.go-cache-local/ipv6-dns64-window-result.json`，两个实际 Go 测试及其子测试通过 |
| DNS64 网络发现 | 授权窗口内 20 条查询收到 13 个有效回复、7 个超时；网络 link-local DNS 的 TCP 对照域名返回真实 AAAA，`ipv4only.arpa` 返回 NOERROR/NODATA，无可发现前缀；不能由此断言上游不存在其他 NAT64 机制 | `.go-cache-local/ipv6-native-dns64-probe.json`；原阻断记录保存在 `ipv6-native-dns64-probe-before-approved-window.json` |
| engine 全量竞态 | 283 个顶层测试通过、0 失败、5 个显式实网测试跳过；新双栈故障案例另行执行 | `.go-cache-local/ipv6-engine-final-real-network.jsonl` |
| desktop 全量竞态 | 415 个顶层测试通过、0 失败、12 个显式环境测试跳过；其中生产 TUN 配置准备已另行显式执行并通过 | `.go-cache-local/ipv6-desktop-final-real-network.jsonl` |
| 原主分支 CI 报错修复 | Windows 缓存断言的 DNS 超时从 200 ms 调整到 2 s，保留清洁退出/缓存断言，实际 bundled FakeIP 连续重启 20 轮通过；提交 `8541a3d` 的两个 CI 均成功 | GitHub Validate Go Engine / Build Desktop |
| 公网 UDP/MTU 检查 CI | 提交 `3adf49b` 的 Go Engine 与 Build Desktop 均成功 | [Go Engine](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/37171201003)、[Build Desktop](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/37171201011) |

原 WFP 事件显示 `Meta / block ipv6 dns` 在 `ALE_AUTH_CONNECT_V6` 拒绝 TCP 和 UDP 53；同事件也记录 HypoMux 的源地址/接口绑定 permit 已匹配，原失败记录保留。授权窗口通过运行时 API 显式保持 `tun.enable=true`、临时设置 `tun.strict-route=false`，未修改代理配置文件或直接删除其他程序的 WFP 规则。API 在 false 时省略该字段，检查需按 false 解释；最终读取控制器确认 TUN 与严格路由均开启。网络 DNS 的真实 NOERROR/NODATA 响应使本次 NAT64 前提检查得到明确结果，不能用手工前缀替代。

双栈故障用 `TestRealDualStackTCPFaultFallback` 执行，需管理员权限、`HYPOMUX_RUN_WFP_IPV6_NETWORK_TEST=1`，以及包含两族物理源地址与接口索引的 `HYPOMUX_IPV6_ACCEPTANCE_CONFIG`。它使用实际 Windows WFP 和公网 TLS，不替换连接函数；临时阻断仅匹配本测试可执行文件、对应源地址、接口及 TCP 443，不改变其他进程或路由。普通代理支持域名候选回退，TUN TCP 池仍按其字面目标语义处理。

系统路由报告保留全部前后快照及差异。Windows Teredo 自动换地址产生的 `Protocol=Local` `/128` 主机路由单独记录，不要求恢复旧地址；物理网卡、其他 VPN 和 Teredo 策略路由逐条核对。首次把自动生成主机路由当作固定路由的失败记录保留在本地，修正检查范围后重新执行通过。

MTU 检查读取 Windows 原生 `NlMtu` 属性。首次使用错误的 `NlMtuBytes` 属性导致检查失败的记录保留在 `.go-cache-local/ipv6-system-mtu-first-attempt.json`，修正后重新执行通过。此检查覆盖最小本地 TUN MTU 下的真实 TCP 传输，不证明下游 ICMPv6 Packet Too Big 或公网大 UDP 行为。

侧车崩溃会使池进入停止/失败状态，Core 崩溃场景需先显式停止并重启池，再激活第三次 TUN；首次遗漏该步骤的失败报告保留。该测试只强制结束由脚本创建的 Core，核对侧车父 PID、可执行文件及创建时间，拒绝清理已复用的 PID。

IPv4 回归只提供 IPv4 源地址与索引，DNS 输入保留匹配的 IPv4 服务器，避免为不使用的 link-local IPv6 DNS 缺少作用域索引。当前网络清华 NTP 的 IPv4 地址在独立原生绑定检查中也超时，阿里/腾讯 NTP IPv4 则有有效回复；最终 IPv4 系统回归使用已验证可达的 `ntp.aliyun.com`，此前失败报告保留。[阿里 NTP 说明](https://developer.aliyun.com/mirror/NTP)提供该公网端点；[清华镜像站域名说明](https://mirrors.tuna.tsinghua.edu.cn/legacy_index)明确 `mirrors4` 只解析 IPv4。本次验证的 A-only 公网端点可供将来 NAT64 网络验收选用，但当前 IPv4 通过不能代替 NAT64 翻译验证。

### 复现严格系统 TUN 检查

从管理员 PowerShell 执行，需 Go、Python 3.10+ 和仓库 bundled sing-box。脚本编译独立 Core/客户端，使用桌面生产配置生成器，短暂运行严格 TUN，并终止本次测试拥有的侧车以验证恢复。已有 HypoMux-Tun 时拒绝执行；不会重配竞争 VPN。

```powershell
.\tools\ipv6-system-acceptance.ps1 -Adapter "网卡名" -Python "python.exe"

# 同时要求系统路由 UDP 请求/回复。
.\tools\ipv6-system-acceptance.ps1 -Adapter "网卡名" -RequireUDP -Python "python.exe"

# 同时验证测试 Core 崩溃后 Windows Job 自动结束侧车并恢复路由。
.\tools\ipv6-system-acceptance.ps1 -Adapter "网卡名" -RequireUDP -CoreCrash -Python "python.exe"

# IPv4 系统回归；地址族必须显式选择，IPv6 MTU 选项仅支持 IPv6。
.\tools\ipv6-system-acceptance.ps1 -Adapter "网卡名" -AddressFamily IPv4 `
  -RequireUDP -NTPDomain "ntp.aliyun.com" -CoreCrash -Python "python.exe" `
  -OutputDirectory ".go-cache-local/ipv4-system-baseline"

# 已独立确认 A-only 的官方国内 HTTPS 域名。
.\tools\ipv6-system-acceptance.ps1 -Adapter "网卡名" -AddressFamily IPv4 `
  -RequireUDP -NTPDomain "ntp.aliyun.com" -HTTPSDomain "mirrors4.tuna.tsinghua.edu.cn" `
  -Python "python.exe" -OutputDirectory ".go-cache-local/ipv4-system-a-only-acceptance"

# 临时测试 TUN 的 IPv6 最小 MTU 与 4 MiB HTTPS 传输；不修改物理网卡 MTU。
.\tools\ipv6-system-acceptance.ps1 -Adapter "网卡名" -RequireUDP -CoreCrash -TUNMTU 1280 `
  -HTTPSDomain "mirrors.tuna.tsinghua.edu.cn" `
  -PayloadPath "/debian/dists/stable/main/binary-amd64/Packages.gz" `
  -PayloadBytes 4194304 -Python "python.exe" `
  -OutputDirectory ".go-cache-local/ipv6-system-mtu-acceptance"
```

证据输出到指定目录，默认 `.go-cache-local/ipv6-system-acceptance`。退出 0 仅代表本次指定系统检查通过，报告仍保留 `full_acceptance=pending_network_matrix`。未经过真实 DNS64/NAT64、物理 IPv6-only、睡眠/重编号、下游 ICMPv6 PMTU 和公网大 UDP 验收前，不宣称完整适配验收通过。

## 2026-10-04 运行时绑定恢复补充

继续核对发现，原有实现会刷新界面和缓存绑定标识，但地址变化后并未自动重建正在运行的 Core 配置。现已在桌面运行状态轮询中加入最多每 5 秒一次的绑定检查：只跟踪实际运行会话的聚合网卡、显式 NIC 通道和 TUN DNS 出口，比较 IPv4/IPv6 源地址、对应接口索引和网络 DNS。权重、描述、跃点以及其他网卡变化不触发重启。

确认绑定变化后，在同一个生命周期事务中停止并重新启动本应用会话，重新枚举 OS 地址，退役旧客户端、DNS/DoH 池、NAT64 缓存、WFP 和侧车 DNS 绑定。用户的 Start/Stop 会取消待执行恢复；清理失败时不会继续启动。临时断网时保留其他可用线路并显示等待提示，可用所选线路绑定变化时允许恢复，不等待已断开的另一张网卡。已有本应用热点会话在重建完成后按原配置恢复，凭据不写入日志；热点恢复顺序通过控制流程测试，真实热点共享重建尚未实机测试。

| 验证 | 结果与范围 |
| --- | --- |
| 自动恢复与资源清理 | `TestRuntimeBindingRealCoreIPv6Loopback` 默认编译实际 Core，经桌面 Snapshot 触发从过期 `::2` 到 `::1` 的受控元数据变更；Core 遥测确认新绑定，旧已接入客户端关闭，IPv6 SOCKS TLS 证书及 HTTP 正文通过。使用独立测试证书与 IPv6 回环，不能替代物理地址变化或公网验收 |
| 生命周期与多网卡边界 | 两族地址/接口/DNS 变化、单族能力丢失、其他元数据不触发、断开网卡等待、另一可用网卡恢复、扫描取消、停止后不重启、失败清理不启动及原热点恢复顺序通过 |
| desktop 全量竞态 | 420 个顶层测试通过，0 失败，13 个显式环境检查跳过；最终多网卡边界和 TLS 正文断言另行通过目标竞态检查 |
| 静态检查与构建 | 最终源码 `go vet ./...` 与 desktop 构建通过 |
| 自动恢复公网补测 | 显式运行 `TestRuntimeBindingRealCoreRecoversStaleIPv6Source` 时，WLAN 已断开且没有首选 IPv6 地址，在前提检查处失败；未启动测试 Core，也未完成该项公网检查 |

本地证据为 `.go-cache-local/ipv6-desktop-runtime-binding-race.jsonl`、`ipv6-desktop-runtime-binding-race-result.json`、`ipv6-runtime-binding-final-race.jsonl` 和 `ipv6-runtime-binding-final-race-result.json`。默认回环测试由常规 desktop Go 测试执行，不依赖公网环境。公网恢复测试需活动物理 IPv6 网卡与独立构建 Core，接入恢复后可运行：

```powershell
$env:HYPOMUX_RUN_RUNTIME_BINDING_TEST = "1"
$env:HYPOMUX_NETWORK_TEST_ADAPTER = "网卡名"
$env:HYPOMUX_ENGINE_PATH = (Resolve-Path .go-cache-local/ipv6-hypomux-engine.exe).Path
go -C desktop test ./internal/services -run '^TestRuntimeBindingRealCoreRecoversStaleIPv6Source$' -count=1 -v -timeout 90s
Remove-Item Env:HYPOMUX_RUN_RUNTIME_BINDING_TEST
```

该入口模拟旧绑定元数据后使用实际 OS 的当前绑定，验证自动恢复链路；真实睡眠唤醒、物理 IPv6-only、接口索引变化、DNS64/NAT64 和下游 PMTU/大 UDP 的实机要求继续保留。
