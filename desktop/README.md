# HypoMux Desktop

HypoMux 的 Windows 桌面客户端，使用 Wails v3、React、TypeScript、Vite 与 Fluent UI React v9 构建。

桌面 WebView2 进程始终以普通用户权限运行。TUN、WFP、路由、DNS 与网络恢复操作由独立的 Go Core 承担；正式安装时 Core 注册为 `HypoMuxCore` Windows Service，开发环境可回退到原生 UAC 按需启动。

## 目录结构

```text
HypoMux/
├─ frontend/          React/TypeScript 前端、Wails bindings 与静态资源
├─ internal/
│  ├─ engineclient/   Core IPC 客户端与权限边界
│  ├─ platform/       Window、Tray、Dialog、Lifecycle 等平台适配
│  └─ services/       设置、聚合、TUN、分流、体检、更新等桌面服务
├─ build/             Wails 平台构建配置与 Windows NSIS 脚本
├─ qa/                必要的视觉与功能验证证据
├─ bin/               本地构建产物和运行依赖（不提交）
├─ main.go            Wails 桌面入口
└─ Taskfile.yml       开发、构建和打包任务
```

迁移与架构总文档位于仓库根目录：

- `FEATURE_INVENTORY.md`
- `UI_MIGRATION_MATRIX.md`
- `WAILS_ARCHITECTURE.md`
- `MIGRATION_PLAN.md`

## 开发

前端依赖使用 pnpm：

```powershell
cd frontend
pnpm install
pnpm run build
```

在项目目录启动 Wails 开发模式：

```powershell
wails3 dev
```

不要使用 `cmd start /b` 托管 Vite。需要独立启动时，应由 PowerShell `Start-Process` 直接运行 `node.exe` 与 Vite 脚本，并重定向标准输出和错误输出。

## 快速验证

```powershell
go test ./...
cd frontend
pnpm run build
```

Engine 测试与构建在仓库根目录的 `engine/` 模块执行。

## TUN 协议栈与持久化缓存

“设置 → 高级网络 → TUN 协议栈”支持 `system`（默认）、`mixed`
（系统 TCP + gVisor UDP）和 `gvisor`（完整用户态协议栈）。选择保存为
`tun_stack`，下次启动 TUN 生效；IPv4 回退配置保留同一选择。旧设置缺少该字段时
仍使用 `system`，不额外修改 endpoint-independent NAT 参数。

sing-box 默认启用 `experimental.cache_file`；使用 FakeIP 时同时启用
`store_fakeip`。缓存固定保存在 HypoMux 数据目录下的 `cache/sing-box.db`
（默认 `%USERPROFILE%\.hypomux\cache\sing-box.db`，支持 `HYPOMUX_DATA_DIR`），
不会因运行配置重新生成、IPv4 回退或 TUN 重启而清除。该文件包含域名映射，属于本地隐私数据。
远程规则集接入后可使用同一缓存机制；当前本地规则集仍使用原有文件更新流程，未增加远程下载或自动更新。

Windows 测试覆盖三种栈的内置 sing-box 配置检查，以及仅绑定本机 UDP、无 TUN/系统 DNS
改动的 FakeIP 缓存恢复测试。sing-box 缓存写入是异步的，刚分配但尚未落盘的映射在强制终止时仍可能丢失。

字段语义参考 [缓存文件文档](https://sing-box.sagernet.org/configuration/experimental/cache-file/)
与 [TUN 文档](https://sing-box.sagernet.org/configuration/inbound/tun/)。

## Windows 生产构建

```powershell
wails3 task windows:build
```

当前本地运行资源输出到 `bin/`。该目录、前端 `dist/`、生成的 bindings、Task 缓存与打包下载物均由 `.gitignore` 排除。

发布前仍需完成管理员环境下的 Service、TUN/WFP、覆盖升级、卸载恢复、WebView2 缺失以及 100%/125%/150% DPI 实机矩阵。
