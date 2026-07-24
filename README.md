# HypoMux

<p align="center">
  <img src="support/icon.ico" alt="HypoMux Icon" width="128" height="128"><br><br>
  <a href="README.md">简体中文</a> | <a href="README_EN.md">English</a>
</p>

---

#  简体中文

<p align="center">
  <img src="https://img.shields.io/badge/Python-3.10%2B-blue?style=flat-square&logo=python" alt="Python">
  <img src="https://img.shields.io/badge/Framework-PySide6-green?style=flat-square&logo=qt" alt="PySide6">
  <img src="https://img.shields.io/badge/UI--Library-QFluentWidgets-orange?style=flat-square" alt="QFluentWidgets">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-brightgreen?style=flat-square&logo=windows" alt="Windows">
  <img src="https://img.shields.io/badge/Architecture-Dual--Protocol%20L3%20Binding-red?style=flat-square" alt="Architecture">
</p>

HypoMux v2.0 是一款专为 Windows 平台打造的**多网卡带宽并发聚合下载加速工具**，用于在多连接下载场景中实现更稳定的带宽叠加体验。

2.0 版本在原有系统代理模式之外加入了**虚拟网卡模式**，通过更完整的流量接管与本地安全分流，让多网络加速在更多应用里保持稳定；同时新增**分流规则**，可以把指定进程放入直连/绕过列表，或继续交给多网卡聚合通道处理，解锁下载、游戏平台、浏览器等更多组合玩法。

在系统代理模式下，HypoMux 通过 L3 物理层套接字绑定（IP_UNICAST_IF）与双协议代理引擎进行连接级调度；在虚拟网卡模式下，HypoMux 会临时调整 Windows 代理与路由相关设置，将需要加速的流量导入本地核心处理，并让非加速流量按高级分流规则直接放行。对于 Steam 游戏更新、IDM 大文件下载、WeGame 下载等多连接场景，HypoMux 可以把不同连接分配到不同网卡上，获得更稳定的多线路吞吐表现。

简单来说，只要你的电脑同时连上了多个网络（比如：**插着学校/家里网线的同时，又连上了 Wi-Fi，或者插上了手机的 USB 网络共享**），HypoMux 就能在多线程下载时把连接分散到这些线路上。它适合 Steam、IDM、浏览器大文件下载等多连接任务；对于单连接下载，效果会受任务本身限制。

---

## Sponsors

<p align="center">
  <a href="https://signpath.io/"><img src="support/SignPath/SignPath.png" alt="SignPath" height="38" /></a>&nbsp;&nbsp;
  Free code signing provided by <a href="https://signpath.io/">SignPath.io</a>, certificate by <a href="https://signpath.org/">SignPath Foundation</a>.
</p>

### Code Signing Policy / Windows 代码签名政策

HypoMux 衷心感谢 SignPath 与 SignPath Foundation 对开源软件的支持，帮助我们为 Windows 用户提供更安全、可信的下载体验。

HypoMux 的官方 Windows 发布版本均由此仓库的 GitHub Actions 构建，并提交至 SignPath 进行代码签名。请仅从[官方 GitHub Releases 页面](https://github.com/Hypostasis-Cat/HypoMux/releases/latest)下载安装包，并确认已签名版本的发布者显示为 **SignPath Foundation**。

### 团队角色

* **提交者与审阅者：**[Hypostasis-Cat](https://github.com/Hypostasis-Cat)，项目维护者。非提交者发起的 Pull Request 必须经项目维护者审阅后方可合并。
* **签名审批者：**[Hypostasis-Cat](https://github.com/Hypostasis-Cat)。每次生产环境签名请求都会在 SignPath UI 中人工批准，再取得已签名构件。

### 隐私政策

HypoMux 不收集、出售或上传个人数据及遥测信息。程序仅会在用户或软件操作者请求相应功能时与其他网络系统通信：转发用户选择的网络流量、从官方 GitHub 仓库检查或下载安装更新，以及在启用虚拟网卡模式后进行网络连通性验证。

---

## ⬇️ 下载

> **Windows 安装包：**[点击前往 Releases 下载最新版本](https://github.com/Hypostasis-Cat/HypoMux/releases/latest)
>
> 打开后，在最新版本页面的 **Assets** 区域下载 `HypoMux_Setup_*.exe` 即可安装。

---

##  界面预览

>  **视觉说明**：Windows 11 风格的轻量化主视窗，移除了高干扰的半透明叠层，采用微光蓝（`#0078d4`）与浅灰监控控制台风格。

<p align="center">
  <img src="assets/ui_idle_2.0.png" alt="HypoMux 2.0 现代化主界面" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

##  核心功能

*  **虚拟网卡模式**：v2.0 新增 TUN 虚拟网卡接管能力，可在更多应用场景下获得更稳定的加速效果。
*  **进程级分流规则**：支持将指定应用配置为直连/绕过，或交给多网卡聚合通道处理，适合下载、游戏平台、浏览器与低延迟应用混合使用。
*  **双协议无感接管**：后台同时运行 SOCKS5 与 HTTP 转发服务，启动后自动接管 Windows WinINet 系统代理，兼容 Steam、IDM、浏览器等常见客户端。
*  **全生命周期代理保护**：主动停止、启动失败、窗口关闭等路径都会强制还原系统代理，降低代理残留导致断网的风险。
*  **五列实时遥测大屏**：实时展示【选择 | 网卡别名 | IPv4 地址 | 实时速度 (MB/s) | 实时连接数】，方便观察每张网卡的吞吐与连接分配情况。
* ️ **异步网络内核**：网卡扫描、异步 DNS 解析、连接调度与流量监测均放在后台线程与 asyncio 事件循环中处理，避免高并发连接拖慢主界面。

---

## 📢 重要提示与合规免责声明

HypoMux 是一个透明、开源的网络工具，仅用于用户本人拥有授权的设备与网络连接。它不应用于绕过第三方访问控制、网络限制、平台规则或任何未经授权的安全措施。

使用前请确认你理解以下行为边界：

1. **系统设置调整**：HypoMux 运行时可能会动态调整 Windows 系统代理和/或路由相关设置，以便将流量导入加速核心。
2. **本地安全代理**：启用加速后，需要加速的网络流量会经过本机安全核心进行分流、代理与多路复用。
3. **自动恢复机制**：停止工具或卸载软件时，HypoMux 会自动恢复被修改的系统代理与网络设置。
4. **游戏与分流规则**：HypoMux v2.0 引入高级分流能力。对于竞技类网游等对延迟极度敏感的应用，建议将其加入**直连/绕过规则列表**，以保持原始网络延迟；也可以在游戏时暂停本工具。

---

##  软件使用方法

1. **环境就绪**：确保您的电脑同时接上了多条独立的网络线路。例如：**网卡1连接校园网/家用有线宽带 + 网卡2连接手机无线热点（5G）**。
2. **选择模式**：根据场景选择系统代理模式或虚拟网卡模式。虚拟网卡模式适合希望更稳定接管流量的场景。
3. **勾选网卡**：双击运行本工具，等待后台自动扫描完成。在网卡表格中，**勾选你想参与带宽聚合的所有活动网卡**。
4. **配置分流**：如需保证游戏、语音、会议等低延迟应用体验，可在路由规则页把对应进程加入直连/绕过规则。
5. **一键加速**：点击 **【一键加速】** 按钮。状态提示切换为运行中后，即可开始下载或更新。
6. **干净停止**：下载完成后，随时点击 **【停止加速】** 或直接关闭软件，系统网络设置会自动还原。

---

##  支持加速的软件与应用场景

只要目标应用遵循 Windows 系统代理规范，且其下载机制为**"多线程/多并发"**，即可接入 HypoMux 的连接分流：

*  **专业下载管理器**：**IDM (Internet Download Manager)**（默认开启从 IE 获取代理）、迅雷、百度网盘客户端等。
*  **主流游戏客户端**：**Steam**（其下载引擎 SteamService 原生读取系统标准代理）、Epic Games Launcher、EA App、Xbox 客户端。
*  **全系列现代浏览器**：Chrome、Edge、Firefox、Safari for Windows 等大文件直接下载。

---

##  技术工作原理

HypoMux 核心分流机制建立在**四层应用层调度**、**三层物理层精准绑定**与 v2.0 的**虚拟网卡分流接管**之上。系统代理模式负责轻量接入支持 WinINet/系统代理的应用；虚拟网卡模式则通过本地内核侧车与高级分流规则覆盖更多流量场景。

```text
[多线程应用流量 (Steam / IDM)] 
               │
               ▼ WinINet 自动拦截劫持
    Windows 系统代理 / 虚拟网卡模式接入
   (http/https -> 10801 | socks -> 10800 | TUN)
               │
               ▼ 
  ProxyWorker 核心引擎 (Asyncio inside QThread)
               │
               ▼ Round-Robin 连接轮询分发机制
   L3 物理层双向套接字强行绑定
   ├── socket.bind((nic1_ip, 0)) + IP_UNICAST_IF ──► 真实物理网卡 1 ──┐
   ├── socket.bind((nic2_ip, 0)) + IP_UNICAST_IF ──► 真实物理网卡 2 ─┼─► 物理带宽叠加吞吐
   └── socket.bind((nic3_ip, 0)) + IP_UNICAST_IF ──► 真实物理网卡 3 ──┘
```

1. **双模式接入**：系统代理模式会向 `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings` 写入本地代理链条：`http=127.0.0.1:10801;https=127.0.0.1:10801;socks=127.0.0.1:10800`；虚拟网卡模式则通过 TUN 接管与高级分流规则处理更多应用流量。
2. **底层双绑**：当分流引擎收到下载客户端的 TCP 连接时，调度器通过 `socket.bind()` 钉死本地网卡 IPv4，并向系统内核发送 `setsockopt(socket.IPPROTO_IP, 31, ...)` 强制锁定网卡物理索引（Interface Index），强制流量剥离默认网关，实现物理多通道并进。
3. **高级分流**：v2.0 支持按进程配置聚合或直连策略。下载任务可以进入多网卡聚合通道，对延迟敏感的程序则可保持直连。

---

##  实战并发加速效果

在多网卡高并发下载测试中，HypoMux 可以将连接分配到【以太网2】、【以太网】与【WLAN】等多路通道，各线路同时承担下载流量。

### 实战案例 A：IDM 极限拉取多线程大文件 (Ubuntu ISO 镜像下载)
> 后台引擎并发接管 190 个有效活跃连接，三路通道各自平摊约 **35~39 MB/s** 的下行吞吐，主大屏合并显示突破 **110.93 MB/s**。

<p align="center">
  <img src="assets/screenshot_2.0_idm.png" alt="IDM 实战满速分流" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### 实战案例 B：Steam 千兆级大体量游戏更新 (*Hogwarts Legacy*)
> 承接 Steam 下载引擎的高频多线程并发，双线/三线同时工作，持续稳定维持在 **98.26 MB/s** 以上。

<p align="center">
  <img src="assets/screenshot_steam.png" alt="Steam 游戏大作满速更新" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### 实战案例 C：WeGame 游戏下载速度展示
> v2.0 虚拟网卡模式与分流规则可覆盖更多游戏平台下载场景，让 WeGame 等客户端也能在多网络环境下获得更高吞吐。

<p align="center">
  <img src="assets/screenshot_2.0_wegame.png" alt="WeGame 下载速度展示" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Windows 任务管理器网卡性能面板遥测
> 任务管理器物理监控实况：三张网卡（以太网、以太网2、WLAN）在同一秒内各自出现约 **~300 Mbps** 的接收速率。

<p align="center">
  <img src="assets/screenshot_taskmgr.png" alt="任务管理器多网卡并发接收物理铁证" width="400" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

## ️ 打包编译 (Nuitka)

本项目使用 `Nuitka` 将 Python 代码直接转译为 **C 语言机器码二进制文件**。

```powershell
# 1. 激活并安装打包依赖
venv\Scripts\activate
pip install nuitka zstandard PySide6-Fluent-Widgets

# 2. 一键执行全程序深度链接优化编译
nuitka --standalone --onefile --enable-plugin=pyside6 --windows-console-mode=disable --windows-uac-admin --windows-icon-from-ico=support/icon.ico --include-package-data=qfluentwidgets --include-data-dir=support=support --python-flag=-O --lto=yes main.py
```

---

## ️ 安全提示与技术边界说明

1. **反作弊风险说明**：本工具工作在标准应用层代理和网络套接字绑定层。**不触碰游戏内存、不拦截或修改任何游戏私有网络封包、不注入任何 DLL 驱动**。
2. **单线程连接限制**：多网卡并发聚合本质上是**多连接负载均衡**。如果您的下载任务是极为罕见的单线程 TCP 连接（例如某网盘的非会员单线程死速限制），任何多网卡聚合工具均无法对其加速。
3. **电竞低延迟恢复提示**：多网卡分流模式面向下载吞吐量。在游玩对延迟（Ping 值）要求较高的即时电竞网游（如 *CS2*、*Valorant*、*GTA 5* 联机）前，请将相关进程加入**直连/绕过规则列表**，或点击【停止加速】让电脑网络回归正常链路。

---

##  特别鸣谢 / Acknowledgments

特别感谢所有对本项目早期核心稳定性、工程规范化作出贡献的开发者们：

<a href="https://github.com/Hypostasis-Cat/HypoMux/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=Hypostasis-Cat/HypoMux" />
</a>

如果你也对多网卡分流、底层 network 调度感兴趣，欢迎提交 Pull Request，一起完善 HypoMux！

---

##  支持与赞赏 (Support)

HypoMux 是一个完全出于技术热情、由作者在业余时间独立开发与维护的开源项目，作者目前仍是在校学生，项目的深度开发与日常维护（如高频使用 AI 工具辅助重构、API 测试等）存在一定的实际开销。如果你觉得这个工具切实解决了你的网络痛点，欢迎请作者喝杯咖啡，支持本项目的持续迭代！

>  **温馨提示：** 量力而行。赞赏纯属自愿，无论是否赞赏，你都可以永久免费使用 HypoMux 的核心功能！
>
> 赞助请留下您的昵称！

<div align="center">
  <table>
    <tr>
      <td align="center" width="320">
        <img src="support/wei.png" alt="微信赞赏码" width="260" />
        <br />
        <sub>微信赞赏（请备注您的昵称，未备注则表示匿名）</sub>
      </td>
      <td align="center" width="320">
        <img src="support/zhi.jpg" alt="支付宝赞赏码" width="260" />
        <br />
        <sub>支付宝赞赏（请备注您的昵称，未备注则表示匿名）</sub>
      </td>
    </tr>
  </table>
</div>


### ️ 开发者声明
* **关于功能走向**：本项目有着清晰的技术主线和架构边界。所有的赞赏均属于无偿赠予，**赞赏行为不等同于商业定制，亦无法直接决定或影响未来新功能的开发走向**。
* **关于免责**：本项目依据 **AGPL-3.0** 协议开源，软件按"原样"提供，作者不承担因使用本工具导致的任何直接或间接损失。

### 个人支持者名单

感谢以下所有为 HypoMux 注入能量的支持者：

<p>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="鲸鱼，咖啡支持" src="https://img.shields.io/badge/%E9%B2%B8%E9%B1%BC-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，特别鸣谢" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，特别鸣谢" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="廾阁，咖啡支持" src="https://img.shields.io/badge/%E5%BB%BE%E9%98%81-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="六花 DY，特别鸣谢" src="https://img.shields.io/static/v1?label=%E5%85%AD%E8%8A%B1%20DY&message=%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5&color=DCD0FF&labelColor=E6E6FA&style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="SK，咖啡支持" src="https://img.shields.io/badge/SK-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="WZLN，咖啡支持" src="https://img.shields.io/static/v1?label=WZLN&message=%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="幸運上上簽，咖啡支持" src="https://img.shields.io/static/v1?label=%E5%B9%B8%E9%81%8B%E4%B8%8A%E4%B8%8A%E7%B1%A4&message=%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，特别鸣谢" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，特别鸣谢" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="匿名，咖啡支持" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="HEDE WANG，咖啡支持" src="https://img.shields.io/static/v1?label=HEDE%20WANG&message=%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
</p>

##  Star 历史趋势 / Star History

随着新功能不断解锁，欢迎见证 HypoMux 的成长！

<a href="https://www.star-history.com/?repos=Hypostasis-Cat%2FHypoMux&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=Hypostasis-Cat/HypoMux&type=date&theme=dark&legend=top-left&sealed_token=TqDZoh8oANl5EOyQEV_OiCtR6pkXcRlH3EJEWuYR_VZq2Kuenl7rWWNzwKjvQjVFjahCDAL-e5Qcr0PqhEKIXMv1kyYwrH0xIWcn6A724P7r5jNz3Hqqg7XNtaPU3Rf2h4olUGuG0rLh54fUCS2zB_m5GSgH5uEP7jnJJQi7yxaGmGgC6JlI4M-hQiIC" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=Hypostasis-Cat/HypoMux&type=date&legend=top-left&sealed_token=TqDZoh8oANl5EOyQEV_OiCtR6pkXcRlH3EJEWuYR_VZq2Kuenl7rWWNzwKjvQjVFjahCDAL-e5Qcr0PqhEKIXMv1kyYwrH0xIWcn6A724P7r5jNz3Hqqg7XNtaPU3Rf2h4olUGuG0rLh54fUCS2zB_m5GSgH5uEP7jnJJQi7yxaGmGgC6JlI4M-hQiIC" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=Hypostasis-Cat/HypoMux&type=date&legend=top-left&sealed_token=TqDZoh8oANl5EOyQEV_OiCtR6pkXcRlH3EJEWuYR_VZq2Kuenl7rWWNzwKjvQjVFjahCDAL-e5Qcr0PqhEKIXMv1kyYwrH0xIWcn6A724P7r5jNz3Hqqg7XNtaPU3Rf2h4olUGuG0rLh54fUCS2zB_m5GSgH5uEP7jnJJQi7yxaGmGgC6JlI4M-hQiIC" />
 </picture>
</a>

##  开源协议

本项目基于 **AGPL-3.0** 开源协议。
