# Go Server Rewrite Resume Status

Last updated: 2026-08-11 (Phase 7 historical migration simulations started; build matrices deferred)

Branch: `go-server`

## Resume point

The committed implementation is safe to resume from. The rewrite checkpoints through the first Phase 7 slice, oldest first, are:

1. `7a9f365 Start Go server compatibility rewrite`
2. `534b20f Implement Go database synchronization`
3. `ee2042d Implement server administration and updater core`
4. `c699eea Record Go server rewrite resume status`
5. `999bf03 Start native platform packaging`
6. `2c42330 Migrate Linux helpers to native updater`
7. `f6028cd Start server release compatibility gates`
8. `7d7f365 Prepare Python to Go bridge updates`

All tracked rewrite progress through the Phase 7 bridge preparation described below is committed. There are no pending tracked implementation edits at this checkpoint.

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
- Historical Python compatibility suite: 57 tests passed with three platform-specific skips on Windows after updating its Docker assertion for the native entrypoint.
- Phase 7 verification rerun: `go test ./...`, `go vet ./...`, Python bytecode compilation, and `git diff --check` passed.

## Important remaining work

Phase 2 is not fully release-gated. Complete runtime counters and health parity, raw malformed/conditional/`100-continue` differentials, concurrent first-writer and multi-bucket scenarios, backup characterization, race testing, TLS edge cases, 1 KiB through 64 MiB bounded-memory transfers, and the complete bidirectional Python/Go handoff matrix.

Phase 3 server-core behavior is implemented. Still expand raw differential coverage, RSA legacy-fixture verification, setup-link expiry/HEAD cases, certificate-share tests, and exact stable-output comparisons with Python.

Phase 4 updater security and transaction core is implemented. Concrete systemd, runit, externally managed Linux, Windows, and macOS service adapters and actual package-script manifest-v2 generation remain coupled to Phase 5. Add complete package-mode before/update/rollback tests and prove settings/data remain byte-for-byte unchanged on every failed update.

## Phase 7 bridge checkpoint

The legacy Python updater now understands `ClipmanServer-Linux-<architecture>-<version>.tar.gz` assets, validates their manifest-v2 artifact and SHA-256 digest, and safely rejects tar traversal, links, devices, excessive entry counts, and excessive expanded size. Its normal release check prefers the native asset, including a same-version migration from Python to Go, while retaining the transition ZIP fallback for a newer release.

Managed Linux migration installs the native core beside the retained Python files and atomically changes the launcher. Program-file snapshots include both the native core and launcher, so failed health checks can restore the prior Python launch path without rewriting settings or data. Unit coverage exercises native selection, transition fallback, archive validation, unsafe links, installation, and restoration. The historical Python suite now also asserts that the migrated Docker entrypoint invokes the native binary rather than Python.

No external release or asset publication has occurred.

The next Phase 7 slice fixed ordinary historical installations: after validating a native tar, the bridge now installs its native core directly instead of incorrectly looking for the legacy transition ZIP's shell installer. The generated launcher safely quotes paths containing spaces and apostrophes. Isolated tests model the old-updater transition-ZIP selection followed by the bridge updater's same-version native selection, a successful ordinary installation, and a failed native health check. Dummy settings, opaque database bytes, TLS authority material, connection files, service definitions, management helper, and retained Python fallback files are asserted byte-for-byte across the relevant paths.

Current verification is 60 historical Python tests passing with three Linux-only skips on Windows, plus `go test ./...`, `go vet ./...`, and `git diff --check`.

## Recommended next action

Continue Phase 7 by adding fixtures from additional supported historical releases and running compatibility `package` mode against an actual installed native artifact after a successful transition and after forced rollback. Add manual-recovery documentation before publishing anything. After bridge behavior is proven, proceed to Phase 8; return to native builds, platform wrappers, Linux/Docker execution, and package matrices at the end as directed.

Before changing files, run:

```powershell
git switch go-server
git status --short
git log -3 --oneline
```

Expected starting commit after this documentation checkpoint: the commit immediately following `7d7f365`. Untracked user files must remain untouched. `.test-tmp-phase7-cache/` is a disposable Go build-cache directory from verification; its attempted cleanup was interrupted by a slow filesystem operation and it is not part of the rewrite checkpoint.

The authoritative design and phase exit criteria remain in `GO_SERVER_REWRITE_PLAN.md`; this file is the concise interruption/recovery checkpoint.
