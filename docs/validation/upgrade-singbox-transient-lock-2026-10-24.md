# v2.7.0 内更新到 v2.8.1 失败：sing-box 短暂占用（2026-10-24）

v2.8.1 的升级修复（见 [sing-box 占用修复](upgrade-singbox-lock-2026-10-24.md)）发布后，用户从 v2.7.0 应用内更新仍然失败，随后删除 GitHub Release。

## 故障证据

经用户 UAC 确认，只读复制 `C:\ProgramData\HypoMux\SetupTransaction` 到 `.tmp/upgrade-failure-2026-10-24-b/`：

```text
2026-10-24T22:48:12+08:00 prepared
2026-10-24T22:48:16+08:00 applying
2026-10-24T22:48:16+08:00 apply FAILED: payload replacement preflight: C:\Program Files\HypoMux\bin\sing-box.exe is not replaceable: open ...: The process cannot access the file because it is being used by another process.
2026-10-24T22:48:18+08:00 rolling back
2026-10-24T22:48:18+08:00 rolled back
```

- 新加的预检生效：没有替换任何文件，事务回滚，Core 服务恢复为 Running / Automatic。
- 这次被占用的是桌面目录 `Program Files\HypoMux\bin\sing-box.exe`，不是 Core 目录。v2.7.0 桌面端不会长期运行这份 sing-box；旧更新脚本 `run-update.cmd` 等桌面进程退出后才启动安装包。
- `StopCoreProcessesForUpgrade` 在 apply 前已对同一文件做过写访问探测并通过。几秒后 Go 预检打开失败。
- 事后以管理员身份复测：该文件在 Read|Write、Read|Delete、Read|Write|Delete 三种共享模式下都能写打开，没有任何进程加载它。Defender 实时防护开启，无排除项。
- journal 显示两份 sing-box 和 wintun 的新旧 SHA-256 完全一致，本次升级本不需要改写它们。

结论：这是一次几秒内就释放的占用，持有者没有确认。最可能是安全软件在写探测关闭后重新扫描 82 MB 的可执行文件。安装器只尝试一次就回滚。

## 修复

- `Transaction.Apply` 对内容不变的文件（目标存在、旧哈希 = 新哈希、当前文件哈希仍等于新哈希）不做预检、不改写，只执行 `ValidateTarget` 和 `PrepareTarget`，Core 目录 ACL 仍会重新加固。`Commit` 照常校验最终哈希。
- 预检打开和替换复制遇到 `ERROR_SHARING_VIOLATION` / `ERROR_LOCK_VIOLATION` 时重试，最长 20 秒，间隔 250 毫秒。超时仍按原逻辑失败并回滚。重试仍在 NSIS 事务助手的 180 秒超时之内。真正在运行的 sing-box 会映射镜像，持续占用，仍会被拒绝。

## 回归结果

- 新增 `TestWindowsTransientSidecarLockIsRetried`：占用 400 毫秒后释放，Apply/Commit 成功。重试窗口设为 0 时，复现出与现场一致的报错。
- 新增 `TestWindowsUnchangedSidecarIsNotRewritten`：相同内容的 sing-box 被持续占用时，升级仍然成功，其余文件正常替换。
- 原有 `TestWindowsPreflightLockedSidecarDoesNotReplaceEarlierFiles`（持续占用、内容变化）仍然在替换任何文件之前就失败并恢复服务。
- `go vet ./...` 通过。Desktop `go test ./...` 只有已知的两个本机 IPv6 回环用例失败（`TestDesktopIPv6ICMPRealLoopback`、`TestRuntimeBindingRealCoreIPv6Loopback`，见 [发布前审查](release-review-2026-10-24.md)），其余全部通过。
- 未编译或签名安装包，也未执行真实安装。

## 发布前仍需处理

- 用新签名包实测从 v2.7.0 应用内升级。
- 删除 Release 不会撤回 `update-channel/latest.json`，需要确认它没有继续指向已删除的版本。
