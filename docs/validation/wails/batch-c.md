# 批次 C 验证报告：网络体检与支持日志

日期：2026-07-31

## 本批范围

- 新增网络体检页和支持日志页，未迁移或改写 TUN/WFP 网络控制。
- 体检网卡选择与首页共用 `AdapterService.SaveSelection`。
- 保留 v2.2.0 的诊断语义：源绑定 ICMP 10 次；绑定 TCP 作为最终可用性真值；ICMP 延迟、丢包和抖动继续作为质量证据。
- 增加源绑定、网关、DNS、跃点及自动/固定跃点检查。
- 增加最近 3 次、5 MiB、敏感信息脱敏、关键事件过滤和 10 秒重复聚合的支持日志。
- 聚合启动、成功、失败、停止和网络体检结果进入同一会话日志。

## 后端证据

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| 真实 Windows 网卡体检 | 通过，执行 `TestRealWindowsAdapterDiagnostic` |
| WinAPI ICMP | 10 次探测契约通过 |
| 绑定 TCP | `IP_UNICAST_IF` 与源地址绑定返回探测证据 |
| TCP 最终真值语义 | 单元测试通过 |
| 取消体检 | 单元测试通过 |
| 日志最近 3 次与脱敏 | 单元测试通过 |
| 日志 5 MiB 裁剪 | 单元测试通过 |
| 日志 10 秒重复聚合 | 单元测试通过 |

真实体检只读网络状态，未修改系统代理、路由、DNS 或网卡配置。当前机器已完成可用网卡真实探测；双物理网卡并行实机矩阵仍保留为发布前验收项。

## 前端与桌面证据

| 检查 | 结果 |
| --- | --- |
| `pnpm --dir frontend build` | 通过，2193 modules |
| TypeScript | 随前端构建通过 |
| Wails v3 bindings | 6 services / 36 methods / 14 models |
| `wails3 build` | 通过，生成 `bin/hypomux.exe` |
| 生产 EXE 启动 | `WaitForInputIdle` 成功，进程稳定后主动关闭 |
| 1120×800 浅色网络体检 | 通过 |
| 1120×800 深色网络体检 | 通过 |
| 1120×800 浅色支持日志 | 通过 |
| 启动体检交互 | 浏览器夹具触发并完成 |

Vite 对单个 JavaScript chunk 超过 500 kB 给出非阻塞警告；这不是本批功能或视觉验收失败，后续可按页面做动态加载。

## 视觉修正

第一张网络体检截图中，两项结果下方存在无意义空白。结果行改为按可用高度弹性分配，1120×800 下两张结果完整填充报告区域；没有增加统计卡或无业务意义的装饰。

截图：

- `docs/validation/wails/screenshots/batch-c/health-1120x800-final.png`
- `docs/validation/wails/screenshots/batch-c/health-dark-1120x800.png`
- `docs/validation/wails/screenshots/batch-c/logs-1120x800-final.png`

## 未在本批重复执行

- 安装包、托盘退出、关闭窗口隐藏到托盘、WebView2 缺失和 100%/125%/150% DPI 已在技术原型阶段验证；本批未改平台生命周期代码，因此未重复安装/卸载和缺失 Runtime 场景。
- 本批没有开始 P7 TUN/WFP/权限 Broker，不声明这些项目已迁移。
