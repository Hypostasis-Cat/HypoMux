# 发布前检查（2026-10-11）

审查提交：`a90a5d45ce0c26356e0a16bb7415d7dd251ab479`。官网子模块：`19eb41b5899683aad1ac2c965bdc49a1d2107111`，与主仓库记录一致。开始检查时工作区干净。

**结论：已具备制作 RC 并进行最终验收的基础，现有证据不足以批准直接发布正式版。** 当前提交的 CI 测试和未签名打包成功，本轮未确认新的产品代码阻塞缺陷；主要缺口是最终签名安装包与真实 Windows 网络生命周期验收。另需确定新的发布版本、处理依赖告警。本机 Go 全量测试没有全部通过，不能将环境导致的失败或跳过记作成功。

本轮执行自动测试、生产编译、依赖扫描并抽查版本发布、更新下载/安装交接、DoT 连接池取消及 MCP 访问边界。不是全部代码逐行审计，也没有进行原生 UI/DPI 人工验收。相对已发布 v2.7.0，主仓库有 213 个文件变化，增加 17,486 行、删除 1,810 行；旧版本的验收不能直接覆盖这些改动。

## 正式发布前必须补齐

### 1. 当前候选版本的签名产物和安装生命周期证据

当前提交的 [Build Desktop #38065266705](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/38065266705) 成功，涵盖绑定生成、前端测试/构建、两模块 Go 测试/vet、格式检查、Wails/NSIS 打包及原生辅助入口。

但这是 `push` 构建，Desktop/Core/installer 签名、签名后重新打包及 `Verify production installer trust policy` 全部跳过。它证明未签名构建可完成，不证明正式签名产物已验收。

本轮本地 NSIS 控制流夹具和新编译程序的辅助入口通过；真实安装、服务替换、UAC、卸载及已签名更新仍未执行。[安装事务验收记录](installer-transactions-2026-10-06.md) 也明确保留了这些边界。应使用同一候选提交生成的最终包，在隔离 Windows 10/11 环境完成：

- 干净安装、已发布 v2.7.0 覆盖升级、自定义目录与旧安装迁移。
- Core 服务启停、安装失败/取消后的回滚、重启恢复、卸载恢复。
- 普通用户与 UAC 取消、WebView2 缺失、正式签名验证，以及旧客户端到新版的完整自动更新。

验收应保留提交 SHA、安装包 SHA-256、签名状态、系统版本和结果。不要将本轮本地编译的 EXE 作为正式分发包。

### 2. 真实网络验收，以及可用 IPv6 环境下的结果

正常权限下的本轮完整 Go 测试仍失败：Core 有 4 个顶层测试失败（含子测试共 7 条 fail），Desktop 有 2 个顶层测试失败。全部集中在 IPv6 回环：

- Core：`TestIPv6ICMPRealLoopback`、`TestIPv6DNSRealUDPAndTCPFallback`、`TestIPv6DoHRealTLSAndCacheIsolation`、`TestDNSPodDualStackIPv6BootstrapAndIPv4WinnerCancellation`。
- Desktop：`TestDesktopIPv6ICMPRealLoopback`、`TestRuntimeBindingRealCoreIPv6Loopback`。

独立于 HypoMux 的宿主系统检查也失败：`ping -6 ::1 -n 1` 返回 `General failure`；.NET TCP6 客户端连接独立 `::1` 监听器被 Windows 以套接字访问权限拒绝。这支持环境故障判断，不能证明产品 IPv6 实现有缺陷，也不能替代产品验收。当前提交 CI 的 Go 测试通过提供了其他环境的正向证据。

真实 WFP、物理 IPv6/NAT64、已安装服务客户端、代理接管/恢复和正式签名安装包等检查默认跳过。发布前仍须对候选包完成真实双网卡/TUN/FakeIP/自定义 DoT、网卡断开再加入、退出/崩溃后的 DNS/路由/代理恢复，以及第三方代理冲突检查。继续保留热点等实验性功能边界；若正式承诺支持，须补手机端共享与停止恢复实测。

### 3. 为后续修复使用新的版本与发布说明

本地统一版本检查通过，但当前 `desktop/VERSION` 和 `CurrentVersion` 仍为 `2.7.0`。GitHub 上 v2.7.0 已于 2026-09-27 发布，对应提交 `d6fb799c08068d2bdea79d3a3521795adf98048d`。

更新器 `isNewerVersion` 对相同版本返回 false；以相同版本重新分发这些修复，已有 2.7.0 用户不会获得正常的新版提示。创建标签流程也拒绝把已有标签移动到新的提交。

按 [版本发布说明](../RELEASE_VERSIONING.md) 选择新版本、准备对应标签的发布说明并运行版本检查。发布工作流可以按新标签注入版本，因此这属于发布准备事项，不是要求开发期间每次提交都提升版本。本轮未更改版本或创建标签。

## 自动检查结果

| 检查 | 本轮结果 |
| --- | --- |
| 版本一致性 | 通过，2.7.0 / Windows 2.7.0.65535 |
| 桌面前端测试 | 59 个文件、398 项通过 |
| TypeScript 与 Vite 生产构建 | 通过 |
| Core 完整 Go 测试，正常权限 | 473 pass / 7 fail / 9 skip |
| Desktop 完整 Go 测试，正常权限，启用本地 NSIS 夹具 | 739 pass / 2 fail / 13 skip |
| 新编译程序原生安装/更新辅助入口 | 单独通过，验证非法范围和未签名包被拒绝、失败记录与包保留 |
| 新编译 Core 真实握手及 stopped 状态 | 单独通过 |
| Core/Desktop `go vet ./...` | 均通过 |
| Core/Desktop `go mod verify` | 均通过 |
| Core 构建、Desktop production 标签构建 | 均通过 |
| 定向 `go test -race` | DoT、UDP/并发、Updater、RuntimeBindingRecovery 通过，无竞态报告；不是完整 race 扫描 |
| Go 格式 | 当前提交 CI 通过；本地 ai_test.go 为换行差异，Git blob 经 gofmt 复核无差异 |
| 官网测试 | 33 项通过 |
| 官网类型检查、lint、生产构建及 postbuild | 均通过；40 页 SEO、28 个双语文档页、1,620 个本地链接/图片检查 |
| 官网子模块 | SHA 匹配，检查时无未提交变更 |
| `git diff --check` | 通过 |

Go 测试数量来自 JSON 的测试及子测试终态，父子记录会分别计数，不是互不重叠的独立场景数。完整 Desktop 执行时跳过的 Core 握手及原生助手两项，已在设置新构建路径后单独补跑通过，其余跳过不记作通过。

运行工具链为模块选择的 Go 1.26.9、Node 24.18.1；前端通过 `npm test` / `npm run build` 使用现有锁定依赖。CI 使用 Node 22 和固定 pnpm 10.34.5，并已执行 frozen-lockfile 安装。默认 pnpm 11 尝试依赖自动校验时中止，未强制清空依赖；漏洞扫描使用项目已有 pnpm 10。沙箱内遇到子进程、管道和私有 ACL 限制，正常权限复测后相关失败消失。未关闭当前网络、代理或防火墙来让测试通过。

## 依赖与安全检查

| 范围 | 结果与影响 |
| --- | --- |
| Core Go 源码依赖 | govulncheck 未发现漏洞 |
| Desktop Go 源码依赖 | 未发现可达漏洞或导入包漏洞；模块层有 GO-2026-5932，涉及未导入的 x/crypto/openpgp，不应按客户端可利用漏洞计数 |
| 桌面前端 | 2 条 high：braces 3.0.3、source-map-js 1.2.1 |
| 官网 | 1 条 high：source-map-js 1.2.1 |

`braces` 来自 `pixi-live2d-display → gh-pages → globby → fast-glob → micromatch`；产品加载的是 `pixi-live2d-display/cubism4` 浏览器入口，未发现产品源码调用这条文档发布工具链。公告涉及深度嵌套模式耗尽调用栈，当前公告无修复版本，应移除不必要的依赖链或记录限制与处置。来源：[GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)。

桌面 `source-map-js` 告警来自 `jsdom → css-tree` 的测试依赖，官网同名依赖也被 npm audit 标出。公告涉及恶意 indexed source map 阻塞事件循环，1.2.2 已修复，建议发布前更新锁文件并复跑构建/测试。没有证据表明最终客户端运行路径接收该类恶意 source map，不能把 audit 的 high 直接等同于已确认的用户端高危漏洞。来源：[GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q)。

当前 CI 只有 Engine 工作流调用 govulncheck，未看到桌面及前端漏洞门禁；建议把依赖扫描和有期限的例外说明纳入发布检查。本轮 Go 扫描覆盖两个源码模块，不等于对所有第三方 EXE/DLL 的完整漏洞审计。

## 放行条件与检查产物

建议先制作新版本 RC，完成上述安装和网络矩阵；依赖告警更新或有依据地记录处理结论后，再批准正式发布。若团队已有这些实测结果，应核对是否对应当前候选提交与最终包，而非要求机械重复旧记录。

原始测试 JSON、构建日志、漏洞扫描及 CI 步骤快照保存在仓库忽略目录 `.tmp/release-audit-2026-10-11/`。本轮仅新增此报告；未修改产品代码、版本或锁文件，未提交、签名、部署或发布。
