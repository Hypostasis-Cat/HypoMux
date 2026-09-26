# 发布前审查（2026-09-26）

目标提交：`35aeafca3abcd9fb2d1e5c60697ef14c5def8ad6`。重点范围：相对 `v2.6.0` 的桌面端/Core 改动、最新页面状态保留改动、安装升级配置和发布流水线。版本号调整不在本次范围；官网仅检查子模块集成，未审计独立官网代码。

## 修复后重新评估（2026-09-26）

评估对象为上述提交加本次工作区修复，尚未提交或推送。版本号未改。

**本次确认的三个功能问题已修复，Windows 管道 race 和 TUN 测试时限问题也已处理。自动检查与本地未签名安装包构建通过，可以进入发布候选验收；正式发布仍需最终产物的安装升级、签名及真实网络验收。** 下方原审查记录保留为修复前证据，不代表当前代码仍存在这些问题。

### 已完成的修复

1. 设置页通过 `SettingsService.UpdateFields` 提交修改字段，后端在同一把锁内合并和持久化，保留 AI/MCP 或其他页面更新的规则、模式、策略、网卡和其他偏好。保留字段校验及保存失败回滚。页面监听 AI 变更，同时保留未提交的端口/DNS 草稿。旧整份 `Update` 接口保留兼容，但设置页不再调用它。
2. 网络体检只提交参与网卡 ID；`EngineService.SaveDiagnosticSelection` 在生命周期锁内校验引擎状态和网卡，只修改所选 ID。模式、四种调度策略、权重和规则均从当前后端配置保留。共享保存队列合并请求时也保留首页尚未提交的策略。
3. 工具箱在重新激活和 AI 变更后读取 Steam 优选配置，取消旧读取，避免旧响应覆盖用户刚保存的开关；后台页面暂停轮询。
4. Desktop 与 Core 的 Windows 管道使用 `github.com/Microsoft/go-winio v0.6.2` 的异步句柄封装，保留现有 ACL、PID、一次性凭据认证、超时和双向通信。原竞争对应 Go 的共享管道偏移问题，参见 [Go 官方修复](https://go.googlesource.com/go.git/+/f4ac29c3c743b30a1e1a2b7ef4eae2588ac5c6f3)。没有通过停用 race 检测或串行阻塞锁掩盖问题。
5. TUN 测试只为模拟配置检查的子进程设置 `GORACE=atexit_sleep_ms=0`，排除诊断器默认退出等待；保持 race 检测和原有 500ms 断言。

### 修复后的验证结果

| 检查 | 结果 |
| --- | --- |
| 前端 `pnpm test` | 54 文件、307 项通过；绑定生成及依赖重建后再次全量通过 |
| TypeScript、Vite 生产构建 | 通过 |
| Wails 生产绑定生成 | 通过，包含新增的两个保存接口 |
| desktop `go test -race ./... -count=1 -timeout 120s` | 通过，包含原失败的认证后双向管道通信用例 |
| engine 同上 | 通过，包含原失败的 TUN 时限用例 |
| 两模块 `go vet ./...`、`go mod verify` | 通过 |
| 新增后端回归 | 并发字段合并、无关配置保留、非法字段/写盘失败原子性、四种策略保留通过 |
| 新增前端回归 | AI 规则保留、草稿保留、缓存页面选网卡、保存队列合并、Steam 激活/事件刷新及旧响应保护通过 |
| Go 格式检查、`git diff --check` | 通过 |
| 本地 `wails3 task windows:package` | 通过：生产桌面端、Core、运行库打包和 NSIS 安装包生成 |

本地环境为 Go 1.26.6、LLVM-MinGW、Wails 3.0.0-alpha2.119、pnpm 11.18.0。本机现有依赖目录使用 pnpm 11，打包时沿用此版本和锁文件；Node 路径以 Windows 短路径传入，规避任务脚本的空格转义问题。CI 固定 pnpm 10.34.5，本次工作区尚未触发新的 CI，因此旧 HEAD 的 CI 成功不能充当本次修复的 CI 结果。

生成的本地验证包：`desktop/bin/hypomux-amd64-installer.exe`，32,546,407 字节，未签名，未安装或发布。

SHA-256：`0EBE6770A14D875645C84B8F3E557C4B5A6CDF1C339C1D1C920B642620736F5B`。

边界：全量测试中的显式安装服务和真实网络集成测试仍按默认条件跳过（`HYPOMUX_RUN_SERVICE_TEST`、`HYPOMUX_RUN_NETWORK_TEST` 未启用）。本次没有执行真实系统聚合、热点共享、安装、覆盖升级或卸载，也没有生产签名验证。上述自动检查不能替代本文末尾列出的最终发布验收。

## 原审查结论（修复前）

暂不建议正式发布。现有测试、生产前端构建和当前提交的打包 CI 已通过，但额外组合场景复现出两个会回退配置的问题，以及一个工具开关与实际状态不同步的问题。应修复后再进行最终安装包和真实网络验收。

本次仅审查，没有修改产品代码、版本号或发布外部资产。三个临时 React 回归文件均已移除；保留本报告。

## 已确认问题

### 1. [P1] 设置页的整份保存会覆盖 AI/MCP 刚保存的规则

- 位置：`desktop/frontend/src/pages/SettingsPage.tsx:392–416`；后端 `desktop/internal/services/settings.go:254–284`。
- 触发：打开设置页；在不重新进入页面的情况下，由 AI/MCP 添加分流规则；随后切换“首页隐藏虚拟网卡”等无关偏好。
- 设置页只在激活/重试时读取配置，没有监听 `hypomux:ai-changed`。保存时将旧 `settings` 快照整份提交，后端 `Update` 直接保存传入的 `RoutingRules`、网卡选择和策略等字段。
- 临时测试模拟后端新增 `cs2.exe → direct` 并发出 AI 变更事件，随后点击隐藏网卡开关；保存后的 `routing_rules` 实际变为 `[]`，保留规则断言失败。
- 影响：已经成功保存的规则可能被静默删除；同样的整份写入还可能覆盖其他操作更新的配置。已运行的规则与磁盘配置也可能出现差异。
- 修复方向：设置保存应在后端锁内只更新调用者负责的字段，保留路由和聚合选择，或使用完整的版本冲突检测。前端同步 AI 变更可以改善显示，但不能单独消除读后写之间的竞态。

### 2. [P2] 网络体检选网卡会回退模式和调度策略

- 位置：`desktop/frontend/src/pages/HealthPage.tsx:182`、`:226–245`；传输入口 `desktop/frontend/src/platform/services.ts:247–248`。
- 触发：停止聚合时访问网络体检，再到首页切换为 TUN / 低延迟优先，返回体检页选择或取消选择网卡。
- 新的 `AppShell` 保留已访问页面，但体检页只在首次挂载时读取 `homeSettingsRef`，重新激活时不刷新。它只保留 `mode` 和 `weighted`，提交时也不传 `strategy`。
- 临时测试模拟上述页面激活切换，实际保存参数为 `{mode: "proxy", strategy: undefined}`，预期为 `{mode: "tun", strategy: "latency-first"}`，断言失败。
- 未传策略会进入旧 `SaveSelection` 路径；`saveRuntimeSelection` 仅根据 `weighted` 推导轮询或手动权重，因此即使没有切换模式，体检页选择网卡也会丢失自适应/低延迟策略。
- 修复方向：体检页仅修改参与网卡，其他字段从提交时的权威配置继承；同步最新策略并使用支持策略的保存接口。补充页面切换及四种策略的回归测试。

### 3. [P2] AI 改变 Steam 优选后，工具箱继续显示旧开关

- 位置：`desktop/frontend/src/pages/ToolsPage.tsx:50–60`、`:76–90`。
- 触发：先访问工具箱，然后切到助手开启 Steam 优选，再返回工具箱。
- 工具箱的已保存开关值仅在首次挂载和错误重试时读取；未监听 AI 变更或页面重新激活。状态轮询只更新 `status`，不更新 `enabled`；显示逻辑优先以旧 `enabled` 判定“已关闭”。
- 临时测试将后端保存值和运行状态均改为开启，发送 AI 变更事件并重新激活页面，开关仍为 `false`，断言失败。
- 影响：实际运行中的功能显示为关闭，用户不能根据开关判断真实状态；从这个旧状态点击会再次发送开启操作。
- 修复方向：在页面激活及 AI 变更后重新读取已保存偏好，避免旧异步响应覆盖新操作；保持保存偏好和运行状态的区别。

## 自动检查证据

| 检查 | 结果 |
| --- | --- |
| 前端 `pnpm test` | 52 文件、300 项通过 |
| 前端 `pnpm build` | TypeScript 和 Vite 生产构建通过 |
| engine `go test ./... -count=1 -timeout 180s` | 正常权限下通过 |
| desktop 同上 | 正常权限下通过 |
| 两模块 `go vet ./...` | 通过 |
| 两模块 `go mod verify` | 通过 |
| 当前提交 GitHub Build Desktop | 通过，含绑定生成、测试、格式检查、生产构建和 NSIS 打包 |
| 官网 `git submodule status` | 正常，提交 `21c179f8d7d3ab2fa258c55cfcfc5c4bccc29bfc` |
| 三项临时组合场景回归 | 均失败，见上述实际值；只使用模拟服务 |

CI：[Build Desktop #36161350608](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/36161350608)。这是 push 触发的未签名构建，签名、生产安装包信任验证和发布步骤均跳过，不能用它证明正式签名包已经验收。

首次沙箱运行出现 Vite 子进程 EPERM、Windows 命名管道/网卡查询拒绝访问及 Core 管道用例超时；正常权限复跑通过。这些首次失败未归为产品缺陷。

## 并发检测与待排查项

使用本机 Go `1.26.6 windows/amd64` 和仓库已有 LLVM-MinGW，分别运行两模块完整 `go test -race ./... -count=1 -timeout 180s`：

- engine 的代理、DNS、运行时及服务器等包通过，未报告数据竞争。TUN 用例 `TestSupervisorReturnsWhenTunInterfaceIsReady` 的 500ms 断言失败（约 1.11 秒）。单独复跑仍失败；设置 `GORACE=atexit_sleep_ms=0` 后整个 TUN 包通过，确认 race 测试子进程的退出等待影响该时限测试，不能据此称生产 TUN 启动回归。
- desktop 的 services、platform、startup 等包通过；`TestAuthenticatedPipeSupportsConcurrentProtocolTrafficAfterAuthDeadline` 报告 data race，单独复跑也复现。
- 竞争栈在本机 Go 标准库 `internal/poll.(*FD).addOffset`，读写分别由 `privileged_windows_test.go:115`、`:138` 发起；被测连接来自生产函数 `privileged_windows.go:188` 的 `os.NewFile`。
- 该结果需要进一步核对正式构建工具链和 Windows 双向管道实现；本轮没有证明发生协议内容损坏，也不能声称 desktop race 检测通过。不要简单删除测试或将全双工收发用同一个阻塞锁包住。

## 最终发布验收尚缺的证据

本轮没有运行真实系统聚合、热点共享、WFP 修复或 MTU 修改，也没有安装、卸载、签名或发布。

修复后仍需对最终产物验收：旧版覆盖升级和自定义目录、Core 启停与卸载恢复；真实 TUN/FakeIP、多网卡和第三方代理共存；热点启动/停止/异常退出恢复；新调度策略真实负载；WebView2 中 Live2D 与后台皮肤 Worker 生命周期。历史审查文档和本次模拟测试不能替代这些最终产物实测。
