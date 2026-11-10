# 官方 CLI（hypomux-cli）架构与安全设计

对应 Issue：[#60 官方 CLI：让 HypoMux 成为 Agent 原生工具](https://github.com/Hypostasis-Cat/HypoMux/issues/60)。
本文记录推荐路径（路径 1）的落地方式：**官方 Go CLI 走 Core 服务管道，纳入既有的
客户端二进制指纹/摘要认证体系**。

## 定位

`hypomux-cli.exe` 是协议 v1（`protocol/v1`）的一个普通客户端，与桌面端平级。
它不引入新的 wire 协议、不新增协议方法；协议 v1 本身保持原样（additive 契约
不受影响）。CLI 的价值在于把既有能力以 Agent 可编排的形式暴露出来：无人值守的
批量下载前后启停聚合、CI 中探测链路质量、脚本化读取遥测等。

```
hypomux-cli ──协议 v1 JSONL──> \\.\pipe\HypoMux-Core-Service ──> hypomux-engine（LocalSystem 服务）
                     ▲
                     └ 服务端校验客户端 PID → 可执行文件路径 → SHA-256 摘要
```

## 安全模型：指纹固定扩展

Core 服务原本只把**桌面程序**（`hypomux.exe`）的安装路径与 SHA-256 摘要固定在
`HKLM\SOFTWARE\HypoMux\CoreServicePolicy`，任何其他二进制连接服务管道都会被拒
绝（第三方客户端被有意拒绝，这是安全设计而非缺陷）。

CLI 采用同一机制，策略增加两个**可选**注册表值：

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

## 传输与会话

CLI 是一次性进程：连接 → 发请求 → 收响应 → 打印 → 退出。

- **服务发现**：SCM 查询 `HypoMuxCore` 服务的运行 PID，连接
  `\\.\pipe\HypoMux-Core-Service`（对 `PIPE_BUSY`/`FILE_NOT_FOUND` 短重试），
  并用 `GetNamedPipeServerProcessId` 复核服务端 PID，防止同名接管管道。
- **协议**：请求 `{protocol:1, id, method, params}`，响应与事件共用
  JSONL 帧（1 MiB 上限）。事件（如 `engine.state_changed`）转发到 stderr，
  stdout 永远只有命令结果——这是给脚本与 Agent 的稳定约定。
- **超时**：默认 30 秒（`--timeout` 可调）；`diagnose` 按探测次数自动放宽。
  读写 deadline 覆盖写阻塞场景。
- **一次性会话语义**：服务端一个会话服务一个客户端连接，命令结束即断开，
  与桌面端长会话共用同一个服务循环，无需服务端改动。

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
hypomux-cli shutdown | version | help
```

设计取舍：

- **`start` 只支持 proxy 模式**。TUN 编排（sing-box 配置生成、摘要固定、特权
  生命周期）是桌面端的特权工作流，CLI 不复刻；请求 `tun_tcp_pool` 会得到明确
  的指引错误。相应地 `tun.activate` 不暴露，`tun` 只有 `status/deactivate`。
- **网卡选择基于本机枚举**（与桌面端同一套过滤基线：活动、非回环、非
  HypoMux-Tun、有全局 IPv4 或 GUA IPv6），`--adapter` 按（大小写不敏感的）
  名称匹配后自动填 `source_ip/if_index/source_ipv6`，避免手写出站参数。
- **调度策略先协商**：`--strategy` 会先核对 `engine.hello.scheduling_strategies`，
  不把自适应模式发给不支持的核心（协议 README 的客户端义务）。
- **`raw` 是兜底出口**：任何 `engine.hello.capabilities` 里的方法都可以带原始
  JSON 参数调用；协议 v1 未来新增的 additive 方法在 CLI 侧即刻可用。
- **`rules` 即域名隔离**：协议 v1 的进程/域名/IP 分流规则保存在桌面端设置里，
  引擎侧的公共边界是 `domain_quarantine.update` 与遥测中的隔离状态。

`--json` 输出引擎返回的原始协议结果（缩进 JSON）；人类可读输出仅作默认视图，
不作为契约——脚本一律走 `--json`。

## 打包与安装链

- `desktop/build/windows/Taskfile.yml` 的 `build:core-runtime` 同时构建
  `hypomux-cli.exe`（与引擎同版本号、同 commit 注入）。
- 安装器把 CLI 纳入 payload：`compute-payload-hashes.ps1` 与
  `internal/setup/payload_windows.go` 的摘要清单、`command_windows.go` 的
  复制清单、`platform_windows.go` 的 `ValidateTarget` 均增加
  `hypomux-cli.exe`，保持“安装事务回滚 + 提权前摘要校验”的既有保证。
- `project.nsi` 在安装时调用
  `hypomux-engine.exe install-service --desktop "$INSTDIR\hypomux.exe"
  --cli "$INSTDIR\bin\hypomux-cli.exe"`，卸载时删除两处 CLI 副本。
- CLI 本体不做 Authenticode 签名（纯客户端、不提权），其完整性由上述
  SHA-256 pin 在服务管道入口强制执行。

## 测试

- 策略层：CLI pin 的构建/校验/半写拒绝（`service_policy_windows_test.go`）。
- CLI 单元：JSONL 会话关联、事件转发、远端错误、超时；`start` 参数构造、
  权重语法、策略协商门禁；命令分发与 `--json`。
- 端到端：CLI 直连 `engine/internal/server` 的真实引擎服务（net.Pipe），覆盖
  hello/health/status/stop/rules/raw/dns 错误路径，保证请求字节与协议
  fixtures 同构。
- 传输层（Windows）：真实命名管道上验证服务端 PID 复核与 SCM 查询语义。

## 与第三方 CLI（路径 2）的关系

官方 CLI 不改变第三方现状：服务管道依旧按指纹拒绝未知客户端，protocol v1
fixtures 仍是社区可依赖的契约。社区路线（自拉 `engine serve` 独立实例）继续
可行，两类客户端互不干扰——一个经由受管服务共享引擎，一个自持引擎实例。
