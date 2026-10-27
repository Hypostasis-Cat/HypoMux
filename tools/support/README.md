# HypoMux 支持与诊断工具

本目录收录随官方安装包分发的自助诊断与修复脚本。双击对应的 `.cmd` 启动器即可使用；所有脚本只读取本机信息或执行下述明确列出的修复动作。

## 入口

| 启动器 | 作用 | 是否需要管理员 |
| --- | --- | --- |
| `Start-HypoMux-Network-Diagnostics.cmd` | 采集网络与聚合运行诊断（网卡、路由、DNS、引擎日志摘要等），全程只读、不改动任何网络设置 | 否 |
| `Start-HypoMux-Install-Diagnostics.cmd` | 采集安装/升级事务诊断（SetupTransaction 报告、安装目录、服务状态），输出到 `%TEMP%` 下的独立报告目录 | 否 |
| `Start-HypoMux-Repair.cmd` | 运行升级修复（`Repair-HypoMuxUpgrade.ps1`），默认只做只读检查；`-Repair` 时经 UAC 提权执行修复 | 检查否；修复是 |

## 各脚本说明

- `Collect-HypoMuxNetworkDiagnostics.ps1`：只读采集；输出自动脱敏（去除用户目录、URL 参数、疑似密钥/令牌与 Authorization 头），`settings.json` 按白名单字段复制并排除 AI 凭据与 sing-box 控制器密钥。采集结果用于附带在支持反馈中。
- `Collect-HypoMuxInstallDiagnostics.ps1`：只读采集安装事务与目录布局信息；`Test-HypoMuxNetworkDiagnostics.ps1` 是网络采集脚本的参数化自测（quoting 与超时路径）。
- `Repair-HypoMuxUpgrade.ps1`：
  - 默认（无参数）只做只读检查，报告安装布局、Core 服务与事务状态。
  - `-Repair` 需 UAC 提权：仅停止可执行路径核实属于本安装目录的进程与 Core 服务，调用已验证 Authenticode 签名（SignPath Foundation）的恢复程序，并清除 Core 文件的只读属性后探测可写性。**不会**删除配置、重置网络或修改杀软设置。
  - 完整边界见脚本头部注释；中文使用说明见 [使用说明.txt](使用说明.txt) 与 [安装诊断说明.txt](安装诊断说明.txt)。

## 反馈

提交问题（GitHub Issues 或 CNB）时，请附上诊断脚本输出的报告目录/压缩包。报告已自动脱敏，但仍请自行确认其中不含你手动输入的敏感信息。
