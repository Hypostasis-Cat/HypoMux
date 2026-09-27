# Bundled runtime assets

## sing-box

- Version: **1.14.2** (Windows amd64, official SagerNet build).
- Release: https://github.com/SagerNet/sing-box/releases/tag/v1.14.2
- Archive: `sing-box-1.14.2-windows-amd64.zip`
- Archive SHA-256: `c2d8bfff918755808781dfdeeb8581b6c91eb3a243d9a7b55483cfc0c0684d32`
- `sing-box.exe` SHA-256: `7bbef1dea9189ee12799ae834ea4b4658355da25c47a21ad8804904c0ccd9410`
- Upstream revision: `af6e64c3b69e6132ebaee0e1a3d24e93903f6709`

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

Windows packaging copies these runtime assets into `desktop/bin`. When testing
locally, keep that copy synchronized: runtime discovery can select it before
the repository's `bin` directory.
