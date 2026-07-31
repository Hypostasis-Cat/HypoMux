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

## Windows 生产构建

```powershell
wails3 task windows:build
```

当前本地运行资源输出到 `bin/`。该目录、前端 `dist/`、生成的 bindings、Task 缓存与打包下载物均由 `.gitignore` 排除。

发布前仍需完成管理员环境下的 Service、TUN/WFP、覆盖升级、卸载恢复、WebView2 缺失以及 100%/125%/150% DPI 实机矩阵。
