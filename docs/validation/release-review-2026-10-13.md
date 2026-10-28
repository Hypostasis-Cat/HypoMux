# HypoMux 发布前验收（2026-10-13）

**整改后结论：前端与依赖问题已修复，可以进入 RC 验收；正式版仍需最终签名安装包和真实 Windows 网络生命周期验收。** 按用户要求，本地版本暂时保持 2.7.0。下文保留验收时的问题和证据，并记录修复后的验证结果。

主仓库提交：`444fb9d9e4bf9a3669fdcf066ddde66aa44320a9`。官网子模块：`19eb41b5899683aad1ac2c965bdc49a1d2107111`。验收开始时，主仓库两个说明文档及官网中英文热点说明已有未提交修改，已保留；因此官网构建结果覆盖本地文档修改，不能仅用子模块 SHA 表示完整输入。相对已发布 v2.7.0，主仓库有 243 个文件变化、19,649 行新增和 1,916 行删除，旧版本验收不能直接覆盖这些变化。

## 整改后复验

- 新规则输入框排除 `isComposing` 和 `keyCode=229` 的输入法事件；正常 Enter 仍能添加规则。新增两项回归覆盖候选字确认和组合结束后的正常提交。
- 活动连接的排序栏改为有名称的控件组，移除孤立的 row/columnheader/aria-sort。按钮使用稳定名称，并暴露当前选中状态，关联排序方向说明；变更继续通过 live status 播报。浏览器验证孤立表格角色数量为 0，键盘 Enter 排序正常。
- 搜索清除按钮提供中文“清空搜索”和英文“Clear search”，新增两项回归验证清除后恢复完整列表。
- 桌面与官网均锁定 `source-map-js` 1.2.2。桌面使用 CI 相同的 pnpm 10.34.5 移除 `pixi-live2d-display` 的 gh-pages 文档发布依赖及其 braces 链，保留 Cubism 浏览器运行入口；没有安装注册表中不存在的 braces 3.0.4。

| 整改后检查 | 结果 |
| --- | --- |
| 桌面前端完整测试 | 60 个文件、419 项通过，包含 Live2D/皮肤测试 |
| 桌面 TypeScript / Vite 生产构建 | 通过 |
| pnpm frozen-lockfile 安装（忽略依赖脚本） | 通过 |
| 官网 npm ci（忽略依赖脚本） | 通过 |
| 官网测试 / lint / 类型检查 / 生产构建 / postbuild | 33 项测试及全部检查通过 |
| 桌面前端 / 官网依赖审计 | 均为 0 条告警 |
| Desktop production 构建 / 版本一致性 | 通过，版本仍为 2.7.0 |
| 浏览器复核 | 键盘排序、排序状态、中文搜索清除正常；控制台无 error |
| 两仓库 git diff --check | 通过 |

复验日志与截图保存在 `.tmp/release-fixes-2026-10-13/`。这些整改尚未提交到 Git；下方 CI 记录对应验收时的提交，不能当作工作区整改后的远端 CI 结果。Go 后端源码未改，下方 Go 测试和 race 数据保留自验收阶段。

## 验收时的前端问题（已修复）

### P2：中文输入法确认候选字会提前添加分流规则

`desktop/frontend/src/pages/RoutingPage.tsx:852` — 新规则输入框只检查 `event.key === "Enter"` 和非空文本，没有排除输入法组合事件。中文输入法用 Enter 确认候选字时，页面会调用 `addRule()`；校验通过后规则进入自动保存流程，输入框还可能被清空。用户本来只是在确认文字，却改变了分流配置。

专项复现向真实 React 页面发送 `compositionStart`、输入变化以及 `Enter / isComposing=true / keyCode=229`。预期不调用规则校验，实际调用一次，匹配值为 `中文.exe`。这是产品代码复现失败，与首次测试的沙箱权限错误无关。建议添加组合输入保护，并覆盖“确认候选字不添加、组合结束后再按 Enter 才添加”的回归场景。项目 AI 助手的消息框已使用 `nativeEvent.isComposing` 保护，可统一行为。

原始复现日志和临时测试副本位于 `.tmp/release-audit-2026-10-13/ime-repro.log`、`ime-repro.test.tsx`。测试副本原执行位置为 `desktop/frontend/qa/release-audit-ime.test.tsx`，复跑时需恢复该位置以解析相对导入。本轮已移出默认测试目录，未修改产品实现。

### P2：活动连接表头缺少所属表格语义

`desktop/frontend/src/pages/ConnectionsPage.tsx:735` — 表头使用 `role="row"`，子项使用 `columnheader` 和 `aria-sort`，祖先中没有 table/grid/treegrid。浏览器 DOM 检查确认表头及五个列标题的表格祖先都为空，可访问性树也没有相应表格。

这会影响辅助技术对列标题、排序状态和数据关系的理解。建议根据现有卡片列表设计，把排序按钮组织成有名称的排序控件组并移除孤立的表格角色，或者实现完整、相互关联的表格语义；只给外层补一个角色而不处理数据行并不完整。依据：[WAI-ARIA row 的上下文要求](https://www.w3.org/TR/wai-aria-1.2/#row)。

### P3：中文连接搜索框的清除按钮仍使用英文名称

`desktop/frontend/src/pages/ConnectionsPage.tsx:635` — SearchBox 没有给内置清除按钮提供本地化名称；中文界面的可访问性树显示 `button clear`。建议统一为“清空搜索”等明确中文标签。这属于语言和可访问性细节，不单独阻塞发布。

## 前端验收结果

桌面前端使用项目现有的开发验收夹具，配置保存在独立浏览器键下；未操作用户真实网络配置。设置页的浅色和深色预览、项目最小尺寸 960×680 下的 DNS 控件、新增地址后的自动聚焦、非法 DoT 地址的行内提示和错误聚焦均正常。活动连接的停止空状态、15 条示例连接分组、Enter 展开及搜索筛选正常；搜索 `msedge` 显示 4/15。

官网生产构建的首页、下载入口与下载页导航正常；390 像素宽首页未发现横向越界。查看的官网页面没有控制台 error/warn；桌面预览仅出现 Wails 的浏览器模式提示。官网自动检查覆盖 40 页 SEO、28 个双语文档页、1,622 个本地链接/图片和部署资源。

这些浏览器预览不等同于 Windows 原生 WebView2、托盘、窗口行为和 100%/125%/150% DPI 验收，也不证明真实引擎与界面的所有交互均已执行。未进行全项目逐行审计或完整可访问性认证。

## 验收阶段自动检查

| 检查 | 本轮结果 |
| --- | --- |
| 桌面前端现有测试 | 60 个文件、415 项通过 |
| 桌面 TypeScript / Vite 生产构建 | 通过 |
| 官网测试 | 33 项通过 |
| 官网 lint / 类型检查 / 生产构建 / postbuild | 全部通过 |
| Engine 默认 Go 测试 | 495 pass / 0 fail / 6 skip |
| Desktop 默认 Go 测试 | 723 pass / 0 fail / 18 skip |
| 两模块 go vet / go mod verify | 全部通过 |
| Engine 构建 / Desktop production 标签构建 | 均通过 |
| 隔离 NSIS 安装器控制流 | 4 个顶层场景、含子项 27 条 pass，0 fail / skip |
| 新编译 Core IPC 握手与 stopped 状态 | 补跑通过 |
| 新编译桌面安装/更新辅助入口 | 补跑通过；非法范围与未签名假包被拒绝 |
| DoT / UDP / 调度相关定向 race | 通过，无竞态报告；不是全量 race |
| 版本一致性 / git diff --check | 通过 |
| 中文输入法新增规则专项复现 | 1 项失败，确认上述 P2 缺陷 |

Go 数量来自 JSON 中测试及子测试的终态，父子分别计数，不等于独立用例数量。NSIS、IPC 和原生辅助入口补跑覆盖了默认 Desktop 的六个顶层跳过项；其余跳过不能记作通过。

首次沙箱内测试遇到临时文件重命名、ACL/套接字等限制；以上测试结论使用正常权限复测结果。此前验收记录中的 IPv6 回环失败本轮未复现：两模块默认测试完整通过。

当前提交的 [Build Desktop #38352517836](https://github.com/Hypostasis-Cat/HypoMux/actions/runs/38352517836) 成功。但它是 push 构建，Desktop/Core/installer 签名、签名后重新打包及 `Verify production installer trust policy` 都被跳过，不能把它当作正式签名包验收。

## 验收时的依赖告警（已清除）

| 范围 | 本轮结果 |
| --- | --- |
| Engine govulncheck | 未发现漏洞 |
| Desktop govulncheck | 0 个可达漏洞、0 个导入包漏洞；模块层 GO-2026-5932 涉及未导入的 x/crypto/openpgp |
| 桌面前端依赖审计 | 2 条 high：braces 3.0.3、source-map-js 1.2.1 |
| 官网依赖审计 | 1 条 high：source-map-js 1.2.1 |

`source-map-js` 位于构建/测试依赖链，扫描记录显示 1.2.2 可修复；建议更新锁文件并复跑现有检查。公告涉及恶意 source map 导致事件循环拒绝服务。[source-map-js 公告](https://github.com/advisories/GHSA-68fv-2mgg-jv7q)

`braces` 来自 `pixi-live2d-display → gh-pages → globby → fast-glob → micromatch`，属于发布工具依赖链。产品使用的是 Cubism 浏览器入口，本轮没有确认客户端运行路径可达该漏洞；建议移除不必要的工具链，或核实可用修复版本后更新。公告涉及深层模式导致调用栈耗尽。[braces 公告](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)

审计的 high 等级不直接等于已确认的用户端高危漏洞，但正式发布前应完成更新或留下明确、可复核的处置结论。Go 扫描不覆盖所有第三方 EXE/DLL。

## 正式版放行条件

1. **已完成：** 修复中文输入法、活动连接可访问性和搜索按钮语言问题，补回归验证。
2. **已完成：** 处理前端和官网依赖告警，整改后测试、构建和审计通过。
3. 使用新版本及对应发布说明。[GitHub 最新正式版](https://github.com/Hypostasis-Cat/HypoMux/releases/tag/v2.7.0) 仍为 2026-09-27 发布的 v2.7.0，本地版本也为 2.7.0。同版本无法正常向已有用户提示升级；发布工作流支持按新标签注入版本，开发阶段保持旧版本不属于独立代码缺陷。
4. 用最终签名安装包完成 Windows 10/11 干净安装、v2.7.0 覆盖升级、自定义目录、UAC 取消、WebView2 缺失、Core 服务替换/恢复、卸载和完整自动更新。保留提交 SHA、包 SHA-256、签名、系统与结果；隔离 NSIS 夹具不能替代真实安装生命周期。
5. 在真实双网卡环境验证代理与 TUN、DNS/DoH/DoT、IPv4/IPv6、网卡断开重连、第三方代理共存，以及正常停止/异常退出后的代理、DNS、路由恢复。真实 WFP、公共 IPv6/NAT64 等默认跳过项须在适用环境补齐。
6. 保持聚合热点的实验性边界；若列为正式承诺，补手机联网、热点停止恢复及独立热点网卡实测。补原生 WebView2、托盘、窗口及 DPI 矩阵。

## 验收产物

验收日志、漏洞扫描、CI 步骤快照和专项复现保存在忽略目录 `.tmp/release-audit-2026-10-13/`；整改后的记录位于 `.tmp/release-fixes-2026-10-13/`。整改涉及两处前端实现及回归测试、桌面 pnpm 覆盖与锁文件、官网 npm 覆盖与锁文件。本地版本按用户要求保持不变。未创建标签、签名、安装或发布；原有工作区改动保留。

![整改后活动连接排序与搜索预览](../../.tmp/release-fixes-2026-10-13/connections-fixed.jpg)
