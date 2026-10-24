# v2.7.0 内更新失败：sing-box 占用修复（2026-10-24）

用户在 v2.7.0 内更新到首次发布的 v2.8.0 时安装失败，随后删除 GitHub Release。已确认这是安装器缺陷；发布前的签名/分发检查和隔离测试没有覆盖该真实升级场景。

## 故障证据

经用户 UAC 确认，只读复制 `C:\ProgramData\HypoMux\SetupTransaction` 的安装日志、事务记录与服务状态到项目 `.tmp/upgrade-failure-2026-10-24/`，没有运行真实安装、恢复命令或更改服务。

`setup.log` 显示：

```text
2026-10-24T21:37:32+08:00 prepared
2026-10-24T21:37:35+08:00 applying
2026-10-24T21:37:35+08:00 replaced C:\Program Files\HypoMux\hypomux.exe
2026-10-24T21:37:35+08:00 replaced C:\Program Files\HypoMux\bin\hypomux-engine.exe
2026-10-24T21:37:35+08:00 replaced C:\ProgramData\HypoMux\Core\bin\hypomux-engine.exe
2026-10-24T21:37:35+08:00 replaced C:\Program Files\HypoMux\bin\sing-box.exe
2026-10-24T21:37:35+08:00 apply FAILED: replace C:\ProgramData\HypoMux\Core\bin\sing-box.exe: open C:\ProgramData\HypoMux\Core\bin\sing-box.exe: The process cannot access the file because it is being used by another process.
2026-10-24T21:37:43+08:00 rolling back
2026-10-24T21:37:44+08:00 rolled back
```

事务已回滚，Core 服务为 Running / Automatic。另对事务中原先存在的 10 个文件/快捷方式逐项计算 SHA-256，全部与 `OldHash` 一致，确认本次失败没有留下部分新旧文件混用。该核对不代替完整网络功能复测。

## 根因与修复

旧升级脚本仅枚举并关闭路径匹配的 `hypomux-engine.exe`，也只检查该文件可写。旧服务停止后仍可能残留受保护 Core 目录下的 sing-box；安装器因此在已经替换桌面端和 Core 后才发现 sing-box 被占用。

修复两处：

- NSIS 升级屏障同时关闭同一 Core 目录内的 engine 和 sing-box，按完整路径匹配并等待退出，再检查 engine、sing-box 与 Wintun 文件的写访问。不会按名称结束其他目录的同名程序。
- 原生安装事务在写入首个文件前，先对所有待替换目标做无截断、无创建的写访问检查。任何目标仍被占用时，先报错并允许恢复原服务，避免先更换部分程序。

后续按用户要求，将产品版本统一改为 2.8.1。中英文 v2.8.1 发布说明沿用 v2.8.0 的完整内容，更新版本标识并补充此次升级修复；v2.8.0 原始说明保持历史记录。

## 回归结果

- 在独立临时目录，用 HEAD 原版脚本执行新增测试：被锁的 sing-box 未被识别，残留 sing-box 未被关闭，两项均按预期失败，证明测试可以抓住此次遗漏。
- 修复后，原有 Core 文件探测场景与新增 sing-box / Wintun 文件锁场景均通过；真实启动的无害 sing-box 夹具被关闭，另一目录的同名夹具保持运行。
- Windows 原生事务测试确认：后面的 sing-box 被锁时，前面的桌面端和 Core 字节均未替换，回滚后原服务状态恢复。
- `go test ./internal/setup`、桌面根包测试、隔离 NSIS 升级/UI/WebView/目录控制流测试均通过。
- `go vet ./...`、Desktop production 构建、原生辅助入口测试均通过。
- 完整 NSIS 安装器编译成功，未执行真实安装。未签名本地复测包位于 `.tmp/upgrade-failure-2026-10-24/HypoMux_Setup_2.8.0_upgrade-fix_unsigned.exe`，46,520,312 字节，SHA-256 `657ca7c147b4d0a7ed0b546708429e979f01183e337911055aef4d6d51b0ae4f`。它不是正式签名发布产物。

下一次正式发布应使用包含修复提交的 v2.8.1 标签，重新签名并复测 v2.7.0 内更新。旧 Actions run 或仍指向 `36f8743` 的 v2.8.0 标签不会自动包含修复。

## 尚未执行的分发操作

本轮只读查询确认：删除 GitHub Release 后，GitHub `update-channel/latest.json` 仍为 2.8.0，包含 CNB 与 GitHub 两个下载地址。删除 Release 不会自动撤回更新清单，也不能证明 CNB 镜像已撤回。正式分发状态需要另行同步处理；本轮没有修改远端渠道。
