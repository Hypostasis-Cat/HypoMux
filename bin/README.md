# Bundled runtime assets

## sing-box

- Version: **1.14.4** (Windows amd64, official SagerNet build).
- Release: https://github.com/SagerNet/sing-box/releases/tag/v1.14.4
- Archive: `sing-box-1.14.4-windows-amd64.zip`
- Archive SHA-256: `99185d75d314a07dbba4a130c8ad986cbeb629a07927975636b341ca61f991df`
- `sing-box.exe` SHA-256: `8f30682ee198855440d2a7fe070d5d3653793b2d01bb8c85eea88f93bbe7c8b9`
- Upstream revision: `c005f35300ce76427dc285481ec5695f5b179cd2`

The official binary includes gVisor support. HypoMux explicitly selects the
1.14+ TUN DNS mode: `disabled` for the system DNS policy and `hijack` for the
other policies. Stop and restart TUN after upgrading to load the new core and
regenerate its configuration.

Normal shutdown now interrupts the core and allows up to five seconds for it
to save FakeIP metadata and exit before releasing process containment. Windows
uses a private hidden console and a helper that stays attached until the core
exits. Engine regression tests exercise immediate and repeated cached-only
restarts against this binary without waiting for a periodic checkpoint.

If the core hangs or the shutdown request is canceled, forced termination still
cleans up its process tree. That fallback (like a crash) cannot guarantee FakeIP
cache preservation; normal shutdown should be used whenever possible.

## wintun

- Version: **0.14.1** (Windows amd64, official WireGuard build).
- Source: https://www.wintun.net/
- `wintun.dll` SHA-256: `e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce`
- Vendor stamp: "Wintun API Library", WireGuard LLC (FileVersion/ProductVersion 0.14.1).

wintun.dll is the driver API library the engine loads for virtual-adapter mode.
It ships inside the installer, is covered by the installer's payload digest
verification, and is replaced only through the protected sidecar upgrade
transaction.
Verify a replacement before committing it:

```powershell
certutil -hashfile bin\wintun.dll SHA256
```

The official archive also contains `libcronet.dll`. It is not shipped: sing-box
loads it only for the naive outbound, which HypoMux never configures, and the
core runs normally without it. Ship it again if a naive outbound is ever added.

Windows packaging copies these runtime assets into `desktop/bin`. When testing
locally, keep that copy synchronized: runtime discovery can select it before
the repository's `bin` directory.
