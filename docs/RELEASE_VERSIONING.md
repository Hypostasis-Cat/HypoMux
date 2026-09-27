# 正式版、Beta 与 RC 发布

## 版本格式

- 正式版：`2.7.0`，Git 标签为 `v2.7.0`。
- Beta：`2.8.0-beta.1`、`2.8.0-beta.2`。
- RC：`2.8.0-rc.1`、`2.8.0-rc.2`。
- 同一基础版本按 `beta.2 < beta.10 < rc.1 < 正式版` 排序。

使用小写 `beta.N` / `rc.N`，序号从 1 开始，不接受 `beta1`、`bata1`、前导零或构建元数据后缀。三个基础版本段为 0–65535，预发布序号为 1–29999，以便映射 Windows 版本资源。

## 统一设置版本

在仓库根目录执行（只修改本地文件，不创建标签或发布）：

```powershell
go -C desktop run ./cmd/release-version -version 2.7.0 -write
# 后续预发布版本示例
go -C desktop run ./cmd/release-version -version 2.8.0-beta.1 -write
go -C desktop run ./cmd/release-version -check
```

`desktop/VERSION` 是本地版本源。命令同步前端展示、package.json、Core Taskfile、更新器、Wails 配置、Windows EXE / NSIS / MSIX 元数据。README 中面向公众的最新正式版标识由正式发布时维护，不随 Beta / RC 改动。

Windows 数字版本和产品展示版本分别生成。例如：

| 展示版本 | Windows 数字版本 |
| --- | --- |
| 2.8.0-beta.1 | 2.8.0.1 |
| 2.8.0-rc.1 | 2.8.0.30001 |
| 2.8.0 | 2.8.0.65535 |

这样安装包和 EXE 的版本资源保持纯数字，正式版排在同一基础版本的所有预发布版之后。不要手动修改生成的版本字段；重新生成 Wails build assets 后再执行版本同步命令。

## 发布步骤

1. 准备 `.github/release-notes/<完整标签>.md`，例如 `v2.8.0-beta.1.md`，内容不能为空；把代码和说明合入 main。
2. 在 main 上运行 **Create Release Tag**，填入完整标签。工作流先验证格式和发布说明，再同步 GitHub / CNB 标签。
3. 运行 **Build Desktop**，选择该标签，`signing_mode` 选择 `publish`。标签版本会在构建、测试、签名前注入所有产品版本字段，避免安装包文件名与内部版本不一致。
4. 可用 **Release Trust Smoke Test** 指定同一标签，只读核对对应更新渠道和签名。

普通 main / PR 构建会校验 `VERSION` 和生成字段是否一致。发布 Beta / RC 仍使用正式代码签名；`test` signing mode 只是签名测试，不是预发布渠道。

## 更新渠道

用户在 **设置 → 全局设置 → 更新渠道** 选择：

- **正式版**（默认）：只读取 `update-channel/latest.json`。
- **预览版（Beta / RC）**：同时读取正式渠道与 `update-channel-preview/latest.json`，选择版本最高的有效更新。

选择持久化保存，重启及升级后继续生效。安装了预发布版本也可以切回正式渠道；程序不会自动降级，等同版本或更高版本的正式版发布后再提示升级。预览渠道用户可以升级到正式版，且仍保留预览渠道偏好。

两个渠道使用同样的 Ed25519 清单验证、安装包 SHA-256 / 大小与 Authenticode 校验。正式渠道若误放预发布清单，客户端会拒绝该来源。首次预发布前预览渠道可能不存在，此时选择预览渠道仍可使用有效的正式渠道。

Beta / RC 的 GitHub 与 CNB Release 标为预发布，不设置为 latest，也不会覆盖正式渠道。旧版客户端继续读取原正式渠道，无需识别 beta / rc。首次参与测试的旧版用户需要先升级至含此功能的正式版，或手动安装新版预发布包。

发布操作须使用包含这些工作流改动的新提交；旧标签仍会使用旧工作流。两个渠道共享应用、配置和服务身份，不支持同时安装稳定版与预览版两个实例。
