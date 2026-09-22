# MTU 检测

入口：网络检测 → MTU 检测。先停止网络服务，选择网卡与允许 ICMP Echo 的 IPv4 目标，点击“检测推荐 MTU”，确认后应用；原值保存在设置目录的 `mtu-originals.json`，可在重开应用后恢复。

检测不修改网络配置。探针通过 Windows `IcmpSendEcho2Ex` 指定源 IPv4，并设置 DF 标志，搜索 576 到当前接口 MTU 的可通过范围（包括 20 字节 IPv4 头和 8 字节 ICMP 头），最后复测三次。仅明确的 `IP_PACKET_TOO_BIG` 缩小上界；超时重试后仍无响应、其他错误、取消或配置变化均不产生推荐值。达到当前 MTU 时显示无需修改，不推断更大数值可用。整个检测最多 50 秒。

应用只接受后端五分钟内的检测结果，重新检查网卡 GUID、索引、源地址和当前 MTU。修改前先原子保存原值。Core 的 `mtu.set` 再检查管理员权限、停止状态、GUID、参数范围和预期旧值，然后修改并回读验证。通信失败时保留恢复记录，界面重新读取实际值。桌面 WebView 不提权。

第一版仅修改 IPv4 的 ActiveStore，不修改 IPv6 或持久系统配置；重启后回到系统配置。结果只代表所选源地址到目标的路径，外部 VPN、路由变化和 ICMP 过滤均可能影响检测，不承诺提速。

自动验证包含二分边界、超时、取消、复测不一致、恢复记录持久化、Core 参数与协议契约、界面确认和失效状态。Windows 实机只读烟测可设置 `HYPOMUX_MTU_SMOKE_ADAPTER` 后运行 `go test ./internal/services -run TestMTUWindowsReadOnlySmoke -v`。实际修改/恢复仍需在可中断网络的 Windows 环境手工验证。

参考：[IcmpSendEcho2Ex](https://learn.microsoft.com/en-us/windows/win32/api/icmpapi/nf-icmpapi-icmpsendecho2ex)、[Set-NetIPInterface](https://learn.microsoft.com/en-us/powershell/module/nettcpip/set-netipinterface)。
