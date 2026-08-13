# Go Server Rewrite Resume Status

Last updated: 2026-08-13 (native macOS wrapper/package and server-function matrix exercised)

Branch: `go-server`

## Resume point

The implementation is safe to resume from. The rewrite checkpoints through the first Phase 7 slice, oldest first, are:

1. `7a9f365 Start Go server compatibility rewrite`
2. `534b20f Implement Go database synchronization`
3. `ee2042d Implement server administration and updater core`
4. `c699eea Record Go server rewrite resume status`
5. `999bf03 Start native platform packaging`
6. `2c42330 Migrate Linux helpers to native updater`
7. `f6028cd Start server release compatibility gates`
8. `7d7f365 Prepare Python to Go bridge updates`

The native macOS checkpoint described below follows those commits and is included with this status update. The worktree should be clean after its commit; do not discard or absorb unrelated user files if a later checkout is not clean.

Do not discard or absorb unrelated untracked files shown by `git status`. They predate or are outside the Go rewrite and belong to the user.

## Implemented

- Go module and `clipman-server`/compatibility commands.
- Compatible settings defaults, sparse-file behavior, path handling, stable output, network-safety checks, data-root locking, listener startup, and graceful shutdown.
- Health endpoint and bearer authentication boundary.
- Database ID validation, revisions, metadata, per-bucket locks, bounded streaming `HEAD`/`GET`, staged atomic conditional `PUT`, and backup hooks.
- Database inventory, JSON listing, guarded deletion, stale selection/pruning, and recoverable moves to `DeletedDatabases`.
- Authenticated legacy backup listing plus the compatibility-scoped backup and restore error routes.
- `.clpconf` and text connection files.
- First-run wrapper connection-file generation and validated safe setup-base URLs.
- Expiring and download-limited setup links, setup HTTP routes, redaction, and concurrency-safe consumption.
- Native-Go RSA private CA and leaf generation, renewal using the existing CA, inspection, fingerprints, TLS settings updates, and temporary public-CA sharing.
- Standalone `clipman-server-updater` command.
- Manifest v2 parsing and OS/architecture artifact selection, including same-version Python-to-Go migration selection.
- HTTPS-only bounded downloads, SHA-256 verification, archive path/symlink/count/expanded-size defenses, staged replacement, service coordination, health checking, and rollback.
- Universal macOS Swift wrapper and Go core packaging with transactional app replacement and post-relaunch health rollback.

## Verification completed

- `cd ClipmanServer && go test ./...`
- `cd ClipmanServer && go vet ./...`
- `cd ClipmanServer && go test -race ./...` on macOS.
- Windows builds of the server, updater, and real `clipman-cli`.
- Real CLI `init`, `put`, `list`, `sync`, and `status` against the Go HTTP server with Unicode dummy data.
- Go-to-Python-to-Go in-place database handoff with both entries preserved.
- Go-generated private CA and leaf certificate, `.clpconf` import, and real CLI synchronization over HTTPS without installing the CA into the operating-system trust store.
- Unit/HTTP coverage for administration, setup-link concurrency and limits, certificate renewal without CA replacement, malicious archive rejection, digest/architecture selection, same-version migration selection, and failed-health rollback with service restart.
- Historical Python compatibility suite: 57 tests passed with three platform-specific skips on Windows after updating its Docker assertion for the native entrypoint.
- Phase 7 verification rerun: `go test ./...`, `go vet ./...`, Python bytecode compilation, and `git diff --check` passed.

## Important remaining work

Phase 2 is not fully release-gated. Complete runtime counters and health parity, raw malformed/conditional/`100-continue` differentials, concurrent first-writer and multi-bucket scenarios, backup characterization, race testing, TLS edge cases, 1 KiB through 64 MiB bounded-memory transfers, and the complete bidirectional Python/Go handoff matrix.

Phase 3 server-core behavior is implemented. Live macOS certificate sharing now passes; still expand raw differential coverage, RSA legacy-fixture verification, setup-link expiry/HEAD cases, and exact stable-output comparisons with Python.

Phase 4 updater security and transaction core is implemented. The macOS wrapper now performs staged app replacement, nested-code validation, local health checking, and rollback. Concrete Windows service/update coverage and remaining systemd/runit/external package paths stay coupled to Phase 5. Add complete package-mode before/update/rollback tests and prove settings/data remain byte-for-byte unchanged on every failed update.

## Phase 7 bridge checkpoint

The legacy Python updater now understands `ClipmanServer-Linux-<architecture>-<version>.tar.gz` assets, validates their manifest-v2 artifact and SHA-256 digest, and safely rejects tar traversal, links, devices, excessive entry counts, and excessive expanded size. Its normal release check prefers the native asset, including a same-version migration from Python to Go, while retaining the transition ZIP fallback for a newer release.

Managed Linux migration installs the native core beside the retained Python files and atomically changes the launcher. Program-file snapshots include both the native core and launcher, so failed health checks can restore the prior Python launch path without rewriting settings or data. Unit coverage exercises native selection, transition fallback, archive validation, unsafe links, installation, and restoration. The historical Python suite now also asserts that the migrated Docker entrypoint invokes the native binary rather than Python.

No external release or asset publication has occurred.

The next Phase 7 slice fixed ordinary historical installations: after validating a native tar, the bridge now installs its native core directly instead of incorrectly looking for the legacy transition ZIP's shell installer. The generated launcher safely quotes paths containing spaces and apostrophes. Isolated tests model the old-updater transition-ZIP selection followed by the bridge updater's same-version native selection, a successful ordinary installation, and a failed native health check. Dummy settings, opaque database bytes, TLS authority material, connection files, service definitions, management helper, and retained Python fallback files are asserted byte-for-byte across the relevant paths.

Current verification is 60 historical Python tests passing with three Linux-only skips on Windows, plus `go test ./...`, `go vet ./...`, and `git diff --check`.

## Full Windows CLI acceptance checkpoint

The reusable `ClipmanServer/scripts/windows-cli-acceptance.ps1` corpus passed against the native Go server on Windows using an isolated server root and two isolated CLI profiles. It created 300 records through real CLI invocations and finished with 332 live history records after additions and deletions from both clients. Coverage included `init`, `status`, `status --refresh`, `put`, `list`, `get`, `rm`, and `sync`; groups, pinned entries, duplicate modes, templates, tombstones, searches, UTF-8, CRLF, and embedded NUL data were exercised. Interactive `menu` and `pick` presentation remain manual-only.

Client A and client B converged after A-to-B and B-to-A mutations. The same encrypted server bucket then survived Go-to-Python-to-Go process handoff, including a record written while Python owned the data root and read after Go restarted. The final canonical logical-history SHA-256 was `49bc693f12cac07ac0c977d5aaad661daba0ec3a404d5055f1c9f9f4818f10ad`; the final encrypted `.clipdb` SHA-256 was `fa3d5d7d6dd30a947323ca4a1c09c3426e40db924151543186de268671c949bb`. Administrative inventory reported exactly one healthy bucket, 17,704 bytes, with one backup. No personal Clipman settings or data were used.

After the corpus, the full Go server test/vet suite, full CLI test/vet suite, and all 60 Python server/updater/installer tests passed; three Linux-only tests skipped on Windows. Machine-readable results are stored in `ClipmanServer/compat/windows-cli-acceptance-latest.json`.

## Native Linux and Docker verification checkpoint

Real amd64 Go server and updater binaries were built in the Go container and installed with the shipped user installer into isolated non-Docker Linux homes. Fresh install, reinstall with byte-identical settings preservation, start, duplicate start, stop, restart, status, console, token, connection files, setup links, list/list-json, prune, guarded delete, forced delete, host and port changes, update checks, and no-op current-version update all passed. Unmanaged automatic-update enable/status correctly refuse because they require an installed service manager; systemd and runit command paths remain covered by isolated installer integration tests. No uninstall command exists yet.

Private-CA TLS was exercised through the installed helper. Certificate creation, authenticated HTTPS health, connection-file regeneration, fingerprint display, renewal with byte-identical CA preservation, and one-time CA sharing with a byte-identical downloaded public certificate passed. The matrix exposed and fixed unmanaged restart racing the data-root lock, live list commands violating that lock, duplicate start reporting false success, and a two-address host update running twice.

The production Docker image builds and runs on amd64 without Python or OpenSSL, runs as UID/GID 10001, supports Docker command overrides, accepts a read-only root filesystem with `/data` writable, preserves volume state across restart, answers health checks, and exits cleanly on SIGTERM. An arm64 build was attempted, but this amd64 host has no ARM binfmt/QEMU registration, so execution stopped with `exec format error`; arm64 runtime validation remains for an arm64 runner or a host with emulation configured.

After these fixes, all 60 Linux Python server/updater/installer compatibility tests pass, `go test ./...` and `go vet ./...` pass from the full repository mount, installer shell syntax passes, and `git diff --check` is clean.

## Native macOS verification checkpoint

The macOS release script now builds both the Swift status-menu wrapper and embedded Go core as universal arm64/x86_64 executables targeting macOS 13, signs the nested core and wrapper before the app, verifies the strict nested signature, checks both architecture slices, and fails on wrapper/core version drift. The native artifact is named `ClipmanServer-macOS-universal-<version>.zip`; the Swift updater prefers it and retains the combined transition ZIP as a fallback. Standalone and combined ZIPs were built, extracted, signature-checked, and their embedded core versions verified on Apple Silicon macOS.

The Swift wrapper launches and routes utilities directly to the bundled Go core. Its updater now accepts the standalone native archive layout, rejects missing core/wrapper executables and symbolic links, verifies the app signature before replacement, stages on the destination volume, waits for the relaunched server's local health endpoint, and restores/relaunches the previous app if replacement, relaunch, or health verification fails. Command-line `--version` is machine-readable.

The packaged core passed live macOS checks for health, authenticated conditional `PUT`, `HEAD`, `GET`, backup listing, legacy backup/restore scoped errors, inventory, guarded deletion, setup-link creation/revocation, private-CA generation and fingerprinting, direct HTTPS, and one-download CA sharing. First-run connection-file creation, setup-base-URL validation, and the legacy backup routes gained regression tests. `go test ./...`, `go vet ./...`, `go test -race ./...`, both Swift architecture type checks, shell syntax checks, and `git diff --check` pass.

## Recommended next action

Continue Phase 7 by adding fixtures from additional supported historical releases and running compatibility `package` mode against an actual installed native artifact after a successful transition and after forced rollback. On macOS, run the packaged CLI corpus and installed-app update/forced-rollback flow on clean Intel and Apple Silicon machines, then complete Developer ID signing, notarization, and Gatekeeper assessment. Add manual-recovery documentation before publishing anything. No bridge or native release has been published.

Before changing files, run:

```powershell
git switch go-server
git status --short
git log -3 --oneline
```

The native macOS checkpoint commit follows `027fbef`. Untracked user files must remain untouched.

The authoritative design and phase exit criteria remain in `GO_SERVER_REWRITE_PLAN.md`; this file is the concise interruption/recovery checkpoint.
