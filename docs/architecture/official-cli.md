# 官方 CLI（hypomux-cli）架构与安全设计

对应 Issue：[#60 官方 CLI：让 HypoMux 成为 Agent 原生工具](https://github.com/Hypostasis-Cat/HypoMux/issues/60)。
本文记录推荐路径（路径 1）的落地方式：**官方 Go CLI 走 Core 服务管道，纳入既有的
客户端二进制指纹/摘要认证体系**，并在服务端按客户端角色强制权限边界。

## 定位

`hypomux-cli.exe` 是协议 v1（`protocol/v1`）的一个普通客户端，与桌面端平级。
它不引入新的 wire 协议、不新增协议方法；协议 v1 本身保持原样（additive 契约
不受影响）。CLI 的价值在于把既有能力以 Agent 可编排的形式暴露出来：无人值守的
批量下载前后启停聚合、CI 中探测链路质量、脚本化读取遥测等。

```
hypomux-cli ──协议 v1 JSONL──> \\.\pipe\HypoMux-Core-Service ──> hypomux-engine（LocalSystem 服务）
hypomux.exe ──协议 v1 JSONL──>        （同一管道，多实例并发）         │
                     ▲                                              ▼
                     └ 服务端校验客户端 PID → 路径 → SHA-256      服务级单例引擎
                       命中桌面 pin → desktop 角色（完整能力）    （proxy / TUN / runtime / 隔离规则）
                       命中 CLI pin → cli 角色（受限能力）
```

## 安全模型

### 指纹固定

Core 服务把**桌面程序**（`hypomux.exe`）的安装路径与 SHA-256 摘要固定在
`HKLM\SOFTWARE\HypoMux\CoreServicePolicy`，任何其他二进制连接服务管道都会被拒
绝（第三方客户端被有意拒绝，这是安全设计而非缺陷）。CLI 采用同一机制，策略增加
两个**可选**注册表值：

| 值 | 含义 |
| --- | --- |
| `CoreCliPath` | 官方 CLI 的规范化安装路径 |
| `CoreCliSHA256` | 该 CLI 文件的 SHA-256 摘要 |

约束与既有桌面 pin 完全一致，全部**默认拒绝、显式开启**：

- 两个值要么同时存在、要么同时缺失；半写状态在 `validateCoreServicePolicy`
  处直接失败，中断的安装不可能留下只配路径或只配摘要的授权。
- 路径必须是固定本地卷上的普通文件（拒绝 UNC、可移动磁盘、reparse point）。
- 连接时按 PID 反查客户端可执行文件，路径规范化（`GetFinalPathNameByHandle`）
  后必须与 pin 一致，再以 `fileintegrity` 复核 SHA-256——文件被篡改即拒。
- `install-service` 未传 `--cli` 时写入空 pin，服务继续拒绝一切 CLI 客户端；
  策略内容完全由最近一次成功的 install-service 调用决定。
- 旧版本引擎写出的策略没有这两个值，新引擎按“未固定 CLI”处理（向后兼容）。
- 桌面 pin 与 CLI pin 的校验彼此独立，命中任意一个即通过，全部失败才拒绝；
  会话（RDP/控制台）检查与之前完全相同。

### 客户端角色与权限边界（服务端强制）

管道 ACL 允许所有交互式用户（`IU`）连接，而 CLI 本体不对调用者做鉴权——任何
能执行 pinned `hypomux-cli.exe` 的进程都能连上管道。若 CLI 与桌面端享有同等能力，
就等于给系统装了一个现成的提权跳板（例如以 SYSTEM 运行任意 sing-box 配置、改
MTU、把代理暴露到局域网）。因此**命中的 pin 决定本次会话的角色**，由服务端强制
裁剪能力，CLI 侧的任何过滤都只是 UX：

- `validateServicePipeClient` 返回命中的角色（`desktop` / `cli`），传入本次会话；
  角色来自服务端 pin 匹配，绝不取自请求体。
- `desktop` 角色：完整协议面，行为与改造前一致。
- `cli` 角色：方法白名单 + 参数校验，白名单为
  `engine.hello/status/telemetry/start/stop/scheduling`、`health.check`、
  `dns.resolve/status`、`diagnostic.run`、`domain_quarantine.update`、
  `tun.status`、`tun.deactivate`（只往安全方向收）。白名单之外的方法
  （`tun.activate`、`mtu.set`、`host.shutdown`、`wfp.inspect`、`hotspot.inspect`、
  `steam_cdn.configure`、`dns.dohRelay`）一律返回 `security_policy_rejected`。
- `cli` 角色的 `engine.start` 在服务端强制 `mode == proxy` 且 `listen_host` 为回环
  地址（空值由 proxy 配置规范化为 `127.0.0.1`）。`raw engine.start` 无法绕过这条
  边界去启动 `tun_tcp_pool` 或绑定 `0.0.0.0`。（proxy 配置规范化本身也拒绝非回环
  监听，服务端角色校验是其上的显式防线。）
- `engine.hello` 的 `capabilities`/`modes`/`mode_features` 按角色裁剪：CLI 只看到
  白名单方法与 `proxy` 模式。于是 `raw` 出口与协议协商天然一致——客户端不会被
  广告一个它实际调不动的方法。

`tun.deactivate` 对 CLI 开放是因为它只关闭 TUN、收敛特权状态，不会新增任何
路由或特权写入；`tun.activate` 则因需要桌面端生成并固定 sing-box 配置摘要而保留
为桌面特权编排。

## 传输与会话

### 服务级单例引擎

引擎运行状态（`proxy` / `tun` / `runtime` / 隔离规则等）是**服务级单例**
（`server.NewHost`），每个认证连接只是它的一个会话（`server.Attach`）。会话结束
**不再自动 stop**：只有显式 `engine.stop`、桌面断线的特权清理（见下）或服务停止
才停引擎。这修正了“每连接一个 Server”的旧模型——旧模型下 `hypomux-cli start`
返回后进程退出、管道断开，`Server.Run` 的 `defer stopProxyForHostExit` 会立刻拆掉
代理，`status` 也永远只能看到新实例的 `stopped`。

现在：一次 `start` 启动的代理在该会话关闭后继续运行，另一次 `status` 连上来仍看
到 `running`；桌面端通过周期性 `engine.status` 与广播事件看到 CLI 引起的状态变化。

### 断线清理保证

桌面端正常退出会先显式 `engine.stop`。为保留“桌面端异常退出后不留残余路由/TUN”
的保证，**桌面角色会话断开时**若托管 TUN 或 WFP DNS 豁免仍存活（例如 UI 崩溃），
服务端拆掉整个 TUN 工作流（`DesktopSessionEnded`）。普通 proxy 池没有路由/TUN
足迹，会保留给下一个客户端或显式 `engine.stop`。CLI 角色无法激活 TUN，其断线不
触发任何特权清理。

### 多实例管道与事件广播

- 命名管道改为多实例（`PIPE_UNLIMITED_INSTANCES`），服务循环为每个连接并发派发
  会话 goroutine，并立即创建下一个监听实例。桌面长连接与 CLI 短连接因此共存，
  不再互相 `PIPE_BUSY`。仅首个实例断言 `FILE_FLAG_FIRST_PIPE_INSTANCE`，被占用
  的管道名仍会让服务 fail-closed。
- 事件（`engine.state_changed` / `tun.state_changed` / `log.record` 等）以单一
  服务级单调序号**广播给所有在线会话**；某个会话传输失败只关闭该会话，不影响
  其余广播。
- **服务发现**：SCM 查询 `HypoMuxCore` 服务的运行 PID，连接
  `\\.\pipe\HypoMux-Core-Service`（对 `PIPE_BUSY`/`FILE_NOT_FOUND` 短重试），
  并用 `GetNamedPipeServerProcessId` 复核服务端 PID，防止同名接管管道。
- **协议**：请求 `{protocol:1, id, method, params}`，响应与事件共用 JSONL 帧
  （1 MiB 上限）。CLI 把事件转发到 stderr，stdout 永远只有命令结果——这是给脚本
  与 Agent 的稳定约定。
- **超时**：默认 30 秒（`--timeout` 可调）；`diagnose` 按探测次数自动放宽。

## 命令面

```
hypomux-cli status | hello | health
hypomux-cli start --all | --adapter 名称[:权重] … [--weight 名称=权重]…
                  [--listen-host] [--socks-port] [--http-port] [--strategy …]
                  [--dns-policy …] [--dns-server]… [--doh-server]… [--dot-server]…
                  [--steam-cdn]
hypomux-cli stop
hypomux-cli telemetry [--connections]
hypomux-cli adapters
hypomux-cli diagnose [--src-ip …] [--target-ip …] [--count …] [--timeout-ms …]
hypomux-cli rules [clear | expiry on|off | remove 网卡 域名]
hypomux-cli dns resolve 域名 [--adapter …] [--type A|AAAA|HTTPS] | dns status
hypomux-cli tun status | tun deactivate
hypomux-cli raw <方法> [JSON 参数]
hypomux-cli version | help
```

设计取舍：

- **`start` 只支持 proxy 模式**。TUN 编排（sing-box 配置生成、摘要固定、特权
  生命周期）是桌面端的特权工作流，CLI 不复刻；服务端对 CLI 角色强制 proxy + 回环。
  相应地 `tun.activate` 不暴露，`tun` 只有 `status/deactivate`。
- **不提供 `shutdown`**。`host.shutdown` 在新会话模型下语义含糊（服务进程并不
  退出），且属于桌面角色；CLI 用 `stop` 显式停引擎即可，避免混淆。
- **网卡选择基于本机枚举**（与桌面端同一套过滤基线：活动、非回环、非
  HypoMux-Tun、有全局 IPv4 或 GUA IPv6），`--adapter` 按（大小写不敏感的）
  名称匹配后自动填 `source_ip/if_index/source_ipv6`，避免手写出站参数。
- **调度策略先协商**：`--strategy` 会先核对 `engine.hello.scheduling_strategies`，
  不把自适应模式发给不支持的核心（协议 README 的客户端义务）。
- **`raw` 是兜底出口**：任何 `engine.hello.capabilities` 里的方法都可以带原始
  JSON 参数调用；能力列表已按 CLI 角色裁剪，协议 v1 未来新增的 additive 方法若
  纳入 CLI 白名单即在 CLI 侧即刻可用。
- **`rules` 即域名隔离**：协议 v1 的进程/域名/IP 分流规则保存在桌面端设置里，
  引擎侧的公共边界是 `domain_quarantine.update` 与遥测中的隔离状态。

`--json` 输出引擎返回的原始协议结果（缩进 JSON）；人类可读输出仅作默认视图，
不作为契约——脚本一律走 `--json`。

## 打包与安装链

- `desktop/build/windows/Taskfile.yml` 的 `build:core-runtime` 同时构建
  `hypomux-cli.exe`（与引擎同版本号、同 commit 注入）。
- 安装器把 CLI 纳入 payload：`compute-payload-hashes.ps1` 与
  `internal/setup/payload_windows.go` 的摘要清单、`command_windows.go` 的
  复制清单、`platform_windows.go` 的 `ValidateTarget` 均包含
  `hypomux-cli.exe`，保持“安装事务回滚 + 提权前摘要校验”的既有保证。
- CLI 是管道客户端而非服务启动的 sidecar，**只复制到 `$INSTDIR\bin`**，不再
  复制到受保护的 `ProgramData\HypoMux\Core\bin`（那份副本没有用途）。pin 指向
  `$INSTDIR\bin\hypomux-cli.exe`；机器级安装目录默认仅管理员可写，与角色白名单
  共同构成防线。
- `project.nsi` 在安装时调用
  `hypomux-engine.exe install-service --desktop "$INSTDIR\hypomux.exe"
  --cli "$INSTDIR\bin\hypomux-cli.exe"`，卸载时删除 `$INSTDIR\bin` 的 CLI 副本。
- CLI 本体不做 Authenticode 签名（纯客户端、不提权），其完整性由上述
  SHA-256 pin 在服务管道入口强制执行。
- CLI 默认不在 `PATH` 中；默认完整路径为
  `C:\Program Files\HypoMux\bin\hypomux-cli.exe`（机器级安装）。脚本与 Agent
  可用该完整路径调用，或自行将安装目录加入 `PATH`。

## 测试

- 策略层：CLI pin 的构建/校验/半写拒绝（`service_policy_windows_test.go`）。
- 角色层：CLI 角色调用 `tun.activate`/`mtu.set`/`host.shutdown`/非回环或非 proxy
  `engine.start` 均被拒；桌面角色行为不变；`engine.hello` 能力按角色裁剪。
- 会话层：多会话共享同一引擎——`start` 所在会话关闭后新会话 `status` 仍为
  `running`；桌面会话在线时 CLI 仍可连接；事件广播到所有会话；桌面断线清理特权
  状态而保留普通 proxy 池。
- CLI 单元：JSONL 会话关联、事件转发、远端错误、超时；`start` 参数构造、
  权重语法、策略协商门禁；命令分发与 `--json`。
- 端到端：CLI 直连 `engine/internal/server` 的真实引擎服务（net.Pipe，共享
  单例 host），覆盖 hello/health/status/stop/rules/raw/dns 错误路径，保证请求字节
  与协议 fixtures 同构。
- 传输层（Windows）：真实命名管道上验证服务端 PID 复核、SCM 查询语义与多实例
  并发连接。

## 与第三方 CLI（路径 2）的关系

官方 CLI 不改变第三方现状：服务管道依旧按指纹拒绝未知客户端，protocol v1
fixtures 仍是社区可依赖的契约。社区路线（自拉 `engine serve` 独立实例）继续
可行，两类客户端互不干扰——一个经由受管服务共享引擎，一个自持引擎实例。
