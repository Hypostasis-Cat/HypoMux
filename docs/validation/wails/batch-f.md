# 批次 F 验证报告：原生 UAC 与风险提示解耦

日期：2026-07-31

## 本批范围

- 保留普通权限 Wails UI + 独立高权限 Go Core。
- 删除“允许独立核心获取管理员权限？”自定义确认 Dialog。
- 无真实风险时，只读预检后直接连接 Core Service 或触发开发版 `runas` 原生 UAC。
- UAC 取消在首页显示 Toast：“未获得管理员权限，聚合未启动”。
- 共享网关、第三方 TUN、WFP 异常和资源缺失等问题继续由独立风险 Dialog 展示。
- Core 客户端增加“Windows Service 优先、服务未安装时开发版 runas 回退”的选择边界。

## 启动状态机

```text
启动聚合
  → 普通权限只读预检
  → 无问题：Core Service / 原生 runas
  → 有 warning：风险 Dialog → 继续 / 返回修改 / 查看详情
  → 有 blocker：风险 Dialog → 返回修改 / 查看详情；继续禁用
  → UAC 取消：Toast，保持 stopped
```

权限本身不再是一条预检风险，也不会触发自定义 Dialog。

## Core Service 客户端边界

- 服务名：`HypoMuxCore`。
- 固定管道：`\\.\pipe\HypoMux-Core-Service`。
- 普通 UI 使用 `SERVICE_QUERY_STATUS` 查询服务状态，不申请服务控制权限。
- UI 从 SCM 获取服务 PID，并用 `GetNamedPipeServerProcessId` 验证服务管道身份。
- 服务未安装时才回退到现有一次性令牌 `runas` Core。
- 服务已安装但停止、管道损坏或身份不匹配时直接失败，不绕过到临时 Core。

服务宿主、服务端 ACL/客户端身份验证和安装器注册尚未实现，不能把正式 Service 路径标记为完成。

## 验证

| 检查 | 结果 |
| --- | --- |
| Desktop `go test ./...` | 通过 |
| Service 未安装时回退 runas | 单元测试通过 |
| 已安装 Service 异常时禁止回退 | 单元测试通过 |
| 前端 TypeScript/Vite 生产构建 | 通过，2194 modules |
| Production-tag Wails Host 编译 | 通过 |
| 无风险启动的自定义 Dialog 数量 | 0 |
| 模拟 UAC 取消 Toast | 文案完全匹配 |
| warning 风险操作 | 继续 / 返回修改 / 查看详情 |
| blocker 风险操作 | 继续存在但禁用，不能绕过 |
| 浏览器控制台错误 | 0 |

截图：

- `docs/validation/wails/screenshots/batch-f/native-uac-cancel-toast-1120x800.png`
- `docs/validation/wails/screenshots/batch-f/network-risk-dialog-1120x800.png`

视觉 QA 使用验证进程内临时静态服务，没有启动 Vite 或外部长驻进程；验证结束后标签、viewport 和 9246 端口均已清理。

## 下一步

1. 实现 `HypoMuxCore` Windows Service 宿主。
2. 服务端创建带 ACL 的固定 Named Pipe，并验证客户端 PID、用户 SID、会话和安装身份。
3. NSIS 安装、升级和卸载服务；处理服务恢复策略与版本一致性。
4. 签名安装包内验证 UI `asInvoker`、服务权限和原生 UAC 回退。
5. 再进行真实 TUN、WFP、路由、DNS 和崩溃恢复矩阵。
