# 发布前复查（2026-09-27）

> 后续修复与最终复评见 [发布前修复与复评](release-readiness-fixes-2026-09-27.md)。下文保留修复前的审查证据。

目标提交：`850e53cd4786900f9ea327b89c3b5ef6841aea5e`。

**结论：暂不建议直接发布正式版。现有自动检查、生产构建和当前提交的打包 CI 通过，但快捷添加分流规则仍存在一处保存缺陷，会回退匹配顺序，并可能静默覆盖并发新增的规则。应修复后进入最终安装包与真实网络验收。**

相对 `v2.6.0`，当前主仓库累计变更 295 个文件、增加 24,398 行、删除 1,029 行。本轮执行桌面端、Core 和官网的自动检查；人工重点复查规则集订阅、分流保存与优先级、TUN 退出及缓存、并发配置、AI/MCP 边界、皮肤导入与页面状态。不是对全部历史代码逐行审计。

本次只新增审查报告，未修改产品代码、版本号或锁文件，未提交、签名、部署或发布。

## 已确认的发布阻塞问题

### [P1] 活动连接的快捷添加绕过了已有的并发保存保护

位置：`desktop/frontend/src/pages/ConnectionsPage.tsx:438`；相关桥接默认值：`desktop/frontend/src/platform/services.ts:300`；后端保护接口：`desktop/internal/services/routing.go:380`。

`saveQuickRule` 在保存前读取 `latest`，但在后续 `previewBatch` 完成后只调用 `appServices.routing.save(nextRules)`。它没有传递 `latest.match_order` 和 `latest.revision`。桥接因此选择无版本校验的 `SaveOrdered`，并使用默认顺序 `process → domain → ip`。

同一处调用存在两种已复现的结果：

1. **并发规则丢失（P1）**：快捷添加取得快照后、提交前，AI/MCP 或其他页面成功新增规则；快捷添加仍整份保存旧列表，刚新增的规则被静默删除。保存前重新读取一次并不能消除两次 RPC 之间的窗口。临时后端测试按此调用顺序插入 `AI-added.exe`，最终保存结果只剩 `Existing.exe` 和新域名规则。
2. **匹配顺序回退（P2）**：用户已经选择 `IP → 域名 → 进程`，随后从活动连接右键添加一条域名规则；保存后全局顺序变成 `进程 → 域名 → IP`，所有手动规则的数值优先级随之重写。这无需并发即可触发，可能改变其他流量的出口选择。

证据：临时 React 回归在返回自定义顺序和 revision 的情况下，捕获的保存调用只有规则列表，附加参数实际为 `[]`；临时 Go 回归调用与该前端相同的公开 API 顺序，分别观察到匹配顺序回退和并发规则丢失。测试使用模拟服务或临时配置目录，不改动真实用户配置。

修复方向：快捷添加须保留读取快照的匹配顺序，并通过 `SaveOrderedChecked` 校验 revision；遇到冲突保留用户选择并重新预检，或提供后端原子“追加/替换单条规则”接口。不要仅增加一次前端刷新或直接忽略冲突。正式回归至少覆盖非默认顺序、保存期间外部新增规则、同值规则被其他操作改动三个场景。

## 自动检查结果

| 检查 | 本轮结果 |
| --- | --- |
| 桌面前端 `pnpm test` | 55 个文件、321 项通过 |
| 桌面前端 TypeScript / Vite 生产构建 | 通过 |
| Core `go test -json -count=1 -timeout 180s ./...` | 12 个包通过；374 个测试/子测试 pass，2 个 skip |
| Desktop 同上 | 7 个有测试的包通过；505 个测试/子测试 pass，10 个 skip |
| 两模块完整 `go test -race` | 均通过，无 data race 报告；跳过项同常规测试 |
| 两模块 `go vet ./...` | 通过 |
| 两模块 `go mod verify` | 通过 |
| Core `govulncheck ./...` | 未发现漏洞 |
| Desktop `govulncheck ./...` | 无命中的漏洞调用路径；依赖模块层面另有 4 项未被导入包命中的公告 |
| 前端 `pnpm audit --json` | 4 条依赖告警，见下文；不是全绿 |
| 官网 `npm test` | 33 项通过 |
| 官网类型检查、lint、生产构建及 postbuild | 通过；包含 32 页 SEO、20 个双语文档页、1,076 个本地链接/图片检查 |
| 官网子模块 | 映射正常，提交 `21c179f8d7d3ab2fa258c55cfcfc5c4bccc29bfc` |
| 新增临时组合场景回归 | 1 个 React 用例、2 个 Go 子用例失败，准确复现上述保存缺陷 |

本机工具链：Go 1.26.6、Node 24.18.1、pnpm 11.18.0；race 使用仓库已有 LLVM-MinGW。首次沙箱执行遇到 Vite 子进程 EPERM、Go 缓存和 Windows 管道拒绝访问，正常权限复跑通过，未将这些环境错误列为产品缺陷。

本地 `gofmt -l` 单独列出 `ai_test.go`；核实是该文件工作副本为 CRLF，而 Git blob 为 LF。按仓库 LF 内容复查通过，当前 CI 格式检查也通过，不属于提交中的格式缺陷。

临时复现文件已移出源码测试目录并保留在忽略目录 `.tmp/`，不会让默认测试套件因审查用例而失败。原始检查日志同样保存在 `.tmp/release-audit-*`。

## 当前提交的 CI 与发布产物

已只读核对完整 SHA 对应的两个成功运行：

- [Build Desktop #36303364047](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/36303364047)：绑定生成、前端测试和构建、Go 验证、格式检查、Wails 构建及 NSIS 打包均成功。
- [Validate Go Engine #36303364018](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/36303364018)：成功。

Desktop CI 使用项目固定的 Node 22 / pnpm 10.34.5，因此本机 Node/pnpm 与 CI 的差异没有替代 CI 检查。本轮未重复本地 NSIS 打包。

该次 Desktop CI 由 `push` 触发，Desktop/Core/installer 签名、签名后重打包及 `Verify production installer trust policy` 全部跳过。成功的未签名打包不能证明最终 SignPath 安装包已经通过发布验收。

## 前端与可用性

使用当前源码的本机 Vite 预览检查首页、AI 助手、手动规则、规则集内容和设置页；在常用的 1120×800 视口核对订阅页与设置页布局，并验证键盘导航、规则集内容搜索和加载状态。搜索示例从 3 条正确过滤为 1 条。未观察到本轮检查页面的脚本错误或业务内容横向溢出。

浏览器未连接 Wails 后端：规则和网卡为示例数据，设置页显示读取失败及重试入口符合服务不可用状态。这些检查只证明浏览器预览下的显示与交互，不证明真实设置保存、WebView2 材质、触屏、原生弹窗或后台 Live2D 生命周期已经验收。

前端人工检查参考 [Web Interface Guidelines](https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md)，重点检查可访问名称、键盘操作、长列表、异步状态及内容溢出；未将纯排版偏好列为正式版阻塞项。

## 依赖告警分级

`pnpm audit` 报告 1 条 critical、3 条 moderate，分别对应 3 个公告，其中 Vitest 公告在两个包上重复计数。

| 依赖 | 审计结果与当前代码路径 | 处理建议 |
| --- | --- | --- |
| `pixi-live2d-display → gh-pages@4.0.0` | critical 原型污染公告；这是上游附带的文档发布工具依赖。本项目导入的是 Live2D 浏览器分发文件，未发现产品源码调用 `gh-pages`，不能据此宣称客户端存在已验证的 critical 漏洞。 | 清理或通过经过验证的 override 升级此多余传递依赖。 |
| `fflate@0.8.2` | moderate，公告涉及 `unzipSync` 处理特制 ZIP64 无限循环。当前皮肤导入使用受限头部检查和流式 `Unzip`，生产源码未调用 `unzipSync`；测试中用于读取固定皮肤样例。 | 升级至修复版本并复跑皮肤导入/导出回归。 |
| `vitest@4.1.10` / `@vitest/mocker@4.1.10` | 同一个 moderate 公告，涉及开发服务器 mock 路径读取；属于开发依赖，现有测试使用 `vitest run`，未发现项目启用公告中的公开 mock 插件路径。 | 更新到包含修复的版本，至少 4.1.11，并保留完整测试结果。 |

公告来源：[gh-pages](https://github.com/advisories/GHSA-8mmm-9v2q-x3f9)、[fflate](https://github.com/advisories/GHSA-px8p-9vwx-vf98)、[Vitest 官方公告](https://github.com/vitest-dev/vitest/security/advisories/GHSA-82fw-gwwq-j7x9)。上述可达性结论来自本仓库源码与依赖入口核查，不是漏洞利用测试；建议发布前整理依赖，但本轮没有把告警等级直接等同于最终客户端风险。

## 正式发布仍需补齐的验收证据

本轮没有运行真实 TUN/WFP 接管、热点共享、MTU 修改或安装/覆盖升级/卸载；需要对修复后的最终产物验证：

- 正式签名及安装包信任校验；旧版覆盖升级、自定义目录、Core 服务启停和卸载恢复。
- sing-box 1.14.2 下真实双网卡、混合 IPv4/IPv6、DNS/FakeIP、规则集热更新和重启，及退出后的路由/DNS/代理恢复。
- 热点启停与异常退出恢复；真实下载负载下的新调度策略；WebView2、Live2D 和 100%/125%/150% DPI 的显示与交互。

自动测试中的显式真实服务/网络集成及官方签名包验证仍按默认条件跳过，跳过项没有被计作通过。当前具备继续候选版验收的基础，但应先修复快捷添加规则的保存缺陷，再对最终版本作正式发布判断。
