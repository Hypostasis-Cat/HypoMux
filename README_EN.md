# HypoMux

<p align="center">
  <img src="support/icon.ico" alt="HypoMux Icon" width="128" height="128"><br><br>
  <a href="README.md">简体中文</a> | <a href="README_EN.md">English</a>
</p>

---

#  English

<p align="center">
  <img src="https://img.shields.io/badge/Python-3.10%2B-blue?style=flat-square&logo=python" alt="Python">
  <img src="https://img.shields.io/badge/Framework-PySide6-green?style=flat-square&logo=qt" alt="PySide6">
  <img src="https://img.shields.io/badge/UI--Library-QFluentWidgets-orange?style=flat-square" alt="QFluentWidgets">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-brightgreen?style=flat-square&logo=windows" alt="Windows">
  <img src="https://img.shields.io/badge/Architecture-Dual--Protocol%20L3%20Binding-red?style=flat-square" alt="Architecture">
</p>

HypoMux v2.0 is a multi-network-adapter bandwidth aggregation and download acceleration tool built for Windows. It is designed for multi-connection download workloads where traffic can be distributed across several active network interfaces.

Version 2.0 adds a **Virtual NIC mode** alongside the original system proxy mode, giving HypoMux a more stable way to capture and split traffic across a wider range of applications. It also introduces **split tunneling rules**, so selected processes can be sent through the multi-NIC aggregation path or kept on a direct/bypass route for low-latency use cases.

In system proxy mode, HypoMux uses L3 socket binding (IP_UNICAST_IF) with a dual-protocol local proxy engine. In Virtual NIC mode, HypoMux temporarily adjusts Windows proxy and routing-related settings, sends accelerated traffic into the local core, and lets non-accelerated traffic pass directly through advanced split tunneling rules. This makes it useful for high-concurrency scenarios such as Steam updates, IDM large-file downloads, WeGame downloads, and browser-based transfers.

In simpler terms, as long as your computer is connected to multiple networks at the same time (for example, **being plugged into a school or home Ethernet cable while also connected to Wi-Fi, or using USB tethering from a phone**), HypoMux can distribute multi-threaded download connections across those networks. Single-connection downloads are still limited by the behavior of the download source.

---

## Sponsors

<p align="center">
  <a href="https://signpath.io/"><img src="support/SignPath/SignPath.png" alt="SignPath" height="38" /></a>&nbsp;&nbsp;
  Free code signing provided by <a href="https://signpath.io/">SignPath.io</a>, certificate by <a href="https://signpath.org/">SignPath Foundation</a>.
</p>

### Code Signing Policy

HypoMux is sincerely grateful to SignPath and the SignPath Foundation for supporting open-source software and helping us deliver a safer Windows download experience.

The official Windows releases of HypoMux are built from this repository through GitHub Actions and submitted to SignPath for code signing. Users should download installers only from the [official GitHub Releases page](https://github.com/Hypostasis-Cat/HypoMux/releases/latest) and verify that signed releases show **SignPath Foundation** as the publisher.

### Team roles

* **Committer and reviewer:** [Hypostasis-Cat](https://github.com/Hypostasis-Cat), the project maintainer. Pull requests from non-committers must be reviewed by a project maintainer before merging.
* **Approver:** [Hypostasis-Cat](https://github.com/Hypostasis-Cat). Each production signing request is manually approved in the SignPath UI before the signed artifact is retrieved.

### Privacy policy

HypoMux does not collect, sell, or upload personal data or telemetry. The program contacts other networked systems only to perform functionality requested by the user or the person operating it: forwarding the user's selected network traffic, checking or downloading an update from the official GitHub repository, and validating connectivity after Virtual NIC mode has been enabled.

---

## ⬇️ Download

> **Windows installer:** [Download the latest release](https://github.com/Hypostasis-Cat/HypoMux/releases/latest)
>
> On the latest release page, download `HypoMux_Setup_*.exe` from the **Assets** section.

---

##  UI Preview

<p align="center">
  <img src="assets/ui_idle_2.0.png" alt="HypoMux 2.0 UI Canvas" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

##  Key Technical Features

* **Virtual NIC Mode**: New in v2.0, TUN-based traffic capture improves acceleration stability across more application types.
* **Per-Process Split Tunneling**: Route selected apps directly/bypassed or through the multi-NIC aggregation path, making download clients, game launchers, browsers, and low-latency apps easier to mix.
*  **Seamless Dual-Protocol Interception**: Starts asynchronous SOCKS5 and local HTTP forwarding services, then applies WinINet system proxy settings when acceleration begins.
*  **Fail-Safe Proxy Restore**: Manual stop, startup failure, and window close paths all attempt to restore the system proxy cleanly.
*  **5-Column Telemetry Matrix Grid**: Dynamically displays precise multi-path load distribution: [ Select | Adapter Alias | IPv4 Address | Real-time Speed (MB/s) | Active Connections ].
* ️ **Pure Async Groundwork**: All PowerShell network querying, background thread interface telemetry, and async DNS processing run entirely inside a detached event loop away from the main Qt UI thread.

---

## 📢 Important Notice & Compliance Disclaimer

HypoMux is a transparent, open-source network utility intended only for authorized use on the user's own devices and network connections. It is not designed or permitted for bypassing third-party access controls, network restrictions, platform rules, or any security measures without authorization.

Before using HypoMux, please be aware of the following behavior:

1. **System Changes**: While active, HypoMux may dynamically adjust Windows system proxy and/or routing-related settings so traffic can enter the acceleration core.
2. **Local Proxying**: Accelerated traffic is processed locally through the secure core for splitting, proxying, and multiplexing.
3. **Automatic Restoration**: Modified system proxy and network settings are restored automatically when the tool is stopped or uninstalled.
4. **Gaming & Split Tunneling**: HypoMux v2.0 introduces advanced split tunneling. For latency-sensitive applications such as competitive online games, users are strongly advised to add them to the **Direct/Bypass routing list** to preserve raw network latency, or suspend HypoMux during gameplay.

---

##  How to Use

1. **Hardware Setup**: Hook up your PC to multiple unique lines (e.g., **Broadband Lan Wire + Mobile Phone 5G Tethering Hotspot**).
2. **Choose a Mode**: Use system proxy mode for lightweight proxy-aware acceleration, or Virtual NIC mode when you want broader and more stable traffic capture.
3. **Select Interfaces**: Start HypoMux, wait for the background scan worker to finish, and **check the adapters you wish to use**.
4. **Configure Split Rules**: For games, voice chat, meetings, or other latency-sensitive apps, add their processes to the direct/bypass rules.
5. **Engage Acceleration**: Click **Boost**. Once the status switches to running, start your downloads or game updates.
6. **Graceful Teardown**: Click **Stop** or close HypoMux when done; modified network settings are restored automatically.

---

##  Supported Applications

Any multi-connection/multi-threaded client acknowledging standard Windows WinINet internet proxy server layouts will immediately benefit from concurrent aggregation:

* **Download Software**: **IDM (Internet Download Manager)**, Thunder (迅雷), Baidu NetDisk Client, etc.
* **Gaming Platforms**: **Steam Client Download Core**, Epic Games Launcher, EA App, Xbox Application.
* **Browsers**: Large file downloads via Chrome, Edge, Firefox, etc.

---

##  Technical Architecture

HypoMux's core distribution mechanism combines **Layer-4 application-level scheduling**, **Layer-3 physical socket binding**, and the v2.0 **Virtual NIC split-tunneling capture path**. System proxy mode covers applications that honor WinINet/system proxy settings, while Virtual NIC mode extends coverage through a local network sidecar and advanced routing rules.

```text
[Multi-threaded Application Traffic (Steam / IDM)]
               │
               ▼ WinINet Auto-Interception
    Windows System Proxy / Virtual NIC Ingress
   (http/https -> 10801 | socks -> 10800 | TUN)
               │
               ▼
  ProxyWorker Core Engine (Asyncio inside QThread)
               │
               ▼ Round-Robin Connection Distribution
   L3 Physical Layer Bidirectional Socket Binding
   ├── socket.bind((nic1_ip, 0)) + IP_UNICAST_IF ──► Physical NIC 1 ──┐
   ├── socket.bind((nic2_ip, 0)) + IP_UNICAST_IF ──► Physical NIC 2 ─┼─► Aggregated Throughput
   └── socket.bind((nic3_ip, 0)) + IP_UNICAST_IF ──► Physical NIC 3 ──┘
```

1. **Dual Ingress Modes**: System proxy mode writes the local proxy chain to `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`: `http=127.0.0.1:10801;https=127.0.0.1:10801;socks=127.0.0.1:10800`; Virtual NIC mode uses TUN capture and advanced split rules to cover more application traffic.
2. **Low-Level Dual Binding**: When the distribution engine receives a TCP connection from a download client, the scheduler pins the local NIC IPv4 address via `socket.bind()` and sends `setsockopt(socket.IPPROTO_IP, 31, ...)` to the system kernel to force-lock the physical interface index, stripping traffic away from the default gateway to achieve true physical multi-channel concurrency.
3. **Advanced Split Tunneling**: v2.0 can route processes through either the aggregation path or a direct path. Download workloads can use stacked bandwidth, while latency-sensitive apps can stay direct.

---

##  Real-World Multi-NIC Benchmarks

### Case A: IDM Multi-threaded Large File Aggregation (Ubuntu ISO Mirror)
> 190 active data channels handled simultaneously. Each adapter absorbs around **35~39 MB/s** evenly, pushing combined network throughput past **110.93 MB/s**!

<p align="center">
  <img src="assets/screenshot_2.0_idm.png" alt="IDM Bandwidth Stacking" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Case B: Steam High-Throughput Game Installation (*Hogwarts Legacy*)
> Flawlessly matching SteamService's multi-connection architecture, running lines concurrently to max out at **98.26 MB/s** combined downloading speed.

<p align="center">
  <img src="assets/screenshot_steam.png" alt="Steam Gigabit Acceleration" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Case C: WeGame Download Speed Showcase
> With v2.0 Virtual NIC mode and split tunneling, HypoMux can cover more game launcher download scenarios, allowing WeGame to benefit from higher aggregate throughput in multi-network environments.

<p align="center">
  <img src="assets/screenshot_2.0_wegame.png" alt="WeGame Download Speed Showcase" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Windows Task Manager Throughput Panels
> Three unique hardware interfaces (Ethernet, Ethernet 2, Wi-Fi) pushing data at **~300 Mbps** apiece at the exact same second.

<p align="center">
  <img src="assets/screenshot_taskmgr.png" alt="Task Manager Throughput Matrix" width="400" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

---

##  Building the Executable (Nuitka)

```powershell
venv\Scripts\activate
pip install nuitka zstandard PySide6-Fluent-Widgets
nuitka --standalone --onefile --enable-plugin=pyside6 --windows-console-mode=disable --windows-uac-admin --windows-icon-from-ico=support/icon.ico --include-package-data=qfluentwidgets --include-data-dir=support=support --python-flag=-O --lto=yes main.py
```

---

## ️ Security & Technical Boundaries

1. **Anti-Cheat Notice**: This tool operates at the standard application-layer proxy and network socket binding level. **It does not touch game memory, intercept or modify any game private network packets, or inject any DLL drivers**.
2. **Single-Thread Connection Limitation**: Multi-NIC concurrent aggregation is fundamentally **multi-connection load balancing**. If your download task is the extremely rare single-thread TCP connection, no multi-NIC aggregation tool can accelerate it.
3. **Low-Latency Gaming Advisory**: Multi-NIC distribution is optimized for download throughput. Before playing latency-sensitive competitive online games (e.g. *CS2*, *Valorant*, *GTA Online*), add the related process to the **Direct/Bypass routing list**, or click **Stop** so the PC returns to its normal network path.

---

##  Acknowledgments & Contributors

A special thanks to all the amazing developers who have contributed to the early core stability and engineering standardization of this project:

<a href="https://github.com/Hypostasis-Cat/HypoMux/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=Hypostasis-Cat/HypoMux" />
</a>

If you're also interested in multi-NIC traffic distribution and low-level network scheduling, feel free to submit a Pull Request and help improve HypoMux!

---

##  Support & Sponsorship

HypoMux is an open-source project driven purely by technical passion, independently developed and maintained by the author in their spare time. The author is currently a student, and the in-depth development and daily maintenance of the project (such as frequent use of AI tools for refactoring, API testing, etc.) involve certain real costs. If you find this tool genuinely solves your networking pain points, feel free to buy the author a coffee to support the continuous iteration of this project!

>  **Note:** Give within your means. Sponsorship is purely voluntary, and you can always use HypoMux's core features for free, regardless of whether you sponsor!
>
> Please leave your nickname when sponsoring!

<div align="center">
  <table>
    <tr>
      <td align="center" width="320">
        <img src="support/wei.png" alt="WeChat Sponsorship QR Code" width="260" />
        <br />
        <sub>WeChat Sponsorship (Please note: HypoMux Support)</sub>
      </td>
      <td align="center" width="320">
        <img src="support/zhi.jpg" alt="Alipay Sponsorship QR Code" width="260" />
        <br />
        <sub>Alipay Sponsorship (Please note: HypoMux Support)</sub>
      </td>
    </tr>
  </table>
</div>


### ️ Developer Statement
* **Regarding Feature Direction**: This project has a clear technical roadmap and architectural boundaries. All sponsorships are voluntary donations, and **sponsorship does not equate to commercial customization, nor can it directly determine or influence the direction of future feature development**.
* **Regarding Disclaimer**: This project is open-sourced under the **AGPL-3.0** license. The software is provided "as is", and the author assumes no liability for any direct or indirect damages resulting from the use of this tool.

### Individual Supporters

Thanks to all supporters who have injected energy into HypoMux:

<a href="https://github.com/Hypostasis-Cat/HypoMux"><img src="https://img.shields.io/badge/Whale-%20Buy%20a%20Coffee-orange?style=for-the-badge&logo=coffeescript&logoColor=white" alt="Sponsor"></a>

Thank you again for your respect and support for the open-source community and independent developers!

##  Star History

Join us on our journey to push Windows multi-adapter aggregation to its absolute limits!

[![Star History Chart](https://api.star-history.com/svg?repos=Hypostasis-Cat/HypoMux&type=Date)](https://star-history.com/#Hypostasis-Cat/HypoMux&Date)

##  License

This project is licensed under the **AGPL-3.0** License.
