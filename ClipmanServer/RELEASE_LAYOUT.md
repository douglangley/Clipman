# Clipman Server release layout

`Build.ps1` on Windows orchestrates the complete release build with the macOS
build host. `build.sh` performs the macOS side directly when a prebuilt Windows
wrapper is supplied in `CLIPMAN_SERVER_WINDOWS_EXE`.

The build output contains one directory named `ClipmanServer-<version>` with
these self-contained platform directories:

```text
windows-amd64/
macos-universal/
linux-amd64/
linux-arm64/
linux-armv7/
```

Every platform directory provides `clipmanserver` (the platform server-management
entrypoint), plus its installer, manifest, checksums, documentation, and platform
support files. Windows uses the `.exe` suffix. macOS retains
`Clipman Server.app`; the command-line `clipmanserver` launcher opens the app or
invokes its bundled Go core. Linux keeps the native daemon and updater under
`support/`.

The Clipman command-line client is intentionally absent. `ClipmanCli/Build.ps1`
and `ClipmanCli/build.sh` remain the only release builders for `clipman-cli`.

## Compatibility names

The new layout does not invalidate Python-era installations or instructions:

- `clipman-server` remains the Linux daemon launcher;
- `clipmanserver` remains the Linux service-management helper;
- `run-clipman-server.sh` remains a byte-identical Linux launcher alias;
- `install-clipman-server.sh` remains a byte-identical Linux installer alias;
- `Clipman Server.exe` remains beside `clipmanserver.exe` on Windows;
- `Clipman Server.app` retains its bundle name and identifier on macOS;
- installed settings, service, runtime, and data paths keep their existing
  `clipman-server` and `Clipman Server` names;
- `ClipmanServer-<version>.zip` remains the combined transition asset while old
  Python updaters require its historical manifest and top-level Python files.

Native update assets retain their established names:

```text
ClipmanServer-Windows-x64-<version>.zip
ClipmanServer-macOS-universal-<version>.zip
ClipmanServer-Linux-amd64-<version>.tar.gz
ClipmanServer-Linux-arm64-<version>.tar.gz
ClipmanServer-Linux-armv7-<version>.tar.gz
```

Windows and macOS updaters prefer their native asset and fall back to the
combined transition ZIP. The legacy Python Linux updater and the Go updater
select the Linux native asset through `manifest-v2.json`. This lets an existing
Python installation migrate without renaming its installed launcher, helper,
settings, service, or data files.
