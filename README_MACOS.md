# HypoMux for macOS (MVP)

This branch adds a native macOS build of HypoMux. It reuses the Go + Wails +
React desktop application and the Go connection scheduler from the upstream
Windows project.

## Implemented

- Native `HypoMux.app` bundle for Apple silicon or Intel macOS.
- HTTP/HTTPS and SOCKS5 local proxies.
- Connection-level scheduling across selected Ethernet, Wi-Fi and USB network
  interfaces by binding each outbound connection to the interface source IP.
- macOS network-service discovery, DNS and gateway metadata.
- Safe system-proxy snapshot, ownership check and restoration. macOS displays
  an administrator authorization dialog when enabling or restoring proxies.
- Source-bound TCP and ICMP diagnostics.
- Per-user login startup through a LaunchAgent.

## Not implemented yet

- TUN mode. The upstream implementation depends on Wintun, Windows Filtering
  Platform and Windows route/DNS orchestration. A production macOS TUN version
  should use a signed Network Extension (`NEPacketTunnelProvider`) or a
  carefully managed utun/sing-box helper and requires a separate privilege,
  signing and recovery design.
- In-app binary updating and Apple notarization.

The proxy MVP is best for multi-connection downloads in browsers and
proxy-aware launchers. As with upstream HypoMux, it cannot combine a single TCP
connection and does not create one shared public IP.

## Build

Requirements: macOS 12+, Xcode Command Line Tools, Go 1.26, Node.js, pnpm 10,
and Wails v3 `v3.0.0-alpha2.119`.

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.119
pnpm --dir desktop/frontend install --frozen-lockfile
cd desktop
wails3 task package
open bin/HypoMux.app
```

The generated development bundle is ad-hoc signed. Public distribution needs
a Developer ID signature and Apple notarization. This derivative remains
licensed under AGPL-3.0; source must be offered to users of a distributed build.
