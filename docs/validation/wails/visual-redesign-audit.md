# HypoMux 正式视觉重构审查

> 审查日期：2026-07-31
> 适用范围：`desktop` Wails 客户端
> 功能真值：归档的 WPF 项目快照（只读）

## 结论

当前 Wails 原型继续作为技术壳与交互线框使用，但不作为视觉基线。保留已经验证的
Wails v3、React、TypeScript、Vite、Fluent UI、托盘、安装包和 WebView2 结构；
首页视觉 DOM、旧主题文件和页面 CSS 全部重建。

本轮不修改旧版项目、不修改 WPF 前端、不接入真实网络核心，也不迁移分流、网络体检、
日志、设置和关于页面的正式视觉。

## 保留的状态和业务逻辑

| 能力 | 当前源码 | 处理 |
|---|---|---|
| 引擎 `stopped / starting / running / stopping` 四态 | `frontend/src/App.tsx` | 移入独立模拟状态 Hook |
| 系统代理 / 虚拟网卡 TUN 模式 | `frontend/src/App.tsx` | 保留语义，使用 Fluent `TabList` |
| 两张模拟网卡及名称、型号、IP | `frontend/src/App.tsx` | 修复中文乱码，保留完整字段 |
| 网卡选择、权重调节 | `frontend/src/App.tsx` | 保留，使用 Fluent `Checkbox`、`SpinButton` |
| 下载、上传、连接数随机更新 | `frontend/src/App.tsx` | 保留并集中到模拟状态 Hook |
| 合并速率、连接数、会话流量派生值 | `frontend/src/App.tsx` | 保留，避免重复计算 |
| 启停延时和成功 Toast | `frontend/src/App.tsx` | 保留，状态过渡缩短到视觉规范范围 |
| 窗口最小化、最大化、关闭到托盘 | `frontend/src/platform/desktop.ts` | 保留在单一平台门面 |
| 托盘打开、隐藏、退出和关闭拦截 | `internal/platform/wails/desktop.go` | 原样保留 |
| 1120×800 初始窗口和最小尺寸 | `main.go` | 保留 |

## 废弃的视觉 DOM

- 宣传式 `page-heading`、眉题、产品口号和版本说明。
- 原 `engine-panel` 的控制区/统计区左右分栏。
- `adapter-list-head` 与五列 `adapter-row` 表格结构。
- 绿色侧边框、独立统计格和传统表格式底栏。
- 导航中的文字常显和旧的禁用页面表现。
- 所有把背景、壳层和内容表面放在同一平面的容器结构。

## 废弃的 CSS

`frontend/src/app.css` 中现有视觉规则全部视为废弃，不做渐进覆盖，主要包括：

- `--shell-border` 等单层页面变量；
- 纯色 `colorNeutralBackground` 页面底色；
- 标题栏、导航和正文的 1px 强分隔线；
- 76px 带文字导航；
- 页面宣传标题排版；
- 二维网卡表格网格与表头；
- 绿色运行侧边框和高饱和状态色；
- 等强度卡片边界及旧响应式规则。

基础 reset、全窗口尺寸和 Wails caption 区域语义可在新样式中重新表达，不直接复用旧规则。

## 新主题目录

```text
frontend/src/theme/
├── appearance.types.ts
├── appearance.store.ts
├── appearance.presets.ts
├── createFluentTheme.ts
├── background.service.ts
├── material.tokens.css
├── semantic.tokens.css
├── typography.tokens.css
└── motion.tokens.css
```

主题 Store 负责实时预览和原型期本地存储，并通过 `AppearancePersistence` 接口为后续
Go 配置持久化预留边界。颜色、材质、透明度、模糊、圆角、密度和动画不得散落在组件中。

## 新组件树

```text
App
├── AppearanceProvider
├── WallpaperLayer                     # 第一层：窗口背景
└── AppShell                           # 第二层：低透明壳层
    ├── TitleBar
    ├── CompactNavigation
    └── PageViewport
        ├── HomePage
        │   ├── EngineHero             # 第三层：主玻璃表面
        │   │   ├── ThroughputDisplay
        │   │   └── ThroughputGraph
        │   ├── NetworkAdapterList
        │   │   └── NetworkAdapterItem
        │   │       └── NetworkHealthBadge
        │   └── RuntimeStatusBar
        └── AppearanceLab              # 仅开发环境
            └── AppearancePreview
```

`Button`、`Switch`、`Input`、`SpinButton`、`TabList`、`Tooltip`、`Dialog`、`Toast`、
`Badge` 和 `Popover` 均使用 Fluent UI React v9 官方组件。自定义组件只组合业务结构。

## Wails 平台边界

- `main.go` 默认启用 Wails 官方 `BackgroundTypeTranslucent + Mica`。
- `internal/platform/wails/DesktopHost` 集中实现 `SetWindowMaterial`、
  `SetWindowTheme` 和 `SetWindowAccent`。
- Windows 实现只负责 DWM 窗口属性；非 Windows 和不支持场景返回安全回退结果。
- 前端只有 `frontend/src/platform/desktop.ts` 可以导入 `@wailsio/runtime`。
- 页面和业务组件仅调用 `desktopPlatform`，不直接依赖 Wails。
- Wallpaper 与 Solid 采用 `None` 原生背板并由第一层背景负责呈现。

## 修改文件清单

### 重写

- `frontend/src/App.tsx`
- `frontend/src/app.css`

### 删除替代

- `frontend/src/theme.ts`（由 `frontend/src/theme/` 替代）

### 新增

- `frontend/src/theme/*`
- `frontend/src/state/useEngineState.ts`（最初的 `useEngineSimulation.ts` 原型已在接入真实服务后移除）
- `frontend/src/components/material/*`
- `frontend/src/components/shell/*`
- `frontend/src/components/home/*`
- `frontend/src/components/appearance/*`
- `frontend/src/pages/HomePage.tsx`
- `frontend/src/pages/AppearanceLab.tsx`
- `internal/platform/wails/appearance.go`
- `internal/platform/wails/appearance_windows.go`
- `internal/platform/wails/appearance_other.go`

### 调整

- `frontend/src/platform/desktop.ts`
- `internal/platform/platform.go`
- `internal/platform/wails/desktop.go`
- `main.go`
- `visual-foundation.md`

## 实施和验收顺序

1. 实现 Theme Engine、语义令牌、四套预设和背景服务。
2. 实现 Wails 材质/主题/强调色平台方法和浏览器安全回退。
3. 先完成开发态 Appearance Lab，覆盖官方控件及成功、警告、错误、禁用状态。
4. 重建标题栏、64px 导航和首页业务组件。
5. 运行并截取七组主题画面；完成两轮视觉审查和修正。
6. 执行前端构建、Go 测试、Wails dev/build 与 NSIS 打包；记录 1120×800、
   深浅色、DPI 和降级行为。
