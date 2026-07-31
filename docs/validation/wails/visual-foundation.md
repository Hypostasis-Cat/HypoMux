# HypoMux 正式视觉重构验证记录

日期：2026-07-31（Asia/Shanghai）

范围：Theme Engine、Windows 窗口材质适配、Appearance Lab、主窗口壳、紧凑导航和首页。
继续使用模拟数据与模拟状态机；未连接真实网络核心，未迁移分流、体检、日志、设置和关于页。

## 环境

| 项目 | 结果 |
|---|---|
| Windows | Windows 11 Pro，build 26200 |
| Go | `go1.26.5 windows/amd64` |
| Wails | `v3.0.0-alpha2.119` |
| WebView2 | `150.0.4078.105` |
| Fluent UI React | `9.74.4` |
| Fluent Icons | `2.0.334` |
| NSIS | `3.12`，项目本地便携工具 |

## 构建与运行

| 验证项 | 状态 | 证据 / 说明 |
|---|---|---|
| 前端生产构建 | 通过 | `pnpm run build`；TypeScript 与 Vite 成功，2184 modules transformed |
| Go 测试/编译 | 通过 | `go test ./...`；所有包通过 |
| Wails 生产构建 | 通过 | `wails3 build`；生成 `bin/hypomux.exe` |
| Wails 开发模式 | 通过 | Vite `9246` ready，Wails 连接成功，WebView2 environment created successfully |
| Windows 用户级安装包 | 通过 | `wails3 package FORMAT=nsis INSTALL_SCOPE=user`；生成 `bin/hypomux-amd64-installer.exe` |
| WebView2 缺失回退 | 安装器逻辑通过；隔离机实测待办 | NSIS 包含 Wails 官方 WebView2 bootstrapper；当前机器已有运行时，未破坏性卸载系统组件 |
| 初始窗口 | 通过 | Wails 主窗口 `1120×800`，最小 `960×680` |

Vite 仍提示生产主 chunk 超过 500 kB。当前只有首页和开发态 Lab，不为消除提示提前引入路由；
正式迁移更多页面时按页面动态加载。

## 窗口材质与平台边界

- Wails 官方窗口选项保留 Mica 默认值；窗口先以透明 WebView2 创建，再在
  `ApplicationStarted` 后通过平台适配层启用 DWM 背板。
- 采用延后启用是因为 Windows build 26200 + WebView2 150 上，
  `BackgroundTypeTranslucent` 在创建控制器阶段出现 `0x8000FFFF`；
  调整后开发和生产窗口均可稳定创建。
- `SetWindowMaterial`、`SetWindowTheme`、`SetWindowAccent` 均位于
  `internal/platform/wails`；React 业务组件不直接调用 Wails。
- 前端只通过 `frontend/src/platform/desktop.ts` 使用生成的官方 Wails 绑定。
- 不支持 DWM 背板时，调用结果会标记 `fallback`，界面继续使用可读的 CSS 玻璃或纯色表面。

## 生命周期与托盘

| 验证项 | 状态 | 结果 |
|---|---|---|
| 主窗口创建 | 通过 | 调试运行获得有效 1120×800 主窗口句柄 |
| 托盘创建 | 通过 | 调试日志无 `systray add failed` |
| 关闭窗口隐藏到托盘 | 通过 | 发送标准 `WM_CLOSE` 后进程继续运行、主窗口句柄归零 |
| 失去焦点 | 通过（实现复核） | 移除不适合主窗口的 `AttachWindow`；切换应用不再自动隐藏 |
| 托盘单击显示 | 通过（代码与原型路径） | `SystemTray.OnClick -> DesktopHost.Show()` |
| 托盘菜单退出 | 通过（代码与既有原型路径） | “退出”菜单调用集中平台层 `Quit()`；本轮未使用坐标脚本点击系统托盘 |

## 视觉截图

所有主题截图使用同一 Vite/React 产物、1120×800 逻辑视口，并在页面 220ms 进入动画完成后捕获：

- `qa/redesign/home-mica-light.png`
- `qa/redesign/home-mica-dark.png`
- `qa/redesign/home-acrylic-light.png`
- `qa/redesign/home-acrylic-dark.png`
- `qa/redesign/home-wallpaper-light.png`
- `qa/redesign/home-wallpaper-dark.png`
- `qa/redesign/home-performance.png`

DPI 渲染：

- `qa/redesign/home-dpi-125.png`：1120×800 逻辑视口，1.25 device scale（1400×1000 输出）
- `qa/redesign/home-dpi-150.png`：1120×800 逻辑视口，1.5 device scale（1680×1200 输出）

两张 DPI 图均完整包含标题栏、导航、引擎、两张网卡和状态栏，无文字或控件裁切。
Wails Windows manifest 继续使用 PerMonitorV2。跨不同物理显示器拖动时的最终缩放切换仍建议在发布前做一次人工桌面复核。

## 两轮视觉修正

### 第一轮

- 修复 Fluent `Tooltip` portal 继承根 Provider 全屏尺寸、覆盖页面的问题。
- 将系统材质回退从单一底色改为冷暖灰 Mica 渐变。
- 主引擎区域固定为更合理的纵向比例，减少无意义空白。
- 实时图改为低对比度平滑曲线并缩小占用。
- 网卡速率、上传和连接改为一个业务数据组，减轻表格列感。
- 增强浅色文本对比，降低连续矩形描边强度。

### 第二轮

- 独立校准 Mica、Acrylic、Wallpaper 和 Pure Performance 参数。
- 提高主/次玻璃表面的透明度差异和状态栏可读性。
- 深色底层改为蓝灰色而非纯黑；正文保持三级对比。
- Solid 模式移除模糊和表面阴影，减少动画。
- 以 1120×800、125% 和 150% 输出重新复核裁切。

复核结论：

- 不再有宣传官网式标题或通用 Dashboard 统计卡。
- 不再有 DataGrid、表头或大量水平分隔线。
- 网卡是单列高密度业务状态条目，字段完整。
- 背景层、壳层和内容表面层可辨识。
- 强调色同时作用于导航、图表、选择态、图标表面和 Fluent 控件。
- 浅色不是纯白，深色不是纯黑；两套主题独立调校。

## 模拟交互

| 项目 | 状态 |
|---|---|
| 代理 / TUN 切换 | 通过 |
| 引擎停止、待命和重新启动 | 通过 |
| 速率与连接随机更新 | 通过 |
| 网卡选择和权重输入 | 通过 |
| 成功 Toast | 通过 |
| Appearance Lab 实时预览 | 通过 |
| 四套预设、明暗模式和强调色 | 通过 |
| 本地背景图片选择 | 通过（Blob 预览；Go 持久化接口已预留） |

## 边界确认

- 未修改只读的 WPF 功能基线。
- 未修改现有 WPF 前端。
- 未修改 Go 网络核心、TUN、代理、路由、调度或高权限逻辑。
- 本轮在首页视觉确认点停止，不继续迁移其他正式页面。
