# Go Server Rewrite Resume Status

Last updated: 2026-08-10 (Phase 6 started; build matrices deferred)

Branch: `go-server`

## Resume point

The current implementation is committed and safe to resume from. The three rewrite commits, oldest first, are:

1. `7a9f365 Start Go server compatibility rewrite`
2. `534b20f Implement Go database synchronization`
3. `ee2042d Implement server administration and updater core`

Do not discard or absorb unrelated untracked files shown by `git status`. They predate or are outside the Go rewrite and belong to the user.

## Implemented

- Go module and `clipman-server`/compatibility commands.
- Compatible settings defaults, sparse-file behavior, path handling, stable output, network-safety checks, data-root locking, listener startup, and graceful shutdown.
- Health endpoint and bearer authentication boundary.
- Database ID validation, revisions, metadata, per-bucket locks, bounded streaming `HEAD`/`GET`, staged atomic conditional `PUT`, and backup hooks.
- Database inventory, JSON listing, guarded deletion, stale selection/pruning, and recoverable moves to `DeletedDatabases`.
- `.clpconf` and text connection files.
- Expiring and download-limited setup links, setup HTTP routes, redaction, and concurrency-safe consumption.
- Native-Go RSA private CA and leaf generation, renewal using the existing CA, inspection, fingerprints, TLS settings updates, and temporary public-CA sharing.
- Standalone `clipman-server-updater` command.
- Manifest v2 parsing and OS/architecture artifact selection, including same-version Python-to-Go migration selection.
- HTTPS-only bounded downloads, SHA-256 verification, archive path/symlink/count/expanded-size defenses, staged replacement, service coordination, health checking, and rollback.

## Verification completed

- `cd ClipmanServer && go test ./...`
- `cd ClipmanServer && go vet ./...`
- Windows builds of the server, updater, and real `clipman-cli`.
- Real CLI `init`, `put`, `list`, `sync`, and `status` against the Go HTTP server with Unicode dummy data.
- Go-to-Python-to-Go in-place database handoff with both entries preserved.
- Go-generated private CA and leaf certificate, `.clpconf` import, and real CLI synchronization over HTTPS without installing the CA into the operating-system trust store.
- Unit/HTTP coverage for administration, setup-link concurrency and limits, certificate renewal without CA replacement, malicious archive rejection, digest/architecture selection, same-version migration selection, and failed-health rollback with service restart.

## Important remaining work

Phase 2 is not fully release-gated. Complete runtime counters and health parity, raw malformed/conditional/`100-continue` differentials, concurrent first-writer and multi-bucket scenarios, backup characterization, race testing, TLS edge cases, 1 KiB through 64 MiB bounded-memory transfers, and the complete bidirectional Python/Go handoff matrix.

Phase 3 server-core behavior is implemented. Still expand raw differential coverage, RSA legacy-fixture verification, setup-link expiry/HEAD cases, certificate-share tests, and exact stable-output comparisons with Python.

Phase 4 updater security and transaction core is implemented. Concrete systemd, runit, externally managed Linux, Windows, and macOS service adapters and actual package-script manifest-v2 generation remain coupled to Phase 5. Add complete package-mode before/update/rollback tests and prove settings/data remain byte-for-byte unchanged on every failed update.

## Recommended next action

Continue Phase 5 from its first native-packaging slice. Windows now embeds/launches `clipman-server.exe`; macOS packages/launches a universal `clipman-server`; Docker is a Go multi-stage/Alpine runtime without Python or OpenSSL; and the transition bundle builds Linux amd64, arm64, and armv7 server/updater binaries with manifest v2. The Windows C# wrapper compiled locally and Go test/vet passed; Docker and macOS execution require their platform environments.

The Linux user/system helper migration now uses native settings queries and the native updater for check, install, and host changes. The Go updater accepts deployed helper options, performs HTTPS release discovery, supports native `.tar.gz` as well as transition `.zip` packages, and runs helper-controlled offline maintenance. Unit coverage includes tar extraction and wrapper settings queries.

Next, add exact platform-native archive generation/naming and full package-mode update/rollback tests, including byte-for-byte settings/data preservation. Then exercise Windows extraction/version/restart and macOS packaging on their actual platforms and build the multi-architecture containers.

Per user direction, the remaining build/package work is deferred until after the server release gates, and the user will handle Linux and Docker execution. Phase 6 has started. Completed gates include raw malformed/no-side-effect cases, Python-compatible health method behavior and conditional text, full health payloads after successful uploads, setup `HEAD` non-consumption, runtime counters, concurrent first-writer convergence, separate-bucket isolation, cancellation/staging cleanup, keyed-lock cleanup, a streaming 64 MiB transfer, and a live Windows CLI `init`/`put`/`list`/`get`/`sync`/`rm`/`status` run with Unicode dummy data.

Next, build reusable Python/Go raw differential scenarios, complete backup and TLS edge characterization, add slow-client/shutdown/endurance loops and fuzz seeds, run all compatibility modes and repeated handoff/rollback loops, then return to the deferred build/package matrices.

Before changing files, run:

```powershell
git switch go-server
git status --short
git log -3 --oneline
```

The authoritative design and phase exit criteria remain in `GO_SERVER_REWRITE_PLAN.md`; this file is the concise interruption/recovery checkpoint.
