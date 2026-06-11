# NetBooster 🚀

<p align="center">
  <img src="assets/icon.ico" alt="NetBooster Icon" width="128" height="128"><br><br>
  <a href="#-netbooster---简体中文">简体中文</a> | <a href="#-netbooster---english">English</a>
</p>

---

# 🇨🇳 NetBooster - 简体中文

<p align="center">
  <img src="https://img.shields.io/badge/Python-3.10%2B-blue?style=flat-square&logo=python" alt="Python">
  <img src="https://img.shields.io/badge/Framework-PySide6-green?style=flat-square&logo=qt" alt="PySide6">
  <img src="https://img.shields.io/badge/UI--Library-QFluentWidgets-orange?style=flat-square" alt="QFluentWidgets">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-brightgreen?style=flat-square&logo=windows" alt="Windows">
  <img src="https://img.shields.io/badge/Release-v1.0.1-blue?style=flat-square" alt="Version">
  <img src="https://img.shields.io/badge/License-AGPL--3.0-red?style=flat-square" alt="License">
</p>

NetBooster 是一款基于 **PySide6** 与 **QFluentWidgets** 开发的现代化 Windows 多网卡并发下载加速工具。

通过动态调度系统的网络接口跃点数（Interface Metric），本工具能够引导多线程下载软件（如 IDM、迅雷、Steam、BT 等）同时利用多条 network 线路（如：以太网 + Wi-Fi + 移动热点），实现带宽叠加与无感加速。

---

## 📷 界面预览

> 📌 **提示**：请将您的软件运行截图重命名为 `screenshot.png` 并放入项目根目录的 `assets/` 文件夹中。

<p align="center">
  <img src="assets/screenshot.png" alt="NetBooster UI Preview" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

## ✨ 核心功能

* 🎨 **Fluent UI 交互**：全面适配 Windows 11 设计语言，支持亚克力效果、原生圆角与平滑过渡动画。
* 🔍 **异步网卡扫描**：自动过滤并展示当前系统中已连接且分配有 IPv4 地址的活动网卡，杜绝失效接口干扰。
* 🚀 **一键并发加速**：动态锁定选中网卡的跃点数（预设为 10）并关闭自动跃点，强制多线程流量进行多路负载均衡。
* 🎮 **一键恢复默认**：快速将所有网卡切回 Windows “自动跃点”状态，彻底清理路由干扰，确保电竞游戏低延迟。

---

## 🛠️ 技术亮点

* **纯数字标识绑定**：全链路采用 `InterfaceIndex`（接口索引）作为唯一凭证操控系统底层，完美规避中文字符集引发的 PowerShell 编码乱码与崩溃问题。
* **多线程异步架构**：将所有 PowerShell 路由表读写、底层网络扫描操作移出主线程（QThread），配合 Qt 信号槽机制驱动，当前端网络阻塞时界面依然保持丝滑响应。
* **生命周期与权限管理**：程序启动时安全触发 Windows UAC 弹窗提权，并精准锁定当前工作目录，防止进程重定向至 `System32` 导致相对路径失效。

---

## 📖 工作原理

Windows 系统默认优先选择跃点数（Metric）较低的网卡传输数据。当多张活动网卡的跃点数被设为完全一致的低数值时，系统底层会激活多路负载均衡。

```
[多线程下载流量] ───►  NetBooster 调度 
                       ├──► 网卡 A (Metric = 10) ──► 线路 1 ──┐
                       ├──► 网卡 B (Metric = 10) ──► 线路 2 ─┼─► 带宽叠加并发下载
                       └──► 网卡 C (Metric = 10) ──► 线路 3 ──┘
```

> ⚠️ **注意**：网卡并发对**单线程 TCP 连接**无效。本工具主要针对 **多线程/多连接** 场景（如 P2P 下载、Steam 游戏更新、分块下载器）。

---

## 📦 快速开始

### 方式 A：源码运行（面向开发者）

```bash
# 1. 克隆仓库并创建虚拟环境
git clone [https://github.com/Hypostasis-Cat/NetBooster.git](https://github.com/Hypostasis-Cat/NetBooster.git)
cd NetBooster
python -m venv venv

# 2. 激活虚拟环境 (Windows CMD)
venv\Scripts\activate

# 3. 安装依赖并运行
pip install -r requirements.txt
python main.py
```

### 方式 B：生产级单文件打包（使用 Nuitka 机器码编译）

推荐使用 `Nuitka` 将项目编译为原生二进制 `.exe` 单文件，以获得最佳的启动性能和最小的体积：

```bash
pip install nuitka zstandard PySide6-Fluent-Widgets
nuitka --standalone --onefile --enable-plugin=pyside6 --windows-console-mode=disable --windows-uac-admin --windows-icon-from-ico=assets/icon.ico --include-package-data=qfluentwidgets --include-data-dir=assets=assets --python-flag=-O --lto=yes main.py
```

---

## 🛡️ 免责声明与注意事项

1. **家庭宽带带宽峰值提醒**：若您的家庭宽带本身已达到物理带宽上限（如千兆光纤满速），单线路叠加可能无法带来明显的速率飞跃。**强烈建议将 PC 同时连接手机移动数据热点（或第二条独立宽带线路）进行多路并发测试**，以验证多网卡叠加加速效果。
2. **游戏安全提示**：本工具仅通过 Windows 官方 PowerShell API 修改系统标准路由表，**不涉及任何内存注入、游戏封包拦截或篡改行为**，100% 安全，绝不触发反作弊封号（如 VAC、BattlEye、DMA 检测等）。
3. **延迟敏感型应用**：并发加速模式会导致多网卡分流，可能引起部分电竞游戏路由抖动。**强烈建议在游玩竞技类游戏前，点击界面右下角的 🎮 [恢复默认] 按钮。**
4. **技术边界与极客指南**：本项目仅供网络分流技术的学习与研究使用，定位为轻量级、零成本的临时下载加速方案。**硬核专业玩家若对全时段策略流聚合有更严苛的需求，可参考 Speedify 专业聚合软件或部署双 WAN 软路由（OpenWrt / iKuai）的多线负载均衡方案。**

---

## 📄 开源协议

本项目基于 **AGPL-3.0** 开源协议。

---
---

# 🇺🇸 NetBooster - English

<p align="center">
  <img src="https://img.shields.io/badge/Python-3.10%2B-blue?style=flat-square&logo=python" alt="Python">
  <img src="https://img.shields.io/badge/Framework-PySide6-green?style=flat-square&logo=qt" alt="PySide6">
  <img src="https://img.shields.io/badge/UI--Library-QFluentWidgets-orange?style=flat-square" alt="QFluentWidgets">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-brightgreen?style=flat-square&logo=windows" alt="Windows">
  <img src="https://img.shields.io/badge/Release-v1.0.1-blue?style=flat-square" alt="Version">
  <img src="https://img.shields.io/badge/License-AGPL--3.0-red?style=flat-square" alt="License">
</p>

NetBooster is a modern Windows multi-network adapter concurrent download acceleration tool developed based on **PySide6** and **QFluentWidgets**.

By dynamically scheduling the network interface metric (Interface Metric) of the system, this tool guides multi-threaded download software (such as IDM, Thunder, Steam, BT, etc.) to utilize multiple network lines simultaneously (e.g., Ethernet + Wi-Fi + Mobile Hotspot) to achieve bandwidth stacking and seamless acceleration.

---

## 📷 UI Preview

> 📌 **PRO TIP**：Please rename your software running screenshot to `screenshot.png` and place it in the `assets/` folder of the project root directory.

<p align="center">
  <img src="assets/screenshot.png" alt="NetBooster UI Preview" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

## ✨ Key Features

* 🎨 **Fluent UI Interaction**: Fully adapted to the Windows 11 design language, supporting Acrylic effects, native rounded corners, and smooth transition animations.
* 🔍 **Asynchronous Adapter Scanning**: Automatically filters and displays active network adapters that are connected and assigned with IPv4 addresses, eliminating interference from invalid interfaces.
* 🚀 **One-Click Concurrent Acceleration**: Locks the metric of selected adapters (preset to 10) and disables automatic metric, forcing multi-threaded traffic to undergo multi-path load balancing.
* 🎮 **One-Click Restore**: Quickly switches all adapters back to the Windows "Automatic Metric" state, thoroughly clearing routing interference to ensure low latency for gaming.

---

## 🛠️ Technical Highlights

* **Pure Numeric Identifier Binding**: The entire link uses `InterfaceIndex` as the unique credential fed to the system underlying layer, perfectly avoiding PowerShell encoding issues and crashes caused by Chinese character sets.
* **Asynchronous Multi-Threading**: Moves all PowerShell routing table operations and network scanning completely out of the main thread (QThread), cooperating with the Qt Signal/Slot mechanism to maintain a smooth UI response even under network blockage.
* **Privilege & Lifecycle Management**: Safely triggers the Windows UAC pop-up for privilege elevation upon startup, and precisely locks the current working directory to prevent process redirection to `System32`.

---

## 📖 How It Works

By default, Windows prioritizes network adapters with lower metrics for data transmission. When the metrics of multiple active adapters are set to the exact same low value, the underlying system activates multi-path load balancing.

```
[Multi-threaded Traffic] ───►  NetBooster Scheduling
                       ├──► Adapter A (Metric = 10) ──► Line 1 ──┐
                       ├──► Adapter B (Metric = 10) ──► Line 2 ─┼─► Bandwidth Stacking
                       └──► Adapter C (Metric = 10) ──► Line 3 ──┘
```

> ⚠️ **Note**: Adapter concurrency is **ineffective for single-threaded TCP connections**. This tool mainly targets **multi-threaded/multi-connection** scenarios (such as P2P downloads, Steam game updates, chunked downloaders).

---

## 📦 Quick Start

### Method A: Run from Source (For Developers)

```bash
# 1. Clone repository and create virtual environment
git clone [https://github.com/Hypostasis-Cat/NetBooster.git](https://github.com/Hypostasis-Cat/NetBooster.git)
cd NetBooster
python -m venv venv

# 2. Activate virtual environment (Windows CMD)
venv\Scripts\activate

# 3. Install dependencies and run
pip install -r requirements.txt
python main.py
```

### Method B: Production Single-File Compilation (Using Nuitka)

We highly recommend using `Nuitka` to compile the project into a native binary `.exe` single file for the best startup performance and minimal size:

```bash
pip install nuitka zstandard PySide6-Fluent-Widgets
nuitka --standalone --onefile --enable-plugin=pyside6 --windows-console-mode=disable --windows-uac-admin --windows-icon-from-ico=assets/icon.ico --include-package-data=qfluentwidgets --include-data-dir=assets=assets --python-flag=-O --lto=yes main.py
```

---

## 🛡️ Disclaimer & Precautions

1. **Bandwidth Peak Notice**: If your home broadband has already reached its physical bandwidth ceiling (e.g., gigabit fiber at full speed), stacking routes on the same line may not yield a significant speed boost. **It is highly recommended to connect your PC to a mobile data hotspot (or a second independent broadband line) simultaneously for multi-path concurrent testing** to verify the stacking effect.
2. **Gaming Safety & Anti-Cheat**: This tool only modifies the system standard routing table through the official Windows PowerShell API. It **does not involve any memory injection, packet interception, or tampering behavior**. It is 100% safe and will never trigger anti-cheat bans (such as VAC, BattlEye, DMA detection, etc.).
3. **Latency-Sensitive Applications**: Concurrent acceleration mode distributes traffic across multiple adapters, which may cause routing jitter in some esports games. **It is strongly recommended to click the 🎮 [Restore Default] button at the bottom right before playing competitive games.**
4. **Technical Boundary & Power User Guide**: This project is only intended for the study and research of network traffic splitting, positioned as a lightweight, zero-cost temporary acceleration utility. **Power users or network geeks who require industrial-grade, full-time multi-line aggregation strategies may refer to commercial software like Speedify or deploy advanced Dual-WAN soft routers (OpenWrt / iKuai) for multi-line load balancing.**

---

## 📄 License

This project is licensed under the **AGPL-3.0** License.