# 批次 D 验证报告：TUN 只读预检与权限安全门

日期：2026-07-31

## 本批范围

- 完整复核 v2.2.0 的第三方 TUN、同子网/同网关、WFP 探测和管理员权限流程。
- 增加 `TunService.Preflight`，所有检查发生在出站池、sing-box、Wintun、路由和 WFP 接管之前。
- `EngineService.Start("tun")` 增加后端二次安全门，前端检查不能被绕过。
- 首页使用 Fluent UI `Dialog`、`Badge`、`Button` 和 Fluent Icons 显示阻断项、提醒项和探测证据。
- 本批不请求 UAC、不启动真实 TUN、不修复 BFE、不修改路由或系统代理。

## 迁移语义

| v2.2.0 真值 | 本批结果 |
| --- | --- |
| 第三方虚拟隧道接管默认路由时阻止启动 | 已实现只读默认路由扫描和阻断结果 |
| 同 CIDR 且共用默认网关只警告、允许继续 | 已实现 CIDR/网关双条件提醒 |
| WFP 启动前无副作用探测 | 使用 `FwpmEngineOpen0` 后立即关闭 |
| WFP 失败时仅本次兼容降级 | 输出 `effective_strict_route=false`，不覆盖用户偏好 |
| TUN 需要管理员权限 | 缺少独立权限 Broker 时在任何网络接管前阻断 |
| UI 不应长期管理员运行 | 检测到高权限 WebView2 Host 同样阻断，不作为兼容路径 |

> 后续兼容策略已调整：桌面入口仍优先自动降权，但降权失败时高权限 UI 只产生警告，不再作为 TUN 阻断条件；独立核心通道不可用仍会阻断网络接管。

## 自动化与真实 Windows 证据

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| TUN 预检单元测试 | 通过 |
| 第三方 TUN / 同网关 / WFP 降级语义 | 通过 |
| 资源缺失阻断 | 通过 |
| 真实 Windows `FwpmEngineOpen0` 探测 | 通过 |
| 预检前后默认路由别名对比 | 完全一致 |
| `pnpm --dir frontend build` | 通过，2194 modules |
| Wails v3 bindings | 7 services / 38 methods / 16 models |
| `wails3 build` | 通过，生成 `bin/hypomux.exe` |

真实预检测试未调用 `engine.start`、`tun.activate`、`Remove-NetRoute`、`sc start` 或任何 WFP 规则添加接口。

## 视觉 QA

- 1120×800 浅色 Dialog：通过。
- 1120×800 深色 Dialog：通过。
- 启动按钮触发预检、阻断后回到 stopped：通过。
- 当前前端 bundle 控制台错误：0。
- 第一轮截图发现网卡地址与同网关提醒夹具不一致；已统一为 `192.168.10.100/24`、`192.168.10.101/24` 和网关 `192.168.10.1` 后复拍。

截图：

- `docs/validation/wails/screenshots/batch-d/tun-preflight-1120x800-final.png`
- `docs/validation/wails/screenshots/batch-d/tun-preflight-dark-1120x800.png`

视觉 QA 使用验证进程内部的临时静态文件服务，没有启动 Vite 或其他外部长驻子进程；验证结束后服务、浏览器标签和临时端口均已关闭。

## 尚未完成

- 独立高权限 Core/Broker 的启动、鉴权命名管道和一次性令牌。
- UAC 同意、取消、超时和错误映射。
- WFP/BFE 修复。
- TUN 外部联网验证、周期看门狗、DoH/WFP 一次受控重启。
- TUN 停止、崩溃、系统退出后的完整残留资源实机矩阵。

在下一小批完成 Broker 前，TUN 保持安全阻断，不能通过提升 WebView2 Host 权限绕过。
