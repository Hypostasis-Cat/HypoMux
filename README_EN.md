# HypoMux

<p align="center">
  <img src="support/icon.ico" alt="HypoMux icon" width="128" height="128"><br><br>
  <a href="README.md">简体中文</a> | <a href="README_EN.md">English</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Version-2.5.9-0078d4?style=flat-square" alt="Version 2.5.9">
  <img src="https://img.shields.io/badge/Core-Go-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Desktop-Wails%20v3-CB3837?style=flat-square" alt="Wails v3">
  <img src="https://img.shields.io/badge/UI-React%20%2B%20Fluent%20UI-61DAFB?style=flat-square&logo=react&logoColor=black" alt="React and Fluent UI">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-0078D4?style=flat-square&logo=windows" alt="Windows 10 and 11">
</p>

HypoMux is an open-source multi-adapter aggregation and split-routing utility for Windows. It distributes multi-connection download workloads across active network adapters, allowing Ethernet, Wi-Fi, mobile hotspots, and USB tethering to carry traffic at the same time.

HypoMux balances independent connections; it does not split one TCP connection across multiple paths. It works best with highly concurrent workloads such as Steam updates, IDM downloads, game launchers, and large browser downloads. A single-connection transfer remains limited by that connection.

## What's new in 2.5.0

Version 2.5.0 completes the desktop migration from the former Python/Qt and transitional WPF implementations to **Go + Wails v3 + React + Fluent UI**. The desktop runs as a standard user, while an independent Go Core/Windows service owns privileged TUN, WFP, routing, DNS, and network-recovery operations.

- **All-Go backend**: Desktop services and the network engine now use Go, with no Python, Qt, asyncio, or .NET/WPF runtime dependency.
- **Safer TUN lifecycle**: Adapters, DNS, the privileged service, Wintun, sing-box, WFP, and foreign TUNs are checked before startup. Failures are blocked before network changes or rolled back automatically.
- **Complete split routing**: Route by process, domain and subdomain, destination IP/CIDR, aggregation, direct connection, or a specific adapter. Multi-value legacy rules are expanded and preserved.
- **Third-party proxy compatibility**: Common local proxies and game accelerators are bypassed by executable path or listener PID to reduce loops and proxy-on-proxy failures.
- **Reliable recovery**: Windows system-proxy state is snapshotted and restored atomically. Recovery is retried after failed startup, abnormal exit, or the next launch.
- **New personalization and diagnostics**: Fluent UI, light/dark themes, Mica/material effects, custom backgrounds, adapter health checks, live connections, support logs, and update checks.

## Sponsors

<p align="center">
  <a href="https://signpath.io/"><img src="support/SignPath/SignPath.png" alt="SignPath" height="38" /></a>&nbsp;&nbsp;
  Free code signing provided by <a href="https://signpath.io/">SignPath.io</a>, certificate by <a href="https://signpath.org/">SignPath Foundation</a>.
</p>

### Code Signing Policy

HypoMux is sincerely grateful to SignPath and the SignPath Foundation for supporting open-source software and helping us deliver a safer Windows download experience.

The official Windows releases of HypoMux are built from this repository through GitHub Actions and submitted to SignPath for code signing. [GitHub Releases](https://github.com/Hypostasis-Cat/HypoMux/releases/latest) is the authoritative public release page, while [Tencent CNB Release](https://cnb.cool/Hypostasis-Cat/HypoMux/-/releases/latest) is the official mainland China mirror; both distribute only the same SignPath-signed installer. Automatic update metadata is delivered through a separate signed update channel and verified with Ed25519, followed by installer size, SHA-256, and Windows Authenticode verification. Signed installers should show **SignPath Foundation** as the publisher.

### Team roles

* **Committer and reviewer:** [Hypostasis-Cat](https://github.com/Hypostasis-Cat), the project maintainer. Pull requests from non-committers must be reviewed by a project maintainer before merging.
* **Approver:** [Hypostasis-Cat](https://github.com/Hypostasis-Cat). Each production signing request is manually approved in the SignPath UI before the signed artifact is retrieved.

### Privacy policy

HypoMux does not collect, sell, or upload personal data or telemetry. The program contacts other networked systems only to perform functionality requested by the user or the person operating it: forwarding selected network traffic, checking the official signed update channel, downloading installers from GitHub or CNB Release, and validating connectivity after Virtual NIC mode has been enabled.

---

## Download

> **Windows installer:** [GitHub Releases](https://github.com/Hypostasis-Cat/HypoMux/releases/latest) · [Tencent CNB Release](https://cnb.cool/Hypostasis-Cat/HypoMux/-/releases/latest)
>
> Download the latest `HypoMux_Setup_*.exe`. Official releases are built by GitHub Actions and signed through SignPath.

## UI preview

### Default interface

<p align="center">
  <img src="assets/ui_idle_2.5.png" alt="HypoMux 2.5 default desktop interface" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Custom theme

<p align="center">
  <img src="assets/paper_dark.png" alt="HypoMux custom dark theme" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

## Core features

- **System Proxy mode**: Starts local HTTP/HTTPS and SOCKS5 services. It manages the Windows system proxy by default, or can leave Windows unchanged and expose only the local ports to manually configured downloaders and other apps. It is lightweight and works with proxy-aware software.
- **Virtual NIC mode**: Uses Wintun and sing-box to capture a broader range of system traffic, with WFP, DNS, and routing rules for precise split tunneling.
- **Per-connection adapter scheduling**: Selects an outbound adapter for each new connection, combining source-address binding with `IP_UNICAST_IF` to pin sockets to physical links.
- **Advanced routing rules**: Route by process, domain, IP/CIDR, aggregation, direct connection, Ethernet, Wi-Fi, or one specific adapter.
- **Per-adapter blocked-domain handling**: Remembers domains unavailable through a particular link so later connections are not assigned to it again.
- **Live telemetry and diagnostics**: Shows adapter throughput, connection counts, and combined speed, with packet-loss, latency, jitter, DNS, gateway, and source-binding checks.
- **Least-privilege architecture**: The UI does not stay elevated. After a normal installation, only the independent Core service has the network-management permissions it needs.

### Choosing a mode

| Mode | Coverage | Privileges and compatibility | Recommended use |
| --- | --- | --- | --- |
| System Proxy | Applications that honor the Windows system proxy or are configured with the local HTTP/SOCKS5 ports | Lightweight; optional system-proxy management; no virtual adapter | IDM, browsers, Steam, and other proxy-aware downloads |
| Virtual NIC | Broader TCP/UDP and non-proxy-aware traffic | Requires the Core service and Wintun/WFP; cannot share the default route with another TUN | Game-launcher downloads, WeGame, advanced split routing, and broader capture |

## 📢 Important Notice & Compliance Disclaimer

HypoMux is a transparent, open-source network utility intended only for authorized use on the user's own devices and network connections. It is not designed or permitted for bypassing third-party access controls, network restrictions, platform rules, or any security measures without authorization.

Before using HypoMux, please be aware of the following behavior:

1. **System Changes**: While active, HypoMux may dynamically adjust Windows system proxy and/or routing-related settings so traffic can enter the acceleration core.
2. **Local Proxying**: Accelerated traffic is processed locally through the secure core for splitting, proxying, and multiplexing.
3. **Automatic Restoration**: Modified system proxy and network settings are restored automatically when the tool is stopped or uninstalled.
4. **Gaming & Split Tunneling**: HypoMux provides advanced split tunneling. For latency-sensitive applications such as competitive online games, users are strongly advised to add them to the **Direct/Bypass routing list** to preserve raw network latency, or suspend HypoMux during gameplay.

---

## Quick start

1. Connect the PC to at least two working networks, such as Ethernet + Wi-Fi or broadband + USB/mobile tethering.
2. Start HypoMux, refresh the Home page, and select the active adapters to include in the pool.
3. Run Network Health first and verify that every link has a valid IPv4 address, gateway, DNS, and working source binding.
4. Choose System Proxy or Virtual NIC mode. Add games, voice chat, and meeting applications to direct rules when latency matters.
5. Enable the aggregation engine, then start the download. If Steam was already running, restart it when prompted so the proxy change takes full effect.
6. Stop aggregation or exit normally when finished. HypoMux restores the system network settings it owned.

## Compatibility with third-party proxies and game accelerators

Version 2.5.3 adds dedicated bypass handling for common local proxies and game accelerators, including process families used by UU, Xunyou, Leigod, Qiyou, Clash/Mihomo, v2rayN, Hiddify, Shadowsocks, and Proxifier. Active products are detected by full executable path where possible, while local system-proxy listeners are resolved back to their owning PID instead of relying only on mutable process names.

The following boundaries still apply:

- If a third-party product only exposes a local HTTP/SOCKS proxy, HypoMux Virtual NIC mode attempts to bypass that product's process and listener to avoid a proxy loop.
- Two applications should not compete for the Windows system-proxy switch. Disable the other application's system-proxy takeover before enabling HypoMux System Proxy mode.
- If another product creates a TUN/VPN adapter and owns the default route, close it before starting HypoMux Virtual NIC mode. HypoMux detects this conflict and blocks startup before changing the system network.
- A proxy or accelerator update may change its process layout. Check HypoMux's compatibility notice and local support log before choosing another mode or closing the conflicting product.

## How it works

```text
Application connections
   │
   ├─ System Proxy: HTTP/HTTPS 10801 · SOCKS5 10800
   └─ Virtual NIC: Wintun + sing-box + WFP/DNS/routing rules
                              │
                       hypomux-engine.exe
                              │
                 Aggregation / direct / adapter route
                    ┌─────────┼─────────┐
                  NIC 1     NIC 2     NIC 3
                    └─────────┴─────────┘
                       Combined throughput
```

System Proxy mode writes the local proxy chain to the current user's Windows Internet Settings. Virtual NIC mode asks the independent privileged service to create and manage TUN, WFP, DNS, and route resources. For every new connection, the Go engine chooses an outbound path and combines local source-address binding with the Windows interface index to keep different connections on their selected physical adapters.

## Supported scenarios and technical boundaries

- Suitable for multi-connection download workloads from IDM, Steam, Epic Games Launcher, EA App, Xbox, WeGame, Chrome, Edge, Firefox, and similar applications.
- Multi-adapter aggregation is **connection-level load distribution**. It cannot make one TCP connection exceed its original path speed, nor does it turn several providers into one bonded link with a single public IP.
- Aggregation targets throughput, not lower latency. Use direct rules or stop aggregation for competitive games, voice chat, and video meetings.
- HypoMux does not read game memory, inject DLLs, or modify private game-protocol packets. Platform and anti-cheat policies vary, so users must still follow the applicable terms of service.
- Use HypoMux only on devices and network connections you own or are authorized to manage. It is not intended to bypass unauthorized access controls, security measures, or platform restrictions.

## Real-world results

The following images are real multi-adapter, multi-connection tests captured with earlier builds. They demonstrate connection-level aggregation; actual results depend on each link, the download source, concurrency, storage, and CPU, and are not a performance guarantee.

### IDM multi-threaded download

<p align="center">
  <img src="assets/screenshot_2.0_idm.png" alt="IDM multi-adapter download" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Steam game update

<p align="center">
  <img src="assets/screenshot_steam.png" alt="Steam multi-adapter update" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### WeGame game download

<p align="center">
  <img src="assets/screenshot_2.0_wegame.png" alt="WeGame multi-adapter download" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Multiple adapters in Windows Task Manager

<p align="center">
  <img src="assets/screenshot_taskmgr.png" alt="Multi-adapter throughput in Task Manager" width="400" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

## Development and building

Recommended environment: Windows 10/11, Go 1.26, Node.js 22, pnpm 10, and Wails v3 CLI `v3.0.0-alpha2.119`. NSIS is also required to package the installer. The repository's `bin/` directory must contain the official runtime files `sing-box.exe`, `wintun.dll`, and `libcronet.dll`.

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.119
pnpm --dir desktop/frontend install --frozen-lockfile
Push-Location desktop
wails3 generate bindings -clean=true -ts -i
Pop-Location

go -C engine test ./...
go -C desktop test ./...
pnpm --dir desktop/frontend build

Set-Location desktop
wails3 dev
# Or build the NSIS installer
wails3 task windows:package
```

See [`.github/workflows/build.yml`](.github/workflows/build.yml) for the complete release pipeline.
Before a production release, the read-only `Release Trust Smoke Test` workflow can validate the update signing key, synchronized GitHub/CNB tags, CNB Release access, and any existing signed update channel without creating a Release, uploading assets, or modifying the channel.

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

#### ✨ Special Thanks

<p align="center">
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, special thanks" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, special thanks" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="六花 DY, special thanks" src="https://img.shields.io/static/v1?label=%E5%85%AD%E8%8A%B1%20DY&message=%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5&color=DCD0FF&labelColor=E6E6FA&style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, special thanks" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, special thanks" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E7%BB%99%E7%8C%AB%E5%92%AA%E5%8F%91%E7%94%B5-DCD0FF?style=for-the-badge&logo=github-sponsors&logoColor=6A5ACD&labelColor=E6E6FA" /></a>
</p>

#### ☕ Coffee Support

<p align="center">
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Shout_bb, ¥6.66 coffee support" src="https://img.shields.io/static/v1?label=Shout_bb&message=%C2%A56.66&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img src="https://img.shields.io/badge/Whale-%20Buy%20a%20Coffee-orange?style=for-the-badge&logo=coffeescript&logoColor=white" alt="Whale, coffee support"></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="廾阁, coffee support" src="https://img.shields.io/badge/%E5%BB%BE%E9%98%81-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="SK, coffee support" src="https://img.shields.io/badge/SK-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="WZLN, coffee support" src="https://img.shields.io/static/v1?label=WZLN&message=%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="幸運上上簽, coffee support" src="https://img.shields.io/static/v1?label=%E5%B9%B8%E9%81%8B%E4%B8%8A%E4%B8%8A%E7%B1%A4&message=%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="HEDE WANG, coffee support" src="https://img.shields.io/static/v1?label=HEDE%20WANG&message=%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="画船倾雨, coffee support" src="https://img.shields.io/static/v1?label=%E7%94%BB%E8%88%B9%E5%80%BE%E9%9B%A8&message=%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1&color=orange&style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
  <a href="https://github.com/Hypostasis-Cat/HypoMux"><img alt="Anonymous, coffee support" src="https://img.shields.io/badge/%E5%8C%BF%E5%90%8D-%E8%AF%B7%E5%96%9D%E5%92%96%E5%95%A1-orange?style=for-the-badge&logo=coffeescript&logoColor=white" /></a>
</p>

Thank you again for your respect and support for the open-source community and independent developers!

##  Star History

Join us on our journey to push Windows multi-adapter aggregation to its absolute limits!

[![Star History Chart](https://api.star-history.com/svg?repos=Hypostasis-Cat/HypoMux&type=Date)](https://star-history.com/#Hypostasis-Cat/HypoMux&Date)

##  License

This project is licensed under the **AGPL-3.0** License.
