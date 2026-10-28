# HypoMux 2.8.0 发布前审查（2026-10-24）

**21:37 实机升级补充：用户从 v2.7.0 应用内升级失败，确认 P1 发布阻断。** 安装事务日志显示受保护 Core 目录的 `sing-box.exe` 仍被占用，安装器在部分文件替换后回滚；事务最终为 `rolled-back`，10 个文件/快捷方式哈希均已恢复到原值。旧的隔离测试没有覆盖残留 sing-box。该失败优先于下方发布前抽查结论；本地修复、回归测试与安装包编译已完成，见 [修复验收记录](upgrade-singbox-lock-2026-10-24.md)。最终签名包真实覆盖升级复测之前不能视为已解决的发布版本。

结论：当前代码已接近正式发布水准，本次抽查未发现已确认的 P0/P1 核心阻断缺陷；建议先更新存在已知告警的开发依赖，再完成最终签名安装包验收。不能把本轮结果表述为“全量测试通过”或“正式包已验收”。用户已反馈实机测试正常，但未说明是否覆盖最终签名包、覆盖升级、异常退出恢复和纯 IPv6 网络。

## 审查输入与范围

- 主仓库 HEAD：`36f87431be0da7e22cd9ccecc9776fb7d235a1c5`，版本 `2.8.0`。
- 相对 v2.7.0：309 个文件变化，24,242 行新增、3,539 行删除。
- 官网主仓库记录的子模块为 `9a1a200a81f3a663a454cee0c2db967ce1194a78`，本机实际检出 `02f51fcd72b6791e6a794393329c88873c1b7ecf`；官网检查覆盖后者。
- 审查前已有官网子模块变更及未跟踪的 `release-review-2026-10-13.md`，均保留。
- 抽查安装事务、NSIS 安装/卸载、更新辅助入口、系统代理恢复、网卡绑定恢复、双栈拨号与 UDP 转发；运行已有自动检查。不代表逐行审计全部代码或完整安全认证。
- 未修改产品实现，未执行真实安装、卸载、发布、服务配置变更或网络配置变更。

## 发现与发布建议

### P2：开发与测试依赖存在新增已知漏洞告警

桌面前端完整审计返回 14 条公告：2 high、9 moderate、3 low。单独生产依赖审计为 0。官网完整审计返回 1 个 high 级别依赖条目（Vite，聚合多个公告）。这些结果来自本轮在线漏洞库查询，不能沿用 10 月 13 日“0 告警”的历史结论。

| 范围 | 当前锁定版本 | 建议最低修复版本 | 说明 |
| --- | --- | --- | --- |
| 桌面 Vite | 8.1.5 | 8.1.6 | Windows launch-editor 命令注入相关及开发服务器告警 |
| 官网 Vite | 8.2.2，package.json override 固定 | 8.2.4 | 同类开发服务器告警 |
| 桌面 Vitest / @vitest/mocker | 4.1.11 | 4.1.12 | 手动 mock 注册、Windows 路径校验告警 |
| jsdom → undici | 8.10.2 | 8.12.2 | 错误处理造成 Node 进程退出、EventSource 等告警 |
| stylelint → table → ajv → fast-uri | 3.1.8 | 3.1.9 | URI 解析及规范化告警 |

尤其注意 `desktop/frontend/pnpm-workspace.yaml:2` 仍将旧 undici 强制替换为 8.10.2，单纯重新安装依赖不一定能修复，应同步调整 override 与锁文件。

影响判断：目前确认的是开发/测试工具链风险，没有证据证明这些 Node 工具随 Windows production 客户端运行。桌面生产依赖审计为 0，故不将 high 扫描标签直接解释成终端用户可利用的客户端高危漏洞。建议发布前完成兼容版本更新，并重跑测试、生产构建和审计。

公告依据：

- [launch-editor / Vite 官方公告](https://github.com/vitejs/launch-editor/security/advisories/GHSA-587h-7wmj-h5pm)
- [undici 官方公告](https://github.com/nodejs/undici/security/advisories/GHSA-xjj6-f33q-929c)
- [Vitest Windows 路径校验官方公告](https://github.com/vitest-dev/vitest/security/advisories/GHSA-xp26-3454-mjgp)

### 验收缺口：本机 IPv6 回环路径受阻，尚不能归因

普通权限复测后，Engine 有 4 个顶层 IPv6 测试失败，Desktop 有 2 个顶层 IPv6 测试失败。TCP 报错为 Windows 拒绝套接字访问，UDP 出现回环超时，ICMP 返回不可用。

`TestRuntimeBindingRealCoreIPv6Loopback` 在启动/调用 Core 前直接建立本机 IPv6 连接即失败。另用独立 .NET TcpListener/TcpClient 对 `::1` 验证，也出现同样的套接字访问权限错误。因此该问题发生于本机网络路径，不能直接判定为待发布代码的回归。检查时本机存在运行中的 hypomux-engine 和 sing-box；本轮没有停止它们或变更防火墙以进一步归因。

当前提交的远端 Windows CI 中完整 Go 验证步骤成功。该证据与本机结果需同时保留：本地失败不是通过，也不应仅凭本地失败认定项目有核心缺陷。建议在回环网络正常的干净 Windows 环境重跑失败项，并对最终安装包验证双栈与纯 IPv6；若用户既有实机测试已覆盖，应记录对应包版本、网络条件与结果。

失败的顶层测试：

- Engine：`TestIPv6ICMPRealLoopback`、`TestIPv6DNSRealUDPAndTCPFallback`、`TestIPv6DoHRealTLSAndCacheIsolation`、`TestDNSPodDualStackIPv6BootstrapAndIPv4WinnerCancellation`。
- Desktop：`TestDesktopIPv6ICMPRealLoopback`、`TestRuntimeBindingRealCoreIPv6Loopback`。

### 发布收尾：签名包与公开版本信息

当前提交的 [Build Desktop #40082523730](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/40082523730) 成功，Go 验证、前端检查、Wails 打包及原生辅助入口检查均成功。但该运行由 push 触发，SignPath 签名、签名后重新打包、production installer trust policy 及发布步骤全部跳过；它只证明常规构建成功。

正式发布前应使用最终签名安装包确认：新安装、从 v2.7.0 覆盖升级、应用内升级，以及停止/退出/卸载后的代理与 TUN 网络恢复。若此前实机测试已针对同一最终包完成这些场景，可直接复用该证据。隔离 NSIS 测试不等价于真实机器上的完整升级与卸载。

README 中英文版本徽标仍为 2.7.0。官网中英文 changelog 的 `latestRelease` 也为 v2.7.0，下载组件直接从该字段生成固定版本下载地址。因此 v2.8.0 发布后必须同步 changelog、下载入口及 README，避免继续引导用户安装旧版；发布前保留当前正式版信息本身不是错误。官网子模块的新界面提交是否纳入此次同步也需要统一。

## 本轮验证结果

| 检查 | 结果 |
| --- | --- |
| 桌面前端测试 | 61 文件、439 项通过 |
| 桌面 TypeScript / Vite production 构建 | 通过 |
| 桌面 CSS stylelint | 通过 |
| 官网测试 | 33 项通过 |
| 官网 lint / 类型检查 / production 构建 / postbuild | 全部通过；40 页 SEO、28 个双语文档页、1,622 个本地链接/图片等检查成功 |
| Engine 默认 Go 测试，本机普通权限 | 493 pass / 7 fail / 9 skip；失败含父子计数，对应上述 4 个顶层 IPv6 用例 |
| Desktop 默认 Go 测试，本机普通权限 | 744 pass / 2 fail / 18 skip；失败为上述 2 个顶层 IPv6 用例 |
| Engine / Desktop go vet | 均通过 |
| Engine / Desktop go mod verify | 均通过 |
| Engine 构建 / Desktop production 标签构建 | 均通过，Engine 确认使用 go1.26.10 |
| 版本一致性 | 通过；2.8.0 / Windows 2.8.0.65535 / stable update-channel |
| 隔离 NSIS 控制流补测 | 4 个顶层测试，含子项 27 条 pass，0 fail / skip |
| 新编译 Core IPC 握手、stopped 状态 | 通过 |
| 新编译 Desktop 安装/更新原生辅助入口 | 通过 |
| Engine proxy race | 通过，无竞态报告 |
| Engine DNS race | 未通过；失败为同类本机 IPv6 回环路径问题，没有竞态报告；不能记为整组 race 通过 |
| Engine govulncheck | 无已知漏洞 |
| Desktop govulncheck | 0 个可达漏洞、0 个导入包漏洞；另有 1 个仅 require 模块层告警 |
| 桌面前端完整 / production 依赖审计 | 14 条公告 / 0 条公告 |
| 官网依赖审计 | 1 个 high 级别依赖条目 |
| 两仓库 git diff --check | 均通过 |

Go pass/fail/skip 统计包含父测试与子测试终态，并非独立用例数量。NSIS、IPC、原生辅助入口的补跑覆盖了默认 Go 测试中的部分 skip；其他需显式启用、安装状态或真实网络条件的测试不计作通过。

初次沙箱运行另有文件重命名、ACL 和 IPv4 套接字限制；本表优先使用普通权限复测结果。既有跳过项包括真实签名安装器验签、已安装服务认证路径、部分真实网络/TUN/WFP 与公开 IPv6/NAT64 验收。

本轮原始日志、审计 JSON、构建二进制和 GitHub Actions 摘要位于 `.tmp/release-audit-2026-10-24/`（本地忽略目录）。
