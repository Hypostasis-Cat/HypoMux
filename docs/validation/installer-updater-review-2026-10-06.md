# 安装与应用内更新链路第二轮审查

> 本文保留阶段性调查记录；最终实现和验证状态见 [安装事务验收](installer-transactions-2026-10-06.md)。


日期：2026-10-06。基于包含上一轮安装器修复的当前工作区。
本轮为审查与隔离复现，未进一步修改产品代码，也没有执行真实安装、卸载或服务变更。

## P1：覆盖更新失败缺少恢复旧版本的路径

`desktop/build/windows/nsis/project.nsi` 的 `StopCoreServiceForUpgrade` 先将已有服务设为 disabled。
`PrepareProtectedCoreDirectory` 随后运行 `protect-core-directory.ps1 -Phase Prepare`；
该脚本先删除旧的 hypomux-engine.exe、sing-box.exe、wintun.dll 和 libcronet.dll，安装器之后才写入新文件。
`RollbackFreshMachineInstall` 在不是首次安装时直接返回，也没有 `.onInstFailed` 恢复已有安装的服务配置和文件。

因此，停止服务后取消安装可能留下 disabled 服务；删除旧文件后发生写入失败、磁盘空间不足、
安全软件阻止或服务注册失败，可能留下不完整载荷，无法保证旧版本仍能使用。
此项由控制流静态确认；没有为了验证而破坏本机安装。

建议：写入前暂存并验证完整新载荷，记录原服务配置，保存可恢复的旧载荷；
统一处理取消、提取失败、权限整理失败和服务启动失败，提交完成前保留恢复能力。

## P1：中文路径在更新启动批处理中可能被错误解码

`desktop/internal/services/updater_windows.go` 将安装包绝对路径直接写入 `run-update.cmd`，
`os.WriteFile(..., []byte(content), ...)` 保存的是 UTF-8 字节，但没有为 cmd 建立匹配的代码页。
在使用其他活动代码页的 Windows 上，中文用户名或中文 TEMP 路径会造成路径被错误解释。
`InstallAndQuit` 只确认 cmd 进程已经创建，随后退出应用，不能保证安装器实际成功启动。

隔离复现：创建含中文路径的普通文件，以生产代码相同的 UTF-8 无 BOM 编码写入批处理，
在代码页 936 下检查同一字面路径。文件系统确认文件存在，批处理输出 `MISSING`。
没有调用真实安装器。

建议：避免把非 ASCII 路径写成批处理字面文本，可由 `%~dp0` 加受限的 ASCII 安装包文件名定位；
更完整的解决方式是使用支持 Unicode 的启动 API，并补齐应用退出后的启动确认和结果记录。

## P1：WebView2 安装失败会被当作可继续安装

`desktop/build/windows/nsis/wails_tools.nsh` 的 `wails.webview2runtime` 宏运行 bootstrapper 时，
没有接收退出码、检查启动错误，也没有在执行后重新确认运行时是否安装成功。
下载失败、权限错误或被拦截时，主安装流程仍可能显示完成，用户随后无法打开桌面界面。

隔离复现：从生产文件提取原宏，仅模拟注册表中不存在运行时，并用返回 67 的无副作用
NSIS 程序替代 Microsoft bootstrapper。原宏调用后继续执行后续 Section，测试安装器退出码为 0。
没有修改真实注册表或安装 WebView2。

建议：在项目维护的安装逻辑中检查启动结果、退出码及安装后的实际运行时状态。
`wails_tools.nsh` 标注为自动生成文件，修复应避免在重新生成时丢失。

## P2：更新失败和取消后仍清理已下载的安装包

`updater_windows.go` 的批处理在 `start "" /wait` 之后立即执行 `del /q`，
没有判断安装器退出码，失败、取消和正常安装都会进入相同清理路径。
应用已退出，用户也没有持久化的安装结果可查看，下一次尝试必须重新下载。

隔离复现：使用返回 67 的无副作用 NSIS 程序；保留相同的 `start /wait` 控制流，
将删除命令换为标记输出，结果为 `INSTALLER_EXIT=67` 后仍到达 `UNCONDITIONAL_CLEANUP_REACHED`。
复现没有删除文件。

建议：仅在确认成功后清理安装包；取消、启动失败和安装失败保留安装包与结果日志，允许重试。

## 已验证与边界

原有更新相关 Go 测试通过：

```powershell
go -C desktop test ./internal/services -run 'Test(Updater|InstallAndQuit|ValidateDownloadedInstaller|LaunchInstaller|VerifyDownloadedInstaller|IsRevokedCertificate|CheckInstallerRevocation)' -count=1
```

这证明已有测试覆盖的版本选择、下载回退、完整性验证和退出回调行为继续通过；
它们没有覆盖上述批处理编码、实际子安装器失败和覆盖更新回滚场景。
本轮复现材料放在忽略目录 `.cache/install-update-review/`，不属于产品发布载荷。
这些问题并不能证明就是此前那位首次安装用户报错的底层原因。
