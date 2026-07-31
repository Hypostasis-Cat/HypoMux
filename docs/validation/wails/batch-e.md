# 批次 E 验证报告：独立高权限 Core 与鉴权 IPC

日期：2026-07-31

## 本批范围

- 把 Core 进程启动和 JSONL 协议传输从桌面客户端中拆为 `CoreLauncher`。
- 普通代理继续使用普通权限 stdio Core；TUN 只允许使用按需管理员 Core。
- Windows 使用 `ShellExecuteExW` + `runas` 启动 Core，桌面 Host 不提权。
- 增加本地命名管道、受限 ACL、双向 PID 校验、一次性令牌与协议握手。
- 预检通过后显示 Fluent UI 权限确认 Dialog；用户取消不调用 `engine.start`。
- 构建阶段归位 Core、sing-box、wintun、libcronet，并写入 NSIS 安装模板。

本批未自动确认真实 UAC，未启动 TUN、sing-box，未写系统代理、路由或 WFP 规则。

## 安全边界

| 项目 | 实现 |
| --- | --- |
| Host 权限 | Wails/WebView2 保持普通用户权限 |
| 管道范围 | `PIPE_REJECT_REMOTE_CLIENTS`、首实例、单实例 |
| ACL | 当前用户 SID、内置 Administrators 与 SYSTEM；兼容标准用户输入管理员凭据 |
| Host 验证 Core | `GetNamedPipeClientProcessId` 必须匹配 `ShellExecuteExW` 返回的进程 |
| Core 验证 Host | `GetNamedPipeServerProcessId` 必须匹配启动参数中的 Host PID |
| 会话凭据 | 256 位加密随机一次性令牌，常量时间比较，不写日志 |
| 协议 | 认证成功后才进入 protocol v1 JSONL |
| 取消 | `ERROR_CANCELLED` 映射为 `ErrElevationCancelled`，不保留 Core 会话 |

## 自动化证据

| 检查 | 结果 |
| --- | --- |
| Engine `go test ./...` | 通过 |
| Desktop `go test ./...` | 通过 |
| 正确 PID + 正确令牌 | 通过 |
| 错误一次性令牌 | 被拒绝 |
| 错误 Core PID | 被拒绝 |
| 错误 Host PID | 被拒绝 |
| 模拟 UAC 取消 | 返回取消错误，Core 未协商 |
| 真实 Windows TUN 只读预检 | 通过，前后默认路由别名一致 |
| `pnpm run build` | 通过，2194 modules |
| Production-tag Go/Wails Host 编译 | 通过，生成 `bin/hypomux.exe` |
| 独立 Core 编译与 hello 握手 | 通过，生成 `bin/hypomux-engine.exe` |
| 运行资源归位 | `sing-box.exe`、`wintun.dll`、`libcronet.dll` 均位于 `bin` |

本轮环境未提供可调用的 `wails3` CLI 与 `makensis`，因此没有重复执行 Wails CLI 编排和 NSIS 安装器生成；生产标签 Host 编译、前端构建、Core 编译和 NSIS 输入文件已分别验证。安装包/UAC 实机项仍保持未完成。

## UI 行为与视觉 QA

- 1120×800 浅色权限确认：通过。
- 1120×800 深色权限确认：通过。
- 取消按钮关闭 Dialog，启动按钮恢复可用，Core 保持 stopped：通过。
- 第三方 TUN 阻断时不存在“允许并继续”，只提供“返回检查网卡”：通过。
- 控制台错误：0。
- 第一轮深色截图发现 warning 背景过重；降低 Fluent token 混合强度后复拍通过。

截图：

- `docs/validation/wails/screenshots/batch-e/tun-uac-confirmation-1120x800-final.png`
- `docs/validation/wails/screenshots/batch-e/tun-uac-confirmation-dark-1120x800-final.png`

视觉 QA 使用验证进程内部的临时静态文件服务，没有启动 Vite 或外部长驻子进程。验证完成后浏览器标签、viewport 覆盖和 9246 端口均已清理。

## 尚未完成

- 签名安装包内真实 UAC 同意、取消、超时和错误提示。
- WFP/BFE 修复。
- TUN 外部联网验证和成功判定。
- DoH/WFP 单次受控兼容重启。
- 周期看门狗。
- 停止、Core 崩溃、Host 崩溃、托盘退出后的路由、Wintun、sing-box、WFP 残留矩阵。
