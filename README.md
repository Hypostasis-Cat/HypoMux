# HypoMux

<p align="center">
  <img src="support/icon.ico" alt="HypoMux 图标" width="128" height="128"><br><br>
  <a href="README.md">简体中文</a> | <a href="README_EN.md">English</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Version-2.5.3-0078d4?style=flat-square" alt="Version 2.5.3">
  <img src="https://img.shields.io/badge/Core-Go-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Desktop-Wails%20v3-CB3837?style=flat-square" alt="Wails v3">
  <img src="https://img.shields.io/badge/UI-React%20%2B%20Fluent%20UI-61DAFB?style=flat-square&logo=react&logoColor=black" alt="React and Fluent UI">
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011-0078D4?style=flat-square&logo=windows" alt="Windows 10 and 11">
</p>

HypoMux 是一款面向 Windows 的开源多网卡聚合与分流工具。它把多连接下载任务分配到多张活动网卡，让有线网络、Wi-Fi、手机热点或 USB 网络共享能够同时承担流量。

HypoMux 聚合的是多个独立连接，而不是把单条 TCP 连接拆成多路。因此，它尤其适合 Steam、IDM、游戏平台更新器、浏览器大文件下载等高并发场景；单连接任务的速度仍受该连接本身限制。

## 2.5.3 新版本

2.5.3 完成了桌面端从 Python/Qt 与过渡期 WPF 实现到 **Go + Wails v3 + React + Fluent UI** 的正式迁移。桌面界面以普通用户权限运行，TUN、WFP、路由、DNS 与网络恢复等高权限操作交给独立的 Go Core/Windows 服务处理。

- **全 Go 后端**：桌面服务与网络引擎统一使用 Go，移除旧版 Python、Qt、asyncio 与 .NET/WPF 运行时依赖。
- **更安全的 TUN 生命周期**：启动前验证网卡、DNS、权限服务、Wintun、sing-box、WFP 与第三方 TUN；失败时在修改系统网络前阻止启动或自动回滚。
- **完整分流规则**：支持按进程、域名及子域名、目标 IP/CIDR 选择聚合、直连或指定网卡；旧配置中的多值规则会逐项迁移并保留。
- **第三方代理兼容**：识别常见本地代理与游戏加速器进程，并按进程路径或监听端口 PID 建立直连旁路，降低回环与相互代理风险。
- **可靠恢复**：系统代理状态采用原子化快照与恢复；异常退出、启动失败或重启后会继续尝试恢复原有设置。
- **新版个性化与诊断**：提供 Fluent UI 界面、明暗主题、Mica/材质、自定义背景、网卡体检、连接查看、日志与更新检查。

## 赞助方

<p align="center">
  <a href="https://signpath.io/"><img src="support/SignPath/SignPath.png" alt="SignPath" height="38" /></a>&nbsp;&nbsp;
  Windows 代码签名服务由 <a href="https://signpath.io/">SignPath.io</a> 免费提供，证书由 <a href="https://signpath.org/">SignPath Foundation</a> 颁发。
</p>

### Windows 代码签名政策（Code Signing Policy）

HypoMux 衷心感谢 SignPath 与 SignPath Foundation 对开源软件的支持，帮助我们为 Windows 用户提供更安全、可信的下载体验。

HypoMux 的官方 Windows 发布版本均由此仓库的 GitHub Actions 构建，并提交至 SignPath 进行代码签名。请仅从[官方 GitHub Releases 页面](https://github.com/Hypostasis-Cat/HypoMux/releases/latest)下载安装包，并确认已签名版本的发布者显示为 **SignPath Foundation**。

### 团队角色

* **提交者与审阅者：**[Hypostasis-Cat](https://github.com/Hypostasis-Cat)，项目维护者。非提交者发起的 Pull Request 必须经项目维护者审阅后方可合并。
* **签名审批者：**[Hypostasis-Cat](https://github.com/Hypostasis-Cat)。每次生产环境签名请求都会在 SignPath UI 中人工批准，再取得已签名构件。

### 隐私政策

HypoMux 不收集、出售或上传个人数据及遥测信息。程序仅会在用户或软件操作者请求相应功能时与其他网络系统通信：转发用户选择的网络流量、从官方 GitHub 仓库检查或下载安装更新，以及在启用虚拟网卡模式后进行网络连通性验证。

---

## 下载

> **Windows 安装包：**[前往 GitHub Releases 下载最新版](https://github.com/Hypostasis-Cat/HypoMux/releases/latest)
>
> 在最新版本的 **Assets** 中下载 `HypoMux_Setup_*.exe`。正式发布包通过 GitHub Actions 构建并由 SignPath 签名。

## 界面预览

### 默认界面

<p align="center">
  <img src="assets/ui_idle_2.5.png" alt="HypoMux 2.5 默认桌面界面" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### 自定义主题

<p align="center">
  <img src="assets/paper_dark.png" alt="HypoMux 深色自定义主题" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

## 核心功能

- **系统代理模式**：启动本地 HTTP/HTTPS 与 SOCKS5 服务，并接管 Windows 系统代理。资源占用较低，适合遵循系统代理的下载器、游戏平台和浏览器。
- **虚拟网卡模式**：通过 Wintun 与 sing-box 接管更广泛的系统流量，并结合 WFP、DNS 和路由规则完成精细分流。
- **多网卡连接调度**：为每个新连接选择出站网卡，并使用源地址绑定与 `IP_UNICAST_IF` 将套接字固定到真实物理链路。
- **高级路由规则**：按进程、域名、IP/CIDR 指定聚合、直连、以太网、Wi-Fi 或某张具体网卡。
- **单网卡受限域名处理**：记录特定链路不可访问的域名，避免后续连接继续分配到该链路。
- **实时状态与诊断**：展示每张网卡的速率、连接数和聚合吞吐，并提供丢包、延迟、抖动、DNS、网关及源地址绑定检查。
- **最小权限架构**：界面不以管理员身份常驻；正式安装后，仅独立 Core 服务持有必要的网络管理权限。

### 两种运行模式如何选择

| 模式 | 覆盖范围 | 权限与兼容性 | 推荐场景 |
| --- | --- | --- | --- |
| 系统代理 | 遵循 Windows 系统代理的应用 | 更轻量；不创建虚拟网卡 | IDM、浏览器、Steam 等代理感知型下载 |
| 虚拟网卡 | 更广泛的 TCP/UDP 与非代理感知流量 | 需要 Core 服务、Wintun/WFP；不能与其他 TUN 同时接管默认路由 | 游戏平台下载、WeGame、复杂分流和全局接管 |

## 📢 重要提示与合规免责声明

HypoMux 是一个透明、开源的网络工具，仅用于用户本人拥有授权的设备与网络连接。它不应用于绕过第三方访问控制、网络限制、平台规则或任何未经授权的安全措施。

使用前请确认你理解以下行为边界：

1. **系统设置调整**：HypoMux 运行时可能会动态调整 Windows 系统代理和/或路由相关设置，以便将流量导入加速核心。
2. **本地安全代理**：启用加速后，需要加速的网络流量会经过本机安全核心进行分流、代理与多路复用。
3. **自动恢复机制**：停止工具或卸载软件时，HypoMux 会自动恢复被修改的系统代理与网络设置。
4. **游戏与分流规则**：HypoMux 提供高级分流能力。对于竞技类网游等对延迟极度敏感的应用，建议将其加入**直连/绕过规则列表**，以保持原始网络延迟；也可以在游戏时暂停本工具。

---

## 快速开始

1. 让电脑同时连接至少两条可用网络，例如有线宽带 + Wi-Fi，或宽带 + 手机 USB/热点共享。
2. 启动 HypoMux，在首页刷新并勾选要加入聚合池的活动网卡。
3. 先运行“网络体检”，确认各链路具有有效 IPv4、网关、DNS 和源地址绑定能力。
4. 选择系统代理或虚拟网卡模式。低延迟游戏、语音和会议程序可先加入直连规则。
5. 开启聚合引擎，再启动下载任务。若 Steam 已经运行，按界面提示重启 Steam 以完整应用代理设置。
6. 使用完毕后停止聚合或正常退出，HypoMux 会恢复其接管的系统网络设置。

## 第三方代理与游戏加速器兼容

2.5.3 对常见本地代理与游戏加速器增加了专门的兼容旁路，包括 UU、迅游、雷神、奇游，以及 Clash/Mihomo、v2rayN、Hiddify、Shadowsocks、Proxifier 等常见进程族。运行中的程序会优先按完整可执行文件路径识别；本地系统代理监听器还会按端口反查 PID，避免只依赖容易变化的进程名。

仍需注意以下边界：

- 如果第三方程序只提供本地 HTTP/SOCKS 代理，HypoMux 的 TUN 模式会尽量让其自身进程与监听器直连，避免代理回环。
- 两个程序不应同时争用 Windows“系统代理”开关。使用 HypoMux 系统代理模式时，请关闭另一个程序的系统代理接管。
- 如果第三方程序创建了自己的 TUN/VPN 虚拟网卡并接管默认路由，请先关闭它，再启动 HypoMux 虚拟网卡模式。HypoMux 会在修改系统网络前检测并阻止此类冲突。
- 代理或加速器版本更新后若更换了进程结构，建议先查看 HypoMux 的兼容提示与支持日志，再决定使用系统代理模式或关闭冲突程序。

## 工作原理

```text
应用连接
   │
   ├─ 系统代理：HTTP/HTTPS 10801 · SOCKS5 10800
   └─ 虚拟网卡：Wintun + sing-box + WFP/DNS/路由规则
                         │
                  hypomux-engine.exe
                         │
              按连接选择聚合/直连/指定网卡
               ┌─────────┼─────────┐
             网卡 1     网卡 2     网卡 3
               └─────────┴─────────┘
                     合并吞吐
```

系统代理模式将本地代理链写入当前用户的 Windows Internet Settings；虚拟网卡模式则通过独立权限服务创建和管理 TUN、WFP、DNS 与路由资源。Go 引擎对每个新连接选择出口，把本地源地址绑定和 Windows 接口索引绑定组合使用，从而让不同连接稳定地走不同物理网卡。

## 支持场景与技术边界

- 适合 IDM、Steam、Epic Games Launcher、EA App、Xbox、WeGame、Chrome、Edge、Firefox 等会产生多个并发连接的下载场景。
- 多网卡聚合是**连接级负载分配**，不会让单条 TCP 连接突破其原有链路速度，也不会把多条线路变成一个具有单一公网 IP 的链路聚合协议。
- 聚合侧重吞吐量，不保证降低延迟。竞技游戏、语音和视频会议建议使用直连规则或暂时停止聚合。
- HypoMux 不读取游戏内存、不注入 DLL，也不修改游戏私有协议数据包；但第三方平台和反作弊规则各不相同，用户仍应遵守相应服务条款。
- 仅可在本人拥有或已获授权的设备与网络上使用，不得用于绕过未经授权的访问控制、安全措施或平台限制。

## 实测效果

下列截图来自早期版本的真实多网卡、多连接测试，用于说明连接级聚合能力；实际速度取决于每条线路、下载源、并发数、磁盘和 CPU，不能视为性能保证。

### IDM 多线程大文件下载

<p align="center">
  <img src="assets/screenshot_2.0_idm.png" alt="IDM 多网卡并发下载" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Steam 游戏更新

<p align="center">
  <img src="assets/screenshot_steam.png" alt="Steam 多网卡并发更新" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### WeGame 游戏下载

<p align="center">
  <img src="assets/screenshot_2.0_wegame.png" alt="WeGame 多网卡并发下载" width="850" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

### Windows 任务管理器多网卡吞吐

<p align="center">
  <img src="assets/screenshot_taskmgr.png" alt="任务管理器多网卡吞吐" width="400" style="border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">
</p>

## 开发与构建

推荐环境：Windows 10/11、Go 1.26、Node.js 22、pnpm 10、Wails v3 CLI `v3.0.0-alpha2.119`。构建安装包还需要 NSIS；仓库根目录 `bin/` 必须包含官方运行时文件 `sing-box.exe`、`wintun.dll` 和 `libcronet.dll`。

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
# 或构建 NSIS 安装包
wails3 task windows:package
```

完整发布流程以 [`.github/workflows/build.yml`](.github/workflows/build.yml) 为准。

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
