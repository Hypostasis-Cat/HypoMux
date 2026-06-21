# HypoMux

<p align="center">
  <img src="assets/icon.ico" alt="HypoMux Icon" width="128" height="128"><br><br>
  <a href="README.md">简体中文</a> | <a href="README_EN.md">English</a>
</p>

---

# 🇨🇳 简体中文

<p align="center">
  <img src="https://img.shields.io/badge/Python-3.10%2B-blue?style=flat-square&logo=python" alt="Python">
  <img src="https://img.shields.io/badge/Framework-PySide6-green?style=flat-square&logo=qt" alt="PySide6">
  <img src="https://img.shields.io/badge/UI--Library-QFluentWidgets-orange?style=flat-square" alt="QFluentWidgets">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-brightgreen?style=flat-square&logo=windows" alt="Windows">
  <img src="https://img.shields.io/badge/Architecture-Dual--Protocol%20L3%20Binding-red?style=flat-square" alt="Architecture">
</p>

HypoMux 是一款专为 Windows 平台打造的**多网卡带宽并发聚合下载加速工具**，用于在多连接下载场景中实现更稳定的带宽叠加体验。

项目不依赖修改全局路由表（Metric 跃点），而是通过 L3 物理层套接字绑定（IP_UNICAST_IF）与双协议代理引擎进行连接级调度；无需复杂手动配置，就能将电脑连接的宽带网线、Wi-Fi 网卡、手机热点等多条独立网络用于同一批高并发下载任务。对于 Steam 游戏更新、IDM 大文件下载等多连接场景，HypoMux 可以把不同连接分配到不同网卡上，获得更稳定的多线路吞吐表现。

简单来说，只要你的电脑同时连上了多个网络（比如：**插着学校/家里网线的同时，又连上了 Wi-Fi，或者插上了手机的 USB 网络共享**），HypoMux 就能在多线程下载时把连接分散到这些线路上。它适合 Steam、IDM、浏览器大文件下载等多连接任务；对于单连接下载，效果会受任务本身限制。

---

## 📷 界面预览

> 📌 **视觉说明**：Windows 11 风格的轻量化主视窗，移除了高干扰的半透明叠层，采用微光蓝（`#0078d4`）与浅灰监控控制台风格。

<p align="center">
  <img src="assets/ui_idle.png" alt="HypoMux 现代化主界面" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

## ✨ 核心功能

* ⚡ **双协议无感接管**：后台同时运行 SOCKS5 与 HTTP 转发服务，启动后自动接管 Windows WinINet 系统代理，兼容 Steam、IDM、浏览器等常见客户端。
* 🔒 **全生命周期代理保护**：主动停止、启动失败、窗口关闭等路径都会强制还原系统代理，降低代理残留导致断网的风险。
* 📊 **五列实时遥测大屏**：实时展示【选择 | 网卡别名 | IPv4 地址 | 实时速度 (MB/s) | 实时连接数】，方便观察每张网卡的吞吐与连接分配情况。
* 🛠️ **异步网络内核**：网卡扫描、异步 DNS 解析、连接调度与流量监测均放在后台线程与 asyncio 事件循环中处理，避免高并发连接拖慢主界面。

---

## 📦 软件使用方法

1. **环境就绪**：确保您的电脑同时接上了多条独立的网络线路。例如：**网卡1连接校园网/家用有线宽带 + 网卡2连接手机无线热点（5G）**。
2. **勾选网卡**：双击运行本工具，等待后台自动扫描完成。在网卡表格中，**勾选你想参与带宽聚合的所有活动网卡**。
3. **一键加速**：点击右下角 **【一键加速】** 按钮。状态提示切换为运行中，系统全局代理正式接管。
4. **开始下载**：此时直接打开 Steam 触发更新，或者在 IDM 里新建大文件下载任务，HypoMux 调度控制台会显示连接分配日志，多网卡大屏会实时展示各线路吞吐。
5. **干净停止**：下载完成后，随时点击 **【停止加速】** 或直接关闭软件，系统代理会自动还原。

---

## 🎯 支持加速的软件与应用场景

只要目标应用遵循 Windows 系统代理规范，且其下载机制为**"多线程/多并发"**，即可接入 HypoMux 的连接分流：

* 📥 **专业下载管理器**：**IDM (Internet Download Manager)**（默认开启从 IE 获取代理）、迅雷、百度网盘客户端等。
* 🎮 **主流游戏客户端**：**Steam**（其下载引擎 SteamService 原生读取系统标准代理）、Epic Games Launcher、EA App、Xbox 客户端。
* 🌐 **全系列现代浏览器**：Chrome、Edge、Firefox、Safari for Windows 等大文件直接下载。

---

## 📖 技术工作原理

HypoMux 核心分流机制建立在**四层应用层调度**与**三层物理层精准绑定**之上，不改动全局系统路由表。

```text
[多线程应用流量 (Steam / IDM)] 
               │
               ▼ WinINet 自动拦截劫持
    Windows 系统全局注册表代理锁
   (http/https -> 10801 | socks -> 10800)
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

1. **全协议注入**：一键加速时，程序自动向 `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings` 写入全覆盖链条：`http=127.0.0.1:10801;https=127.0.0.1:10801;socks=127.0.0.1:10800`。
2. **底层双绑**：当分流引擎收到下载客户端的 TCP 连接时，调度器通过 `socket.bind()` 钉死本地网卡 IPv4，并向系统内核发送 `setsockopt(socket.IPPROTO_IP, 31, ...)` 强制锁定网卡物理索引（Interface Index），强制流量剥离默认网关，实现物理多通道并进。

---

## 📈 实战并发加速效果

在多网卡高并发下载测试中，HypoMux 可以将连接分配到【以太网2】、【以太网】与【WLAN】等多路通道，各线路同时承担下载流量。

### 实战案例 A：IDM 极限拉取多线程大文件 (Ubuntu ISO 镜像下载)
> 后台引擎并发接管 190 个有效活跃连接，三路通道各自平摊约 **35~39 MB/s** 的下行吞吐，主大屏合并显示突破 **110.93 MB/s**。

<p align="center">
  <img src="assets/screenshot_idm.png" alt="IDM 实战满速分流" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### 实战案例 B：Steam 千兆级大体量游戏更新 (*Hogwarts Legacy*)
> 承接 Steam 下载引擎的高频多线程并发，双线/三线同时工作，持续稳定维持在 **98.26 MB/s** 以上。

<p align="center">
  <img src="assets/screenshot_steam.png" alt="Steam 游戏大作满速更新" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>


### Windows 任务管理器网卡性能面板遥测
> 任务管理器物理监控实况：三张网卡（以太网、以太网2、WLAN）在同一秒内各自出现约 **~300 Mbps** 的接收速率。

<p align="center">
  <img src="assets/screenshot_taskmgr.png" alt="任务管理器多网卡并发接收物理铁证" width="400" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

## 🛠️ 打包编译 (Nuitka)

本项目使用 `Nuitka` 将 Python 代码直接转译为 **C 语言机器码二进制文件**。

```powershell
# 1. 激活并安装打包依赖
venv\Scripts\activate
pip install nuitka zstandard PySide6-Fluent-Widgets

# 2. 一键执行全程序深度链接优化编译
nuitka --standalone --onefile --enable-plugin=pyside6 --windows-console-mode=disable --windows-uac-admin --windows-icon-from-ico=assets/icon.ico --include-package-data=qfluentwidgets --include-data-dir=assets=assets --python-flag=-O --lto=yes main.py
```

---

## 🛡️ 安全提示与技术边界说明

1. **反作弊风险说明**：本工具工作在标准应用层代理和网络套接字绑定层。**不触碰游戏内存、不拦截或修改任何游戏私有网络封包、不注入任何 DLL 驱动**。
2. **单线程连接限制**：多网卡并发聚合本质上是**多连接负载均衡**。如果您的下载任务是极为罕见的单线程 TCP 连接（例如某网盘的非会员单线程死速限制），任何多网卡聚合工具均无法对其加速。
3. **电竞低延迟恢复提示**：多网卡分流模式面向下载吞吐量。在游玩对延迟（Ping 值）要求较高的即时电竞网游（如 *CS2*、*Valorant*、*GTA 5* 联机）前，**请务必点击【停止加速】退出软件**，让电脑网络回归正常的单一默认网关。

---

## 🤝 特别鸣谢 / Acknowledgments

特别感谢以下开发者对本项目早期核心稳定性作出的杰出贡献：

@Requiem_ovo

如果你也对多网卡分流、底层网络调度感兴趣，欢迎提交 Pull Request，一起完善 HypoMux！

---

## 💖 支持与赞赏 (Support)

HypoMux 是一个完全出于技术热情、由作者在业余时间独立开发与维护的开源项目，作者目前仍是在校学生，项目的深度开发与日常维护（如高频使用 AI 工具辅助重构、API 测试等）存在一定的实际开销。如果你觉得这个工具切实解决了你的网络痛点，欢迎请作者喝杯咖啡，支持本项目的持续迭代！

> 💡 **温馨提示：** 量力而行。赞赏纯属自愿，无论是否赞赏，你都可以永久免费使用 HypoMux 的核心功能！
>
> 赞助请留下您的昵称！

<div align="center">
  <img src="./assets/Support/wechat_pay.png" alt="微信赞赏码" width="300" />
  <br />
  <sub>微信号赞赏（请备注：HypoMux 支持）</sub>
</div>


### ⚖️ 开发者声明
* **关于功能走向**：本项目有着清晰的技术主线和架构边界。所有的赞赏均属于无偿赠予，**赞赏行为不等同于商业定制，亦无法直接决定或影响未来新功能的开发走向**。
* **关于免责**：本项目依据 **AGPL-3.0** 协议开源，软件按"原样"提供，作者不承担因使用本工具导致的任何直接或间接损失。

### 🏆 鸣谢与支持名单 (Sponsors)

感谢以下所有为 HypoMux 注入能量的支持者：

<a href="https://github.com/Hypostasis-Cat/HypoMux"><img src="https://img.shields.io/badge/鲸鱼-☕请喝咖啡-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>

再次感谢你对开源社区和独立开发者的尊重与支持！

## 📄 开源协议

本项目基于 **AGPL-3.0** 开源协议。
