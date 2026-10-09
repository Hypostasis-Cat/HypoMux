HypoMux 一键网络排查

用户只需：
1. 解压本工具包，保持 HypoMux 的 TUN 开启，并让故障保持可复现。
2. 双击 Start-HypoMux-Network-Diagnostics.cmd，等待约 2–5 分钟。
3. 把桌面新生成的 HypoMux-Network-日期时间.zip 发回。不要只截屏。
   桌面不可写时会自动改存系统临时目录，以窗口最后显示的路径为准。

不需要安装软件，使用 Windows PowerShell 5.1 和系统自带 curl。
无需主动以管理员身份运行。个别信息无权读取时会保留错误，不修改权限。
不会重置网络、清空 DNS、关闭其他代理、切换网卡或停止 HypoMux。
不会自动上传。报告包含 IP、路由、进程名、测试目标和最近应用日志；
已隐藏常见密钥及用户目录，但仍可能包含域名等网络信息，发送前可以检查。

自动检查：
- 网卡、地址、DNS、路由、MTU、接口优先级、系统代理、相关进程/服务/监听端口。
- 系统 DNS、最多 4 个网卡 IPv4 DNS、公共 DNS 的百度 A 记录。
- 百度和腾讯 HTTPS、百度固定真实 IP 对照、公网 TCP 443、两目标 ICMP。
- 最多 4 张启用的物理网卡分别绑定源 IPv4 的 HTTPS 对照。
- HypoMux 网络设置、最近运行配置、测试结束后的应用日志尾部。
- 中文 SUMMARY.txt 和全部原始探测结果。

说明：
源地址绑定不等同于只启用单网卡，不保证绕过 TUN/WFP。
指定 DNS 服务器仍可能被 TUN 劫持；固定 IP 仍可能受核心嗅探及分流影响。
固定百度 IP 183.2.172.177 是此次故障的已知对照，不保证永久有效。
ICMP 超时不代表网页丢包；TCP 成功不保证 TLS 成功。
工具只记录当前现场，不凭单一测试确定根因。

自定义数据目录（可选，排查人员使用）：
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Collect-HypoMuxNetworkDiagnostics.ps1 -DataDirectory "D:\HypoMuxData"

自定义输出目录（桌面不可写时）：
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Collect-HypoMuxNetworkDiagnostics.ps1 -OutputDirectory "$env:TEMP"
