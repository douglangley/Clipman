# Clipman Server Go Compatibility Rewrite Plan

Status: In progress on branch `go-server`

Interruption/resume checkpoint: `GO_SERVER_REWRITE_STATUS.md`

Revision: 2026-08-10 — CLI-driven automated compatibility testing

Implementation checkpoint, 2026-08-10: phases 1 and the primary vertical slices for phases 2–4 are implemented. In addition to authenticated database synchronization, the Go server now provides database inventory/deletion/pruning, connection files, concurrency-safe expiring setup links, native-Go private-CA and server-certificate generation/renewal/inspection, temporary public-CA sharing, and private-CA HTTPS accepted by the real CLI without system trust installation. The Go updater now provides manifest-v2 OS/architecture selection, same-version Python-to-Go migration selection, HTTPS/digest/download limits, hostile-archive defenses, verified artifact selection, service-coordinated replacement, health checks, and rollback. Remaining release-gate work includes the deeper phase-2 differential/load matrix and phase-4 integration of concrete systemd, runit, Windows, and macOS service controllers with the phase-5 packages/wrappers.

Target: Rewrite the current Clipman Server 2.x implementation in Go without changing the client protocol, encrypted database format, settings, on-disk layout, or normal desktop user experience.

Primary outcome: Windows and macOS users can run Clipman Server without installing Python, Linux users receive a native static binary, and the container no longer needs a Python runtime or OpenSSL package for normal operation.

## 1. Scope and relationship to the other server plan

This document plans a compatibility rewrite of the server that exists today:

- bearer-token authentication;
- password-scoped opaque `.clipdb` database buckets;
- `HEAD`, `GET`, and conditional `PUT` synchronization;
- rolling file backups and stale-bucket maintenance;
- temporary setup links and `.clpconf` connection files;
- optional direct HTTPS with a private certificate authority;
- Windows tray and macOS status-menu wrappers;
- Linux user/system service installers;
- Docker packaging and self-update support.

The top-level `clipman-server.md` describes a different and much larger future server with accounts, passkeys, per-entry synchronization, SQLite, a web administration UI, and native service management. That plan also recommends Go, but it must not be silently combined with this rewrite. This rewrite is successful only if current Clipman clients continue working without a client migration or data migration.

Features from `clipman-server.md` may be built later on top of lessons from this work, but they are out of scope here unless separately approved.

## 2. Executive decision

Rewrite the shared Python server and Python updater in Go. Keep the existing C# Windows tray wrapper and Swift macOS status-menu wrapper during the first release series, changing them to launch a bundled Go executable instead of locating Python.

Use the Go standard library wherever practical. The initial production dependency budget is:

- `golang.org/x/sys` for reliable Windows and Unix file locking and any narrowly scoped native integration;
- no web framework;
- no database driver;
- no cross-platform tray framework;
- no external OpenSSL process for certificate generation, inspection, or fingerprinting;
- no CGO.

The first Go release is a behavior-preserving replacement, not an opportunity to redesign the HTTP API, settings schema, backup policy, or desktop administration UX.

Use the working `ClipmanCli` executable as the primary released-client simulator. Successful synchronization behavior is proved by driving the same CLI command sequences against the Python and Go servers with deterministic synthetic histories. A smaller raw-protocol driver covers malformed requests and server-only administration behavior that a correct client cannot or should not generate. This replaces most per-client manual testing and avoids mechanically duplicating every Python test as a Go test.

### 2.1 Working compatibility decisions

Auditing the plan against the shipping server surfaced four cases where "preserve current behavior" is defective, unreachable, or internally inconsistent. To keep implementation moving without repeated product input, this plan adopts the following working decisions. They may be changed before the dependent phase begins, but implementation and tests otherwise proceed with these defaults.

| # | Decision | Section | Working resolution |
|---:|---|---|---|
| 1 | Backups inherit the database's mtime, so any backup of a bucket idle longer than `BackupRetentionHours` is deleted at creation. | 12.6.1 | Fix in Go and record a narrow intentional divergence; also fix Python during the bridge if doing so does not delay it |
| 2 | The Python listener is IPv4-only despite IPv6 support throughout the surrounding code. | 12.1 | Enable IPv6 in Go and cover it with Go-only tests |
| 3 | `GET /api/v1/backups` exposes metadata for every bucket to any token holder. | 18.1 | Add optional bucket scoping while preserving the legacy unscoped response; defer mandatory scoping to the future-server plan |
| 4 | Served `.clpconf` files are LF, while Windows on-disk copies are CRLF. | 6.4 | Use LF for all newly generated `.clpconf` files, matching the form clients already download and accept |

Two further corrections are not decisions but factual fixes to this document, recorded here because an earlier draft stated them incorrectly: the exit-code table in section 8.4, and the claim in section 12.12 that the updater already performs symlink-safe extraction.

## 3. Current implementation baseline

The production delivery surface currently includes approximately:

| Area | Size | Responsibility |
|---|---:|---|
| `ClipmanServerLinux/clipman_server.py` | 2,045 lines | Configuration, HTTP/TLS, setup links, certificates, blob storage, backups, pruning, logging, and CLI |
| `ClipmanServerLinux/clipman_server_updater.py` | 610 lines | GitHub release discovery, verified downloads, safe extraction, service-aware updates, health checks, and rollback |
| `ClipmanServerWindows/Program.cs` | 1,351 lines | Tray UI, process supervision, startup, utilities, and Windows self-update |
| `ClipmanServerMac/Sources/ClipmanServer/main.swift` | 856 lines | Status-menu UI, process supervision, login startup, utilities, and macOS self-update |
| Linux installers and helpers | about 1,044 lines | User/system service installation and management for systemd and runit |
| Docker packaging | about 128 lines | Python image, OpenSSL installation, environment mapping, and launch |
| Certificate/trust tools | about 1,274 lines | Cross-platform certificate generation, validation, distribution, and testing |

The Python core uses only the standard library, which makes a direct Go port practical. The server treats every `.clipdb` as an opaque encrypted blob; it does not parse or migrate client history data.

The current Python test suite is a *partial* behavioral specification. Verified on Windows on 2026-08-10, 49 tests pass and three Linux-only installer tests skip (52 defined in total). The suite covers settings load/save, data-root locking, setup links, connection files, and certificates well. It does **not** cover backups, revision-token format, conditional `PUT` beyond the first-writer race, database-ID validation, health JSON shape, metadata touch throttling, or the database maintenance CLI.

The working Go CLI materially changes the cost of filling that gap. Its full `go test ./...` suite passes, and the executable already implements real health checks, authenticated `HEAD`/`GET`/conditional `PUT`, bucket derivation, encrypted `.clipdb` validation, private-CA connection-file import, conflict retry, entry creation, templates, duplicate modes, tombstones, and structured JSON output. It can therefore provide most successful-path, upgrade, rollback, dummy-data, and concurrency acceptance coverage. It is not a substitute for raw protocol probes, server administration tests, updater tests, or package/wrapper tests; section 16 assigns each surface to the cheapest suitable automated layer.

## 4. Goals

1. Remove the Python runtime requirement on Windows, macOS, Linux, and Docker.
2. Remove OpenSSL as a runtime requirement for normal certificate operations.
3. Preserve the current HTTP protocol so every released Clipman client continues to work unchanged.
4. Preserve all existing settings keys, default paths, data directories, database buckets, metadata, backups, TLS material, connection files, and setup-link state.
5. Preserve wrapper UX: tray/status menu, restart behavior, setup-link actions, certificate actions, update controls, startup controls, logs, and notifications.
6. Use streaming I/O and explicit resource limits so large transfers do not require one or more full database copies in memory.
7. Improve request timeouts, bounded concurrency, cancellation, and graceful shutdown without changing successful client behavior.
8. Ship native binaries for supported operating systems and CPU architectures.
9. Retain verified, rollback-capable updates and provide a safe path from installed Python servers.
10. Establish CLI-driven acceptance and focused differential tests that can prove the Python and Go implementations behave equivalently without requiring repeated manual client input.
11. Make builds reproducible, race-tested, signed where applicable, and suitable for automated release packaging.

## 5. Non-goals

The compatibility rewrite will not add:

- user accounts, email login, passkeys, OAuth, or multi-tenant administration;
- server-side clipboard decryption;
- per-entry server storage or a new sync protocol;
- SQLite or another server database;
- PostgreSQL, clustering, horizontal scaling, or object storage;
- a web administration application beyond the existing temporary setup page;
- server-side search, indexing, or history inspection;
- a Go tray/status-menu framework;
- automatic Windows Service Control Manager installation in the first release;
- a privileged macOS `LaunchDaemon` in the first release;
- changes to the `.clipdb` codec or password-derived bucket identity;
- API cleanup that would remove or repurpose existing endpoints;
- a combined implementation of the future backend described in `clipman-server.md`.

## 6. Compatibility policy

Compatibility is divided into three levels.

### 6.1 Must remain byte- or behavior-compatible

- Existing settings JSON remains readable without rewriting an unchanged file.
- Unknown settings keys survive any explicit settings update.
- Existing database paths and bucket directories are reused in place.
- Existing TLS CA, leaf certificate, and private-key files remain usable.
- Renewing a leaf certificate reuses the existing CA unless `--new-ca` is explicitly supplied.
- Existing `.clpconf` files remain accepted by clients.
- Newly generated `.clpconf` files retain schema version 1 and current field names.
- Database IDs, revision preconditions, status codes, response headers, and error meanings remain compatible.
- Existing backups and metadata remain discoverable.
- CLI output parsed by the Windows and macOS wrappers retains stable prefixes.
- Existing exit codes retain their meanings.

Byte-level serialization details that this level depends on are specified separately in section 6.4. They are not incidental formatting; Go's defaults differ from Python's on every one of them.

### 6.2 Must remain semantically compatible

Formatting may differ only where no client or wrapper consumes it, such as:

- HTTP `Date` and implementation-identifying `Server` headers;
- whitespace in human-readable diagnostics;
- internal log timestamps;
- certificate subject display formatting when the underlying subject is unchanged;
- ordering of maps inside diagnostic JSON if consumers treat them as JSON objects.

Any allowed difference must be explicitly listed in the differential-test normalizer. The test harness must not broadly ignore response headers or bodies.

### 6.3 Intentional internal improvements

The Go server should deliberately improve:

- streaming and bounded buffering;
- header, body, idle, and shutdown timeouts;
- authenticated upload concurrency limits;
- cancellation of maintenance workers;
- constant-time comparisons for bearer tokens and setup-code hashes;
- file-descriptor cleanup and error propagation;
- symlink/reparse-point defenses where the current implementation is weaker;
- updater extraction validation on every platform;
- Windows file ACLs for newly created secrets where this can be done without breaking existing installations.

These improvements must not cause ordinary released clients to fail.

### 6.4 Byte-level file encoding and JSON serialization

Go's standard `encoding/json` and file-writing defaults differ from Python's on every point below. Each one changes bytes on disk, so each needs a deliberate decision recorded in the differential-test normalizer.

| Aspect | Current Python behavior | Go default | Required action |
|---|---|---|---|
| Line endings | Files written through text mode (`json.dump` to an opened stream, `Path.write_text`) get **CRLF on Windows, LF on Linux/macOS** | LF everywhere | Match existing per-file behavior, except use LF for all newly generated `.clpconf` files per section 2.1 |
| Trailing newline | `clipman-server-settings.json` has **none**; `.clpconf`, setup-link state, and the connection `.txt` end with one; database metadata has none | none unless written | Match per file exactly |
| Non-ASCII escaping | `ensure_ascii=True` — `münchen.local` is stored as `m\u00fcnchen.local` | raw UTF-8 bytes | Escape non-ASCII to match |
| HTML-unsafe characters | not escaped — `a<b>&c` is stored literally | escaped to `a\u003cb\u003e\u0026c` unless `SetEscapeHTML(false)` | Call `SetEscapeHTML(false)` |
| Key order and indent | `sort_keys=True, indent=2` | struct order, configurable indent | Sort keys; indent 2 |
| Integer rendering | Python ints are exact | `float64` round-trip through `interface{}` can render `1e+06` | Use `json.Number` or typed fields when preserving unknown keys |

One live inconsistency is deliberately not copied: the `.clpconf` **served** over `/setup/<code>/connection.clpconf` is built by `connection_config_bytes()` and is **always LF**, while the **on-disk** `.clpconf` written by `write_connection_config()` goes through text mode and is **CRLF on Windows**. Per section 2.1, Go uses LF for both, matching the served form that clients already download and accept. Record only the Windows on-disk case as an intentional divergence.

## 7. Protocol contract to preserve

### 7.1 Routes

| Method | Route | Authentication | Required behavior |
|---|---|---|---|
| `GET` | `/api/v1/health` | None | Return status, version, listener, TLS, backup, and runtime statistics JSON |
| `HEAD` | `/api/v1/database/{database-id}` | Bearer token | Return existence, `Content-Length`, `ETag`, and `X-Clipman-Revision` without a body |
| `GET` | `/api/v1/database/{database-id}` | Bearer token | Return the opaque database bytes with length and revision headers |
| `PUT` | `/api/v1/database/{database-id}` | Bearer token | Conditionally create or replace one opaque database blob |
| `GET`/`HEAD` | `/setup/{code}` | Temporary code in path | Return the accessible setup page; `HEAD` has no body |
| `GET`/`HEAD` | `/setup/{code}/connection.clpconf` | Temporary code in path | Return the importable connection file; only a successful `GET` consumes a download |
| `POST` | `/api/v1/backup` | Bearer token | Continue returning the current database-scoping error |
| `GET` | `/api/v1/backups` | Bearer token | Preserve current backup-list response behavior |
| `POST` | `/api/v1/restore?name=...` | Bearer token | Continue returning the current database-scoping error |

Unknown routes return the current `404` behavior. A malformed or expired setup URL must return the same generic response as an unknown, revoked, or exhausted setup URL.

Route-matching details that are easy to lose in a port:

- **Trailing slashes are stripped, not merely tolerated.** `Handler.route()` applies `path.rstrip("/") or "/"`, so *any* number of trailing slashes is removed before matching, including on the database route. Go's `ServeMux` does not behave this way; parse and normalize deliberately.
- **Only `GET /api/v1/health` is unauthenticated.** `HEAD /api/v1/health` fails the `command == "GET"` guard, falls through to the authorization check, and returns `401` unauthenticated or `404` authenticated. This is unlikely to be deliberate, but it is current behavior and monitoring tools may depend on the status code.
- **`GET /api/v1/backups` is not scoped to the caller's bucket.** It globs `Databases/*/ServerBackups/*.clipdb` and returns backup names, sizes, revisions, and timestamps for **every** bucket on the server. Because all clients share one bearer token while holding per-password buckets, any authenticated client can enumerate metadata about other users' backups. Preserving this is a decision, not a default; see section 18.1.
- Failed probes must not create bucket directories. `HEAD`, `GET`, and conditional `PUT` against a nonexistent database all return before `touch_database` runs, so an attacker with a valid token cannot inflate the data root by probing IDs. Keep that ordering.

### 7.2 Authentication

- The accepted header is exactly `Authorization: Bearer <AuthToken>` after the current header trimming behavior.
- An empty configured token never authorizes a request.
- Unauthorized API requests return `401` with the current plain-text body.
- Setup URLs and the health endpoint remain the only unauthenticated routes.
- Logs never include the bearer token, database ID, or temporary setup code.

### 7.3 Database IDs

- IDs are URL-decoded from the database route.
- IDs issued by current Clipman clients are 32 through 128 ASCII characters.
- Client-issued IDs use ASCII letters/digits plus `-` and `_`.
- Invalid IDs do not resolve to a filesystem path and must fall through to `404`.
- No path separator, encoded separator, dot segment, Unicode confusable, or alternate data stream may escape the bucket root.

The Python implementation currently uses Unicode-aware `str.isalnum`, so it accidentally accepts some non-ASCII letters and digits that no Clipman client generates. Current Windows, macOS, iOS, Android, Linux-backend, and CLI generators all produce unpadded URL-safe base64 over a 32-byte HMAC, so their IDs are 43 ASCII characters. The working Go contract is the narrower ASCII grammar. Phase 0 still characterizes Python's broader acceptance and locks the supported generator vectors into the manifest; accepting any non-ASCII ID is an intentional Python-only divergence, not a client requirement.

### 7.4 Revisions

The current revision is:

1. file size formatted as lowercase hexadecimal;
2. a hyphen;
3. modification time in nanoseconds formatted as lowercase hexadecimal;
4. URL-safe base64 without padding over that ASCII string.

Go must reproduce this format where the operating system exposes equivalent timestamp precision. Fixtures must cover Windows, macOS, and Linux timestamp behavior.

If a platform cannot reproduce an existing revision token after an in-place upgrade, the accepted fallback is one harmless conflict/refetch cycle. It is not acceptable to acknowledge a write against the wrong revision.

### 7.5 Conditional uploads

The `PUT` contract is critical:

- A valid, nonnegative `Content-Length` is required.
- Chunked requests without a known content length remain unsupported.
- The configured `MaxDatabaseBytes` is enforced before accepting the body, and an oversize declaration returns **`413`** with the limit in the plain-text body.
- A short request body returns `400`.
- `If-None-Match` is accepted only when its value is `*`.
- `If-Match` and `If-None-Match` cannot be combined.
- `If-None-Match: *` returns `412` if the bucket already exists and includes the current revision.
- A mismatched `If-Match` returns `409` and includes the current revision.
- Conditional headers are parsed *after* the body has been read, so a malformed `If-None-Match` still consumes the upload. Go will naturally validate first; this is an improvement, but it changes when the client learns of the rejection.
- Identical bytes do not replace the database or create a backup, and record metadata event `"head"` rather than `"write"` — `LastWrittenUnixMs` is deliberately not advanced. Preserve this; staleness pruning depends on it.
- A changed database creates a backup according to current policy before the atomic replacement.
- Successful responses return the new revision in `X-Clipman-Revision` and the current status JSON body.

`Expect: 100-continue` needs explicit attention rather than a line item. Python's `BaseHTTPRequestHandler` sends `100 Continue` **eagerly**, before `do_PUT` runs, so an oversize upload is fully transmitted and only then rejected with `413`. Go's `net/http` defers the interim response until the handler first reads the body, so a Go server that checks `Content-Length` first replies `413` without the client ever sending the payload. That is strictly better and released clients handle it, but it is a visible protocol difference that must be listed as an intentional divergence rather than surfacing as a differential-test failure.

Related: the current `413` and early-`400` paths return **without draining the request body**. On a keep-alive HTTP/1.1 connection the unread bytes are then parsed as the next request line, breaking the connection. Go's `net/http` drains a bounded amount and otherwise closes cleanly. Treat the Go behavior as the target and add a slow-client test that confirms released clients recover.

### 7.6 Setup-link contract

- Codes contain at least 32 URL-safe characters and are generated from cryptographically secure randomness.
- State stores only a SHA-256 code hash, creation time, expiration time, remaining download count, and schema version.
- State files never store the clear setup code or history password.
- Code comparison is constant-time.
- Expired and exhausted state is removed.
- `HEAD` never consumes a download.
- Loading the HTML page never consumes a download.
- A successful `GET` of the `.clpconf` file consumes exactly one download.
- Only one of two concurrent final downloads succeeds.
- Public HTTP setup URLs remain prohibited; public addresses require HTTPS.
- The existing security headers, no-cache policy, accessible content, platform suggestions, and lack of external resources are preserved.

### 7.7 Health and runtime statistics

Preserve these top-level health keys:

- `Status`;
- `Version`;
- `Machine`;
- `DatabaseRevision`;
- `DatabaseLength`;
- `DatabaseModifiedUnixMs`;
- `ListenPrefix`;
- `TlsEnabled`;
- `TlsCertificateExpires`;
- `BackupRetentionHours`;
- `MaxBackups`;
- `Runtime`.

Preserve runtime counters and their current capitalization. Counter accuracy under concurrency should improve, but the presence or shape of fields must not change during the compatibility release.

Four of these fields are not what their names suggest, and a "clean" Go implementation would change all four:

- **`DatabaseRevision`, `DatabaseLength`, and `DatabaseModifiedUnixMs` are hardcoded stubs** — always `""`, `0`, and `0`. They predate per-bucket storage and describe a single-database server that no longer exists. Go must keep emitting the constants. Populating them is a protocol change requiring its own review.
- **`Machine` is empty on Windows.** It is `os.uname().nodename`, guarded by `hasattr(os, "uname")`, which is false on Windows. Go's `os.Hostname()` returns a real name on every platform, so a naive port would leak the server hostname into an endpoint that is deliberately unauthenticated. Preserve the current platform-conditional behavior: emit an empty string on Windows and the current hostname form on Unix-like systems.
- **`TlsCertificateExpires` is a raw OpenSSL `notAfter` string** (for example `Aug  5 12:00:00 2027 GMT`, with the two-space day padding). It is computed **once at startup**, never refreshed, and reaches `status()` through a private `_TlsCertificateExpires` pseudo-key injected into the live settings dictionary. Go must reproduce the string format, compute it at the same lifecycle point, and must never persist that pseudo-key to the settings file. See section 9.2.

The runtime `Clients` map grows without bound — one entry per unique client IP for the process lifetime. Only the derived `UniqueClients` count is exposed, so Go may replace it with a bounded structure provided the reported count stays accurate enough for its purpose. State the chosen bound and its accuracy characteristics in the release notes.

## 8. CLI and process contract

### 8.1 Existing flags

The Go binary must initially accept the current command line:

- `--version`;
- `--config`;
- `--host`;
- `--advertise-host`;
- `--port`;
- `--suggest-port`;
- `--database`;
- `--log`;
- `--cert-file`;
- `--key-file`;
- `--allow-insecure-remote`;
- `--create-tls-certificate`;
- repeated `--cert-host` and `--cert-ip`;
- `--new-ca`;
- `--show-ca-fingerprint`;
- `--share-ca`, `--share-minutes`, and `--share-host`;
- `--show-token`;
- `--write-connection-info`;
- `--create-setup-link`;
- `--revoke-setup-link`;
- `--setup-minutes`;
- `--setup-downloads`;
- `--setup-base-url`;
- `--list-databases`;
- `--list-databases-json`;
- `--delete-database`;
- `--prune-databases-days`;
- `--confirm`;
- `--force-recent`.

New structured subcommands may be added later, but wrappers and installers should move to them only after the compatibility flags are proven.

### 8.2 Legacy Linux updater interface

The bridge implementation must continue accepting the arguments currently passed by installed Linux helpers until those helpers have migrated:

- exactly one of `--check`, `--install`, or `--set-host <address>`;
- `--yes` for noninteractive installation;
- `--advertise-host` during host changes;
- `--current-version`;
- `--app-dir`;
- `--bin-dir`;
- `--config`;
- `--service-file`;
- `--init-system` with `systemd`, `runit`, or `none`;
- `--helper-path`;
- `--launcher-path`;
- `--managed-program-only`;
- the test-only release API override.

These may be implemented as hidden compatibility options on the main Go executable or through a small Go updater command. They cannot disappear until installed helper scripts from every supported prior release have a working migration route.

### 8.3 Stable wrapper-consumed output

The following output is an API and must remain stable:

- `--suggest-port` prints only an integer on stdout.
- `--show-token` prints only the token on stdout.
- `--show-ca-fingerprint` prints only the fingerprint on success.
- setup-link creation includes a line beginning `Setup URL: `.
- certificate sharing emits its URL, fingerprint, and lifetime as its first three complete stdout lines.
- connection-file creation prints the resulting `.clpconf` path where the current wrappers expect it.
- `--version` prints the exact server version without decoration.

Human diagnostics go to stderr when an operation fails.

The certificate-sharing lines carry an additional requirement that is invisible in the text above: `--share-ca` is a **long-running** process, and the Windows wrapper calls `StandardOutput.ReadLine()` three times and blocks on the result before the process exits. The Python implementation passes `flush=True` on each of those three prints for exactly this reason. A Go implementation must flush or line-buffer those three lines before entering its wait loop, or the tray dialog hangs until the share times out.

### 8.4 Exit codes

An earlier draft of this plan described code `2` as covering refused unsafe operations. That is **wrong**, and porting to it would break every wrapper and installer that branches on exit status. The verified behavior is:

| Code | Meaning | Source |
|---:|---|---|
| `0` | Success | normal `return 0` |
| `1` | Runtime failure **and every refused unsafe operation** | `raise SystemExit("<message>")` — Python prints the string to stderr and exits `1` |
| `2` | Argparse rejection, and exactly two explicit `return 2` paths: out-of-range `--port` and an invalid `--setup-base-url` | `argparse` / explicit returns |
| `20` | Listener bind failure | `BIND_ERROR_EXIT_CODE` |
| `21` | Data-root lock failure | `DATA_ROOT_LOCK_EXIT_CODE` |

Everything that reads like "code 2 territory" is actually code `1`: `--delete-database` without `--confirm`, `--prune-databases-days` without a positive value, `--share-minutes` outside 1–60, the insecure-remote listener refusal, and every TLS-configuration refusal. Measured directly against the shipping script:

```text
--delete-database <id>   (no --confirm)   exit=1
--share-minutes 999                       exit=1
--port 99999                              exit=2
--bogus-flag                              exit=2
```

Go must reproduce this table exactly, including the counter-intuitive rows. Fixture-test every listed path. Additional stable exit codes require a documented compatibility review.

### 8.5 Shutdown

- `SIGINT` and `SIGTERM` initiate graceful shutdown on Unix.
- Windows console control and wrapper termination are handled predictably.
- The listener stops accepting requests immediately.
- Active requests receive a bounded grace period.
- Maintenance workers stop through context cancellation rather than sleeping indefinitely.
- The data-root lock is released by normal shutdown and automatically by the operating system after process death.
- A final runtime summary is logged once.

## 9. Settings contract

### 9.1 Existing keys and defaults

Preserve all current keys:

| Key | Default or rule |
|---|---|
| `Host` | `127.0.0.1` |
| `AdvertiseHost` | empty; fall back to `Host` |
| `Port` | cryptographically random available port from 20000 through 49151 |
| `DatabasePath` | platform-specific data path ending in `clipman-history.clipdb` |
| `AuthToken` | 32 random bytes encoded as unpadded URL-safe base64 |
| `LogPath` | platform-specific rotating log path |
| `CertFile` | empty |
| `KeyFile` | empty |
| `CaFile` | empty |
| `AllowInsecureRemote` | `false` |
| `SetupBaseUrl` | empty |
| `BackupIntervalMinutes` | `60` |
| `BackupRetentionHours` | `24` |
| `MaxBackups` | `48` |
| `CreateBackupBeforeEveryUpload` | `true` |
| `DatabasePruneDays` | `0` |
| `DatabasePruneIntervalHours` | `24` |
| `MaxDatabaseBytes` | 64 MiB |

### 9.2 Load and save rules

- A missing settings file is created with all defaults.
- A sparse existing settings file receives defaults in memory but is not rewritten merely because the server started.
- Plain startup must preserve existing settings bytes and modification time.
- Explicit CLI overrides update the settings file.
- Unknown JSON properties survive a rewrite.
- UTF-8 with or without BOM remains readable.
- Saved JSON uses stable indentation and key ordering to minimize unnecessary diffs, following the exact serialization rules in section 6.4.
- Writes use a private temporary file, flush it, atomically replace the target, and preserve the existing owner where supported.
- Settings and generated connection files receive mode `0600`; their directories receive `0700` on Unix.
- Windows uses a current-user-only DACL for newly created secret-bearing files where safely implementable.
- **Runtime-only keys must never be persisted.** `run_server` injects `_TlsCertificateExpires` into the live settings dictionary to carry the certificate expiry into `status()`. It survives only because the settings-mutation comparison happens earlier in `main()`. A Go implementation that keeps a single settings object and saves it later would write this pseudo-key into the user's file. Keep computed runtime state out of the persisted structure entirely rather than relying on ordering.

The no-rewrite guarantee applies to **settings only**. Two related behaviors are easy to mistake for it:

- **Connection files are reconsidered on every ordinary startup** whenever they already exist. `maybe_write_connection_info` and `maybe_write_connection_config` regenerate `clipman-server-connection.txt` and `.clpconf` if the target — or the legacy `.txt` — is present. Go preserves that refresh decision but writes atomically only when the newly generated bytes differ. This retains current values while avoiding needless mtime and flash writes; the differential driver compares content and records the unchanged-file mtime improvement narrowly.
- **Logging is configured after the connection files are written.** `configure_logging` runs after both `maybe_write_*` calls, so the "existing connection information was preserved" warning never reaches the log file — it goes to the default root handler on stderr. A Go implementation will almost certainly initialize logging first, which *adds* entries to the log. That is an improvement; list it as an intentional divergence so the log-comparison arm of the differential harness does not flag it.

Use a typed configuration structure for validation plus a raw-property map for unknown-key preservation. Do not decode and immediately re-encode on ordinary startup.

### 9.3 Configuration precedence

For native execution:

1. built-in defaults;
2. settings file;
3. explicit CLI overrides.

For containers:

1. built-in defaults;
2. settings file in the data volume;
3. supported `CLIPMAN_*` environment variables;
4. explicit CLI overrides.

Environment support belongs in the Go configuration package so the production container does not require a shell entrypoint.

### 9.4 Default platform paths

The Go path package must reproduce the paths actually used by each launch mode:

| Platform/mode | Settings | Data | Log |
|---|---|---|---|
| Windows tray | `%LOCALAPPDATA%\Clipman Server\clipman-server-settings.json` | `%LOCALAPPDATA%\Clipman Server` | `%LOCALAPPDATA%\Clipman Server\logs\clipman-server.log` |
| macOS status app | `~/Library/Application Support/Clipman Server/clipman-server-settings.json` | `~/Library/Application Support/Clipman Server` | `~/Library/Logs/Clipman Server/logs/clipman-server.log` |
| Installed Linux | `~/.config/clipman-server/clipman-server-settings.json` or configured XDG equivalent | `${XDG_DATA_HOME:-~/.local/share}/clipman-server` | `${XDG_STATE_HOME:-~/.local/state}/clipman-server/logs/clipman-server.log` |
| Direct package run | `<current-directory>/Settings/clipman-server-settings.json` unless `--config` is supplied | platform default | platform default |
| Container | `${CLIPMAN_DATA_DIR:-/data}/clipman-server-settings.json` unless overridden | `${CLIPMAN_DATA_DIR:-/data}` | `${CLIPMAN_DATA_DIR:-/data}/logs/clipman-server.log` unless overridden |

Tests must cover XDG overrides, missing Windows known folders, home-directory fallbacks, path separators, spaces, and non-ASCII user names.

## 10. On-disk data contract

The Go server must reuse the existing layout:

```text
<data-root>/
  .clipman-server.lock
  Databases/
    <database-id>/
      clipman-history.clipdb
      clipman-server-metadata.json
      ServerBackups/
        clipman-history-YYYYMMDD-HHMMSS-nnnnnnnnn.clipdb
  DeletedDatabases/
    <database-id>-YYYYMMDD-HHMMSS/
```

Settings-directory files remain:

```text
clipman-server-settings.json
clipman-server-connection.txt
clipman-server-connection.clpconf
clipman-server-setup-link.json
tls/
  clipman-server-ca.key
  clipman-server-ca.crt
  clipman-server.key
  clipman-server.crt
  clipman-server-fullchain.crt
```

Metadata field names, event values, timestamp units, and the five-minute touch-throttling behavior remain compatible.

Moving a database to `DeletedDatabases` remains recoverable. Automatic pruning must never permanently delete a database bucket.

Two details of this layout are easy to get wrong:

- **The `YYYYMMDD-HHMMSS` stamps in backup and `DeletedDatabases` names are local time, not UTC.** Both use `time.strftime(..., time.localtime())` while every timestamp *inside* metadata and JSON payloads is Unix milliseconds. Go's `time.Now()` formatting must stay local to keep names comparable with existing files. Note the DST consequence: names are not monotonic across a backward clock shift. Backups tolerate this because of the nine-digit nanosecond suffix; `DeletedDatabases` relies on an explicit `-2`, `-3` collision counter, which must be ported.
- **`DatabasePath` names a file that is never created or read.** Only its parent directory is used, as the anchor for `Databases/`, `DeletedDatabases/`, and `.clipman-server.lock`. The setting is a legacy artifact of the single-database server. Keep the setting and keep deriving paths from its parent; do not "fix" it into a real path, and do not create the file.

Also note that `resolved_data_root` (used for the lock) resolves symlinks while `database_root` and `deleted_database_root` do not. If `DatabasePath` traverses a symlink, the lock file and the bucket tree can land in different directories. Go should resolve consistently and add a test for a symlinked data root.

## 11. Proposed Go module layout

Put the implementation inside the existing top-level `ClipmanServer` directory so documentation, versioning, settings examples, and implementation have one owner:

```text
ClipmanServer/
  go.mod
  go.sum
  version.txt
  Manual.html
  clipman-server-settings.example.jsonc
  cmd/
    clipman-server/
      main.go
    clipman-server-compat/
      main.go
  internal/
    app/
      run.go
      signals.go
    buildinfo/
      version.go
    config/
      config.go
      defaults.go
      paths.go
      save.go
    dataroot/
      lock.go
      lock_unix.go
      lock_windows.go
    blobstore/
      store.go
      revision.go
      upload.go
      download.go
      metadata.go
    backup/
      backup.go
      prune.go
    maintenance/
      databases.go
      scheduler.go
    connection/
      document.go
      files.go
    onboarding/
      setup.go
      page.go
      page.html
    certificates/
      generate.go
      inspect.go
      share.go
      pem.go
    httpapi/
      server.go
      routes.go
      auth.go
      database.go
      setup.go
      responses.go
      limits.go
    logging/
      logging.go
      rotate.go
      redact.go
    metrics/
      runtime.go
    update/
      releases.go
      download.go
      extract.go
      install.go
      health.go
      rollback.go
    platform/
      files_unix.go
      files_windows.go
      process_unix.go
      process_windows.go
    compat/
      process.go
      normalize.go
      protocol.go
      filesystem.go
      cli.go
  compat/
    README.md
    corpus/
    snapshots/
```

Package boundaries should remain narrow:

- `httpapi` depends on interfaces exposed by `blobstore`, `onboarding`, and `metrics`.
- `blobstore` does not know about HTTP.
- `certificates` does not know about wrappers.
- `update` does not import HTTP handlers; it uses a small health-client package or local function.
- platform-specific code is isolated with build tags.
- no package imports the Clipman client database codec.
- `internal/compat` and `cmd/clipman-server-compat` are test tooling only and are never included in production server packages.
- The compatibility driver invokes a built `clipman-cli` as a subprocess. It does not import `ClipmanCli/internal/...`, so the acceptance test exercises the same executable and configuration path users receive.

Version injection uses `ClipmanServer/version.txt` as the single release source. Release builds pass the version through `-ldflags`; development builds have an explicit, test-visible fallback. CI fails if the embedded version, package manifest, wrapper version, and `version.txt` disagree.

## 12. Detailed implementation design

### 12.1 HTTP server

Use `net/http` with an explicit `http.Server` configured with:

- `ReadHeaderTimeout`;
- a bounded `ReadTimeout` appropriate for a 64 MiB upload on a slow private network;
- `WriteTimeout` chosen so legitimate database downloads complete;
- `IdleTimeout`;
- `MaxHeaderBytes`;
- explicit TLS minimum version 1.2;
- graceful `Shutdown` with a fixed deadline;
- panic recovery that logs a redacted request identity and returns `500` only if headers are not committed.

Do not rely on the default `ServeMux` path cleaning if it changes encoded-path semantics. Parse and validate the escaped path deliberately, with tests for `%2f`, `%5c`, repeated slashes, dot segments, invalid UTF-8, and trailing slashes.

Two listener-level properties change simply by moving to Go; their working resolutions are fixed here and recorded as intentional divergences.

**The current server is IPv4-only.** `ThreadingServer` inherits `address_family = socket.AF_INET` from `socketserver.TCPServer` and never overrides it, so binding an IPv6 address fails outright:

```text
ThreadingServer.address_family = 2   (AF_INET)
bind ::1 -> gaierror: getaddrinfo failed
```

This is at odds with the rest of the codebase, which formats bracketed IPv6 URLs, classifies `::` as a wildcard, puts `::1` in generated certificate SANs, and handles `[::]` in the container entrypoint. All of that IPv6 support is unreachable today. Go enables IPv6 listeners as a **new capability**, not a differential match. Cover it with Go-only tests, including `validate_network_security` classification of IPv6 ranges and explicit single-stack/dual-stack behavior on each target OS.

**`allow_reuse_address` is `True`.** On Unix this is ordinary `SO_REUSEADDR`. On Windows it permits a second process to bind a port already in use and silently steal connections. Go deliberately does **not** set `SO_REUSEADDR` on Windows listeners, so a Go server will fail to bind — exit `20` — in cases where Python succeeded. The data-root lock catches the common case of two instances sharing a data root, but not two instances with different data roots on the same port. The Go behavior is correct and should be kept; list it as an intentional divergence and confirm no installer or wrapper depends on the old permissiveness during restart.

### 12.2 Concurrency model

- One process holds one exclusive data-root lock.
- Requests for different database IDs may proceed concurrently.
- State-changing operations for the same database ID are serialized.
- A reference-counted keyed lock table removes unused database locks so attacker-selected IDs cannot grow an unbounded map.
- Setup-state reads and download consumption use one in-process mutex plus atomic state replacement.
- A global upload semaphore limits concurrent staging writes and aggregate resource use.
- Maintenance operations take the same per-database or data-root coordination paths as requests.
- Runtime counters use atomics or a small lock and are race-test clean.

### 12.3 Streaming upload algorithm

The implementation must not hold the full request body and current database in memory.

1. Authenticate and validate the route, `Content-Length`, size limit, and conditional headers.
2. Reserve an upload slot.
3. Stream exactly `Content-Length` bytes into a uniquely named private staging file under the data root using a bounded buffer.
4. Reject a short body, unexpected extra bytes where detectable, write error, cancellation, or size violation; remove the staging file.
5. Flush and close the staging file before mutation.
6. Acquire the database-specific lock.
7. Recompute the current revision and evaluate `If-None-Match` or `If-Match` at commit time.
8. Compare the staged file with the current file using size plus chunked comparison. Do not rely on hashes alone for equality.
9. If identical, remove the stage, update metadata with the current compatibility event, and retain the current revision.
10. If changed, create the configured backup before replacement.
11. Atomically replace the database, apply private permissions, update metadata, and calculate the revision from the committed file.
12. Release the keyed lock and upload slot.
13. Return the current success JSON and revision header.

Every error path must remove its own staging file. Startup may remove only clearly identified stale upload files older than a conservative threshold and only after acquiring the data-root lock.

### 12.4 Streaming download algorithm

The response revision, length, and bytes must refer to one consistent file instance.

1. Acquire the database lock.
2. Open the database and stat the open handle.
3. Record the revision and length from that handle.
4. Update metadata according to the current touch-throttling rule.
5. Release the lock only when platform semantics guarantee that atomic replacement can coexist with an open reader.
6. Stream through a bounded buffer and close the handle on completion or cancellation.

Add a cross-platform test that keeps a download handle open while a writer replaces the file. If Windows sharing semantics prevent safe replacement, use an immutable snapshot or hold only the minimum required lock without allowing a slow client to block all writes indefinitely.

`HEAD` never reads the database contents.

### 12.5 Atomic files and durability

Centralize atomic replacement rather than duplicating temp-file logic.

The helper must:

- create the temporary file in the destination directory;
- reject symlink/reparse-point surprises;
- set private permissions before writing secrets;
- preserve owner/group where required;
- flush file contents before rename for state that must survive a crash;
- use replace-existing semantics on Windows and rename semantics on Unix;
- optionally sync the parent directory on Unix for critical state;
- remove the temporary file on all failures.

Durability improvements are welcome, but they must be benchmarked because every upload already creates significant disk traffic.

### 12.6 Backups and maintenance

Preserve:

- pre-upload backup behavior;
- backup interval throttling;
- retention-hour pruning;
- maximum-backup pruning;
- timestamped backup filenames;
- list ordering;
- stale-database age calculation;
- 24-hour recent-activity deletion guard;
- dry-run output when `--confirm` is absent;
- moves to `DeletedDatabases` rather than permanent deletion;
- scheduled pruning disabled when `DatabasePruneDays` is zero.

The maintenance scheduler uses `time.Ticker` plus context cancellation. It runs an initial pass at the same lifecycle point as the current server and never overlaps itself.

#### 12.6.1 Intentional fix: backups inherit the database's modification time

`create_backup` copies with `shutil.copy2`, which **preserves the source database's mtime on the backup**. `prune_backup_directory` then prunes by mtime against `now - BackupRetentionHours`. A backup therefore inherits the age of the data it is protecting, and any backup of a database that has not changed within the retention window is deleted by the same call that created it. Measured against the shipping implementation with default settings (`BackupRetentionHours = 24`):

```text
fresh db (mtime = now)        -> backups retained: 1   Length=100  Revision='NjQtMThj…'
db last written 25h ago       -> backups retained: 0   Length=0    Revision=''
db last written 10 days ago   -> backups retained: 0   Length=0    Revision=''
```

The pre-upload safety backup does not exist in exactly the scenario it was designed for: a client that has been offline for days replacing the server copy. `create_backup` returns `{"Name": "…", "Length": 0, "Revision": ""}` for the deleted file rather than reporting failure, so nothing surfaces the loss.

The same inherited mtime propagates further. `newest_backup` orders by content age rather than backup age, so the `BackupIntervalMinutes` throttle measures time since the *previous content* was written, not time since the last backup. `backup_info` reports `CreatedUnixMs` as the database's modification time, so the `/api/v1/backups` listing misreports when every backup was taken.

This is a live defect, not a compatibility quirk. **Go fixes it deliberately**: a streaming copy sets the destination mtime to the backup creation time, backups are retained correctly, and the differential harness expects a narrowly scoped mismatch against Python.

Set backup mtime to creation time, prune by creation time, and report `CreatedUnixMs` honestly. Then:

1. Record the change as an intentional divergence in the differential normalizer, scoped narrowly to backup timestamps and retention counts so genuine backup regressions still fail the harness.
2. Add a Python characterization test in Phase 0 that pins the *current* behavior, so the divergence is demonstrated rather than assumed.
3. Note the operational consequence: servers with long-idle buckets will begin retaining backups they previously discarded, so disk use grows toward the documented `MaxBackups` ceiling for the first time. `MaxBackups` (default 48) already bounds this per bucket, but the release notes should say so.
4. Fix the shipping Python server during the bridge if its focused regression tests pass and doing so does not delay the native rewrite. The Go behavior does not depend on that bridge fix.

### 12.7 Logging

Preserve a 1 MiB active log with five rotated backups unless the setting is changed in a separately approved schema update.

Implement a small internal rotating writer rather than adopting a logging framework solely for rotation. Use the standard `log/slog` API internally if desired, but keep the written format understandable and compatible with existing manual instructions.

Redaction occurs before formatting:

- `/api/v1/database/<id>` becomes `/api/v1/database/<database-id>`;
- `/setup/<code>` becomes `/setup/<temporary-code>`;
- authorization headers are never logged;
- connection-file bodies and tokens are never logged;
- ordinary successful database polls may remain suppressed to control noise.

### 12.8 TLS and private CA

Use `crypto/tls`, `crypto/x509`, `encoding/pem`, and `crypto/rand`.

Required compatibility:

- load existing PEM chains and private keys;
- accept existing RSA CA keys in PKCS#1 or PKCS#8 form;
- validate that `CaFile` contains exactly one public certificate and no private-key block;
- validate CA constraints, signing usage, validity dates, and the active leaf chain;
- validate the advertised DNS name or IP with `VerifyHostname`;
- emit the same colon-separated uppercase SHA-256 fingerprint format;
- warn when the leaf expires within 30 days;
- retain existing CA material during ordinary renewal;
- replace the CA only with explicit `--new-ca`;
- generate a leaf with server-auth extended usage, `CA:FALSE`, required key usage, SAN DNS/IP values, and current Apple/Android compatibility;
- retain the current `Clipman Server Private CA` CA common name and `Clipman Server` leaf common name unless a separately reviewed change is made;
- retain the current ten-year new-CA validity and 397-day leaf validity;
- write a leaf-plus-CA full chain in the current order;
- never expose the CA private key through connection or sharing operations.

For the compatibility release, retain RSA-4096 for a new CA and RSA-2048 for a new leaf unless benchmarks or platform tests demonstrate a compelling reason to change. A later cryptographic-algorithm change requires its own compatibility and client-trust review.

### 12.9 Certificate sharing

The temporary CA-sharing server remains separate from the main listener:

- bind to the configured or overridden host;
- select a random high dynamic port;
- use a random unguessable path;
- serve only the public CA certificate;
- use plain HTTP deliberately because it distributes trust material whose fingerprint is verified separately;
- stop after the first successful `GET` or the configured timeout;
- do not stop after `HEAD`;
- print the stable three-line startup output before waiting;
- stop promptly when the parent process is terminating.

### 12.10 Connection documents

Preserve the version-1 JSON schema:

```json
{
  "address": "clipman://host:port",
  "clipman": "server-connection",
  "host": "host",
  "port": 12345,
  "token": "secret",
  "version": 1
}
```

When a validated private authority exists, add `ca_cert_pem` exactly as current clients expect and use an `https://` address.

Both text and `.clpconf` files remain private and are written atomically. If private-CA validation fails during ordinary startup, preserve the existing connection files and log the warning. If the user explicitly requests a rewrite, return an error instead of silently preserving stale output.

### 12.11 Network safety

Preserve the current startup refusal:

- loopback, link-local, RFC1918, and carrier-grade NAT addresses may use HTTP;
- wildcard addresses are not considered private client addresses;
- public plain-HTTP listeners require explicit `AllowInsecureRemote` or the matching CLI override;
- a wildcard listener requires a usable `AdvertiseHost` before connection files or setup links are produced;
- public setup URLs require HTTPS.

DNS resolution used to classify setup hosts must be bounded by a timeout and cancellation.

### 12.12 Updater

Rewrite updater behavior as an internal Go package exposed through compatibility flags and, later, structured commands.

Preserve:

- GitHub stable `server-vX.Y.Z` release selection;
- ignoring drafts, prereleases, and client-only tags;
- HTTPS-only API and asset URLs, including redirect validation;
- bounded release metadata, download size, entry count, and extracted size;
- required GitHub SHA-256 digest validation;
- traversal- and absolute-path-safe extraction;
- manifest name/version validation;
- service stop and restart;
- local TLS health checking against the local bind address while validating the advertised certificate identity;
- rollback of program and service files when health does not recover;
- preservation of settings, data, logs, and externally managed state;
- useful bounded diagnostics.

Symlink and special-file rejection is listed in section 6.3 as an improvement to **add**, not behavior to preserve. The current `safe_extract` validates only absolute paths and `..` components before calling `zipfile.extractall`; it performs no symlink, hard-link, device-node, or case-collision checking. Python's `extractall` happens not to materialize symlinks — it writes the link target as regular file content — which is why this has not caused a known incident. Go's `archive/zip` gives no such accidental protection, so the Go extractor must implement these checks explicitly rather than assume parity with Python.

Note also that `PurePosixPath("C:/evil").is_absolute()` is `False`, so a Windows-style drive-qualified entry passes the current guard. The Linux-only updater makes this moot today; a cross-platform Go updater must reject it.

The updater must never install an artifact for the wrong OS or CPU architecture. The package manifest gains a versioned artifact section rather than overloading the current `ServerProgram` field.

Container deployments do not self-update inside the container. They update by pulling a new immutable image and retaining the mounted data volume.

## 13. Platform integration

### 13.1 Windows

Keep the WinForms tray wrapper for the first Go release.

Change it as follows:

1. Build `clipman-server-core.exe` for Windows x64 first; add arm64 when the client/release matrix supports it.
2. Embed that binary as a wrapper resource, as the Python script is embedded today.
3. Extract it atomically to `%LOCALAPPDATA%\Clipman Server\Runtime\clipman-server-core.exe`.
4. Compare content before replacement so ordinary startup does not rewrite an unchanged runtime.
5. Launch it directly with `--config`, without `python.exe`, `pythonw.exe`, or `py.exe` discovery.
6. Route every utility action to the same executable.
7. Preserve stdout/stderr capture, restart limits, bind-error handling, tray labels, startup registry behavior, and update UX.
8. Preserve the existing mutex name for at least the transition release so an old and new wrapper cannot run together.
9. Remove Python-specific UI messages and runtime extraction names.
10. Sign the outer wrapper and ensure the embedded/extracted Go binary has a verifiable release signature or is authenticated by the signed wrapper plus a compiled digest.

The existing wrapper self-updater can continue replacing the outer EXE. Because the Go core is embedded, replacing the wrapper also updates the core.

Longer term, a native Windows service may be added under the separate future-server plan. It is not required to remove Python.

### 13.2 macOS

Keep the Swift/AppKit status-menu application for the first Go release.

The native Go server minimum is macOS 13, matching the durable Go toolchain direction and the broader server plan. The current app advertises macOS 10.13, but a current, security-supported Go toolchain must not be replaced with an obsolete compiler to retain that target. Publish the last Python-backed app for macOS 10.13 through 12 as an explicitly time-limited legacy artifact with a documented support end date; do not imply that it receives indefinite server security updates. This working floor may be overridden before packaging begins if a supported current Go toolchain materially changes its platform policy.

1. Build arm64 and amd64 Go binaries with `CGO_ENABLED=0`.
2. combine them into a universal executable with `lipo`;
3. place the core at `Clipman Server.app/Contents/Resources/clipman-server-core` or another stable internal path;
4. launch it directly from Swift;
5. remove Homebrew and `/usr/bin/python3` discovery;
6. preserve status-menu actions, login item, restart behavior, output parsing, logs, and update UX;
7. sign the nested core and then the complete app in the correct inside-out order;
8. run Gatekeeper/notarization validation for release artifacts when signing infrastructure is available.

The existing app updater continues replacing the full app bundle, which carries the new core.

### 13.3 Linux

Produce static binaries for:

- amd64;
- arm64;
- ARMv7 if Raspberry Pi support still requires it.

The user installer should:

- detect architecture without executing an untrusted candidate;
- copy the correct binary to `~/.local/lib/clipman-server/clipman-server`;
- use `~/.local/bin/clipman-server` as a small launcher or symlink;
- retain the `clipmanserver` management helper name;
- remove all Python JSON snippets by asking the Go binary for config values or by moving those operations into Go subcommands;
- preserve systemd user and runit/turnstile support;
- retain offline maintenance and restart safeguards;
- preserve custom install-directory environment variables;
- keep the privileged system-helper workflow functional without rewriting administrator-owned service state unexpectedly.

System installations must run the binary as the configured non-root account. The service remains an ordinary foreground process supervised by systemd or runit.

### 13.4 Docker

Replace `python:3.11-slim` and `apt install openssl` with a multi-stage Go build.

Preferred production image:

- distroless/static or `scratch` if CA roots and user identity are handled correctly;
- a non-root UID/GID;
- only the Go binary, public root certificates needed for outbound update-independent operations, license/notices, and required static assets;
- `/data` as the only writable volume;
- no package manager and no shell;
- direct environment-variable support in the Go binary;
- an internal `healthcheck` command if Docker health checks are added.

Retain all existing `CLIPMAN_*` environment names. Preserve the rule that a non-loopback plain-HTTP container listener requires explicit reverse-proxy or insecure-private-network acknowledgement.

Publish multi-architecture images for amd64 and arm64. Pin builder and runtime images by digest in release CI.

## 14. Build and artifact design

### 14.1 Go build settings

- Pin a supported Go patch release in CI and release scripts.
- Record the platform floor implied by that toolchain. Current official requirements are Windows 10/Windows Server 2016 or newer, Linux kernel 3.2 or newer for modern Go, and the macOS limits described above.
- Use `CGO_ENABLED=0`.
- Use `-trimpath`.
- Set version and commit metadata through `-ldflags`.
- Strip symbols for release binaries with `-s -w`; keep symbolized CI artifacts for crash diagnosis where appropriate.
- Run `go vet`, `go test`, `go test -race` on supported race-capable hosts, and vulnerability scanning.
- Generate checksums and a software bill of materials for published artifacts.

### 14.2 Release artifacts

Move toward platform-specific assets:

```text
ClipmanServer-Windows-x64-<version>.zip
ClipmanServer-macOS-universal-<version>.zip
ClipmanServer-Linux-amd64-<version>.tar.gz
ClipmanServer-Linux-arm64-<version>.tar.gz
ClipmanServer-Linux-armv7-<version>.tar.gz
```

Keep `ClipmanServer-<version>.zip` as a transition asset while old updaters require it. Do not permanently put every architecture into one archive; that would forfeit much of the distribution benefit.

Each native package includes:

- executable or wrapper app;
- server manual;
- example settings;
- license and dependency notices;
- manifest with OS, architecture, version, executable path, and format version;
- SHA-256 checksums;
- signature/provenance artifacts where supported.

### 14.3 Build script changes

- Teach `Build-ServerBundle.ps1` to build the Go core before compiling the Windows wrapper.
- Embed the Windows Go binary in the C# wrapper.
- Send both macOS Go architectures to the Mac packaging step or build them on the Mac and combine them there.
- Update `ClipmanServerMac/Scripts/package-release.sh` to bundle and sign the Go core.
- Update `package-combined-server.sh` only for the transition package.
- Replace Docker build copies of `clipman_server.py` with the native binary build stage.
- Make the release fail if any package still requires Python after the transition milestone.

## 15. Migration and update strategy

### 15.1 Data migration

There is no data-format migration.

The Go process starts against the existing settings and data root, acquires the existing lock, reads existing metadata/backups/TLS files, and serves the existing buckets. The first start must not rename, rewrite, or relocate user data.

### 15.2 Runtime migration

The migration replaces only program files:

- Windows: outer tray EXE update, followed by extraction of the embedded Go core;
- macOS: full app replacement containing the Go core;
- Linux: installer replaces the Python launcher/runtime with the native binary while preserving config, data, logs, service state, and ownership;
- Docker: new image with the existing `/data` volume.

### 15.3 Bridge releases

Old Linux updaters require `clipman_server.py` and `clipman_server_updater.py`, and externally managed installations may update only a fixed set of Python program files. A direct incompatible package would strand or break those installations.

The constraint is enforced by three specific pieces of already-deployed code. Every installation that has not yet been migrated runs its *own existing copy* of these, so they cannot be relaxed by shipping a new updater — they bound what the transition release is allowed to look like:

| Enforcing code | Constraint it imposes |
|---|---|
| `locate_package_root` | Rejects any package that does not contain **all three** of `clipman_server.py`, `clipman_server_updater.py`, and `Linux/install-clipman-server.sh`. The transition ZIP must carry these files for as long as one un-migrated installation exists — not merely until the Go default ships. |
| `MANAGED_PROGRAM_FILES` | A fixed four-name tuple: `clipman_server.py`, `clipman_server_updater.py`, `Manual.html`, `LICENSE.txt`. A `--managed-program-only` update copies exactly these names and nothing else, so **a managed-mode install can never receive a Go binary through its current updater.** The only route is the two-step migration in point 6 below: the bridge updater arrives as `clipman_server_updater.py`, and on its *next* run it installs the native asset itself. |
| `find_update` | Raises a hard `RuntimeError` — it does not report "up to date" — when a newer `server-v*` release exists but `ClipmanServer-<version>.zip` is missing from its assets. Publishing a release with only platform-specific assets makes every deployed updater fail loudly. Section 14.2's transition asset is therefore mandatory, not a convenience. |

Because the second row means a managed installation needs two successive update cycles to reach Go, the bridge release must be published far enough ahead of the Go default that both cycles can complete within the support window.

Use a bridge strategy:

1. Publish at least one Python-compatible bridge release whose updater understands versioned manifests, native OS/architecture assets, and same-version runtime migration.
2. During the first Go release, continue publishing a legacy-compatible `ClipmanServer-<version>.zip` containing safe Python fallback files plus the new installers/wrappers.
3. Publish native Go assets beside the combined transition asset.
4. Existing Windows and macOS updaters install the Go-backed wrapper/app from the combined asset.
5. Normal Linux updates invoke the new installer from the combined asset and move to the Go binary.
6. Managed-program-only Linux installs first receive the bridge updater; on its next run it recognizes that the current version still uses Python and installs the same-version native asset without requiring a higher server version.
7. Keep the Python fallback and combined asset for at least one full release cycle after the Go default ships.
8. Record migration completion in program state only after the Go process passes its health check.
9. Remove the Python fallback only after telemetry-free evidence from issue reports, clean-VM tests, and update simulations shows that every supported install path migrates safely.

The migration logic must be tested from more than one historical server package, not just the immediately preceding release.

### 15.4 Rollback

- Retain the previous program files until the new Go process passes health and a real `HEAD`/conditional `PUT` smoke test against a disposable bucket.
- Never roll back settings or user data merely because a program update failed.
- If the Go process fails before modifying a bucket, restart the Python program.
- If the Go process successfully writes data and then fails, the Python server must still be able to serve that unchanged on-disk format.
- Preserve the previous wrapper/app/binary until the post-update check completes.
- Log which version and runtime were restored.

## 16. Testing strategy

### 16.1 Test ownership and source of truth

Do not mechanically port every Python test into an equivalent Go test. That would double maintenance while still missing real-client behavior. Keep the Python tests unchanged as the legacy-runtime regression suite during the bridge, then assign every compatibility requirement to one primary automated layer:

| Surface | Primary test owner | Why |
|---|---|---|
| Successful client sync, encryption, bucket identity, conflicts, and normal errors | Built `clipman-cli` subprocesses | Exercises a real cross-platform client and the same command/configuration path users receive |
| Exact HTTP status, headers, malformed input, slow/disconnected bodies, and unsupported methods | Raw protocol differential driver | A correct CLI deliberately cannot generate these cases |
| Settings, on-disk bytes, metadata, backups, locks, maintenance CLI, logs, and process exit codes | Process/filesystem compatibility driver | These are observable server contracts but not client operations |
| Setup links, connection files, and private-CA TLS | CLI plus raw driver | CLI proves a real client can import and use the result; raw probes prove consumption, privacy, and exact headers |
| Algorithms and security invariants inside the Go implementation | Focused Go unit, fuzz, and race tests | Faster and more diagnostic than black-box process tests |
| Updater archives, rollback, service coordination, wrappers, installers, and containers | Package sandboxes and platform CI | These change host/process state outside the sync protocol |

The rule is "one authoritative test at the lowest useful layer, plus a CLI acceptance test for user-visible sync," not "the same assertion in Python, Go, and the harness." Existing Python cases may move into the shared black-box driver when that makes them reusable against both runtimes. Do not delete the Python suite until the rollback window closes.

Maintain a machine-readable coverage manifest. Each route, server command, settings mutation, on-disk artifact, updater operation, and intentional divergence names its owning test case and required platforms. CI fails when a contract entry has no owner. This manifest replaces repeated human sign-off that a checklist was remembered.

### 16.2 CLI-driven acceptance harness

Build `clipman-server-compat` as a repository test tool. It receives paths to a Python server, a Go server, and one already-built `clipman-cli`; launches servers in isolated temporary roots; reserves ports; writes token files and per-client config paths; invokes the CLI as an external process; and records stdout, stderr, exit code, JSON output, server files, and logs. It must never use a developer's real Clipman configuration, credential store, data root, or network listener.

Build the CLI once per target job and pin it to the repository commit under test. `go test ./...` in `ClipmanCli` is a prerequisite gate. Invoke the executable with `--config` and `CLIPMAN_PASSWORD` rather than putting test secrets in command arguments. Use token files for initialization. Test credentials are random per run, local to the temporary root, and never copied into failure artifacts without redaction.

Run three categories of CLI scenarios:

1. **Same-runtime acceptance.** Run the same scenario independently against Python and Go from a fresh root, normalize documented nondeterminism, and compare semantic results.
2. **In-place handoff.** Seed history through CLI against Python, stop it, start Go over the same root, read and mutate through CLI, stop Go, restart Python, and verify the final history. Repeat with the direction reversed. This is the highest-value drop-in replacement and rollback test.
3. **Concurrent clients.** Launch multiple CLI processes with separate config files and machine names against the same password/bucket, and with different passwords/buckets, using a barrier so creates and updates race intentionally.

The deterministic synthetic corpus must include:

- an absent bucket and an empty newly created database;
- ASCII, Unicode, emoji, combining characters, NUL, tabs, quotes, JSON/HTML-like text, CRLF, LF, trailing whitespace, and no-final-newline values;
- history entries, templates, pins, names, groups, duplicate modes, touches, removals, tombstones, and unknown rich-text fields from the existing Windows fixtures;
- multiple passwords under one token to prove bucket isolation;
- wrong token and wrong-password outcomes without exposing either value;
- blobs around meaningful size boundaries and a generated large history up to the configured limit;
- identical uploads, changed uploads, first creation, stale-revision conflicts, conflict retries, simultaneous creation, and simultaneous independent-bucket writes;
- plain HTTP on loopback, direct private-CA HTTPS, and `.clpconf` initialization.

At minimum, exercise `init`, `status`, `status --refresh`, `put`, `list --json`, `get`, `get --touch`, `rm --yes`, and `sync --json`. Compare structured JSON and payload bytes rather than prose wherever the CLI offers both. The driver owns an explicit field-level normalizer for ports, timestamps, revisions where filesystem precision legitimately differs, generated IDs, certificate values, and health counters. It must not ignore whole bodies, headers, log lines, or filesystem trees.

The CLI is intentionally not extended with malformed-request or server-administration commands solely for this rewrite. If a generally useful diagnostic command emerges, it requires its own product review; the compatibility driver may use test-only raw HTTP freely.

### 16.3 Raw protocol and filesystem differential driver

The same compatibility tool launches Python and Go servers with equivalent settings and sends scripted raw HTTP/1.1 requests where `net/http.Client` would normalize away the behavior being tested. For each case it compares:

- status code, required headers, body bytes, and connection-close behavior;
- resulting database bytes and exact revision token;
- metadata fields and `"head"`-versus-`"write"` events;
- backup count, names, contents, timestamps, throttling, retention, and listing;
- setup-link state transitions, download limits, response bytes, and redacted logs;
- settings mutations, generated connection files, certificate files, and permissions;
- process exit code and wrapper-consumed stdout/stderr prefixes.

Cases include malformed and percent-encoded paths, database-ID grammar, missing/wrong auth, unsupported methods, trailing slashes, missing/negative/conflicting lengths, short and oversized bodies, both conditional headers, quoted revisions, `Expect: 100-continue`, concurrent creates/conflicts, cancellation, slow bodies, disconnects, TLS, IPv4, wildcard listeners, and keep-alive recovery. IPv6 is Go-only because the Python implementation cannot bind it.

The driver also closes the existing characterization gaps against Python before dependent Go code lands:

- backup creation, interval throttling, retention, `MaxBackups`, and `/api/v1/backups`;
- revision format and platform timestamp precision;
- conditional `PUT` failures and status bodies;
- database-ID validation, including Python's Unicode-aware `str.isalnum` behavior;
- health JSON shape and platform-dependent fields;
- metadata touch throttling and event selection;
- `--list-databases`, `--delete-database`, `--prune-databases-days`, and the recent-activity guard;
- every exit code in section 8.4.

Store compact normalized snapshots only for stable external contracts. Prefer assertions over large goldens, and generate large dummy data during the run from a fixed seed rather than committing large blobs. Reuse the existing `ClipmanCli/testdata/fixtures` corpus for codec interoperability; do not duplicate those files under the server.

Beyond nondeterminism, maintain a short, machine-readable list of **intentional divergences**. The initial list is: fixed backup timestamps/retention (12.6.1), `Expect: 100-continue` timing and request-body draining (7.5), log ordering around connection-file writes (9.2), Windows exclusive bind behavior (12.1), IPv6 support (12.1), and LF for new Windows `.clpconf` files (6.4). Each entry identifies one exact case and expected side-specific result. Adding a broad normalizer is treated as a protocol change.

### 16.4 Unit, fuzz, race, and resource tests

Add focused Go unit tests for revision encoding, JSON unknown-key preservation, platform paths, atomic replacement, data-root locks, keyed-lock lifecycle, streamed equality comparison, backup boundaries, certificate/key compatibility, SAN validation, setup HTML escaping, log redaction, updater version/package selection, and hostile archive extraction.

Fuzz database and setup route parsing, settings JSON, connection-document inputs, PEM parsing, certificate name normalization, updater manifests/archive paths, conditional headers, and redaction. Fuzz tests must never write outside their temporary root.

Run `go test -race` for two first writers, upload versus download, upload versus prune/delete, setup final-download races, runtime counters, shutdown during transfers, certificate-share completion versus timeout, and updater health polling/cancellation.

Add bounded-resource checks for 1 KiB, 1 MiB, 16 MiB, and 64 MiB blobs; slow and abandoned transfers; file-descriptor, staging-file, keyed-lock, and goroutine cleanup; header limits; decompression limits in the CLI validation path; and archive entry/expanded-size limits. The raw load driver, rather than repeated CLI process startup, measures server throughput and memory. CLI scenarios remain the semantic correctness gate at each size.

### 16.5 Platform, package, and wrapper automation

Automate packaging tests before reserving a scenario for manual execution:

- **Windows:** install/extract without Python, Git, or OpenSSL; launch the wrapper and discover its loopback endpoint; run the CLI corpus; exercise restart, startup registration state, update, failed-update rollback, uninstall-with-data-preservation, paths with spaces/non-ASCII, secret ACLs, and wrapper/core version matching.
- **macOS:** install the universal app without Homebrew or Python; launch the status app/core; run the CLI corpus on Intel and Apple Silicon runners; exercise login-item state, update/rollback, data preservation, nested-code signing, notarization, and Gatekeeper assessment.
- **Linux:** run the CLI corpus against installed amd64, arm64, and supported ARMv7 binaries under systemd user, runit, no-service-manager, privileged helper, custom directory, and rootless modes; verify update/rollback, ownership, modes, and uninstall preservation.
- **Container:** run the CLI corpus against amd64/arm64 images as a non-root UID with fresh and existing volumes, reverse proxy, direct TLS, explicit insecure private-network mode, environment precedence, immutable root filesystem where practical, clean shutdown, and volume ownership.

Wrappers and installers should expose or log the child core endpoint and version in a stable, test-readable way if they do not already. Test automation may use wrapper CLI switches or environment variables that are explicitly documented as diagnostic-only; it must not depend on clicking screen coordinates.

Manual release-candidate work is limited to behavior that CI cannot faithfully observe: Windows tray/menu presentation and notifications, macOS status-menu presentation and login approval prompts, screen-reader behavior where relevant, Defender/SmartScreen reputation, Gatekeeper user prompts, and one clean-machine install/update/rollback smoke on each desktop OS. Normal synchronization correctness is not retested manually; the packaged core must pass the same CLI corpus first.

### 16.6 Failure artifacts and reproducibility

Every compatibility run records the seed, runtime versions, OS/architecture, normalized command transcript, server arguments, and paths relative to its temporary root. On failure, retain the minimal redacted settings/filesystem snapshot and exact request/response transcript needed to reproduce it. Never retain bearer tokens, passwords, private keys, full database IDs, or setup codes in CI artifacts.

Support these modes:

- `reference`: run Python alone and refresh reviewed stable snapshots;
- `differential`: run Python and Go and compare them;
- `go-only`: run after Python retirement using the reviewed snapshots and contract assertions;
- `package`: target an already installed or wrapped server endpoint and run the CLI corpus.

The same seed must reproduce the same logical history and operation order. Port allocation, clock-dependent checks, and concurrency barriers are controlled by the harness; arbitrary sleeps are not synchronization. Python reference servers use the documented persistent range `20000`–`49151`, not Windows' dynamic client-port range, and the selected listener is verified healthy before the CLI starts.

## 17. Performance and resource plan

Performance work must compare measured behavior, not assume Go is faster.

### 17.1 Baseline before implementation

Record Python results for:

- idle resident memory;
- cold startup to healthy;
- `GET /api/v1/health` throughput and latency;
- database `HEAD` throughput at 1, 10, and 100 clients;
- `GET` and `PUT` for 1 KiB, 1 MiB, 16 MiB, and 64 MiB blobs;
- identical upload versus changed upload;
- backup-enabled changed upload;
- concurrent operations on the same and different bucket IDs;
- slow upload/download and abandoned connections;
- TLS and plain HTTP;
- Raspberry Pi-class storage if available.

### 17.2 Go acceptance targets

- No regression in single-client `HEAD`, `GET`, or `PUT` wall time beyond an explicitly justified tolerance.
- Better throughput under concurrent independent buckets.
- Transfer buffers remain bounded; a 64 MiB upload must not allocate a 64 MiB request slice or read the existing 64 MiB database into another slice.
- Memory use grows with bounded per-transfer buffers and configured concurrency, not total blob size times uncontrolled request count.
- No goroutine, file-handle, staging-file, or keyed-lock leaks after cancelled requests.
- Startup reaches health faster than the Python wrapper path on representative Windows and macOS machines.
- Container image size is materially smaller than the Python-plus-OpenSSL image.
- Native package size is recorded honestly even though it will exceed the compressed Python source size.

Publish benchmark methodology and results with the first release candidate.

## 18. Security review checklist

- Threat-model public TLS, private LAN/VPN, reverse proxy, malicious authenticated client, local unprivileged user, malicious update archive, and interrupted update.
- Confirm no code path logs or returns tokens, CA private keys, setup codes, or database IDs unnecessarily.
- Verify server-side TLS minimums and certificate-chain loading on all platforms.
- Verify existing private CA renewal does not rotate trust unexpectedly.
- Verify file permissions and ACLs after fresh install, upgrade, rollback, and explicit settings rewrite.
- Bound headers, bodies, release metadata, archives, redirects, DNS lookups, concurrent uploads, and diagnostics.
- Refuse archive symlinks, hard links, devices, alternate data streams, traversal, absolute paths, and case-collision overwrites.
- Refuse data roots or database buckets that resolve through unsafe links where feasible.
- Use cryptographically secure randomness for ports where required by compatibility, tokens, setup codes, certificate serials, and temporary names.
- Use constant-time secret comparisons.
- Keep health information non-sensitive.
- Test request smuggling and conflicting length/transfer-encoding inputs through `net/http` and any reverse-proxy configuration.
- Run Go vulnerability scanning and dependency review for every release.
- Generate an SBOM and retain build provenance.
- Obtain an independent review before making the Go server the only shipped runtime.

### 18.1 Compatibility extension: optionally scoped backup listing

`Handler.list_backups` globs `Databases/*/ServerBackups/*.clipdb` across the whole data root and returns a name, length, creation timestamp, and revision for **every** backup on the server. The endpoint requires the bearer token, but the token is shared by every client of a server while buckets are partitioned by history *password*. A household or team member with the token — and no knowledge of another member's password — can therefore observe how many buckets exist, how large each backup is, and when each was written, even though the bucket contents remain encrypted and the bucket IDs are not returned.

The exposure is metadata only and no released client calls this endpoint for anything other than its own diagnostics. The compatibility release uses option 2 below:

1. **Preserve.** Zero client risk, zero migration cost. Records a known cross-bucket metadata leak as intended behavior.
2. **Scope to a requested bucket** by adding an optional database-ID parameter and returning only that bucket's backups when supplied, preserving the unscoped response otherwise. Compatible, and gives future clients a correct endpoint to move to.
3. **Scope unconditionally**, returning an empty list without a bucket parameter. Cleanest, and a protocol change that section 5 places out of scope for the compatibility release.

Implement option 2 during the compatibility release and defer option 3 to the separate future-server work in `clipman-server.md`. State both the legacy behavior and the new scoped form in the release notes and manual.

## 19. Implementation phases and exit criteria

### Phase 0: Freeze the compatibility contract

Work:

- add the machine-readable coverage manifest and assign every contract to the CLI harness, raw driver, filesystem/process driver, Go unit tests, or platform package tests;
- build `clipman-server-compat` in `reference` mode so it launches the Python server in a temporary root and drives one built CLI through the deterministic synthetic corpus;
- implement the raw/filesystem characterization cases catalogued in section 16.3 against Python — backups, revisions, conditional `PUT`, database-ID validation, health shape, metadata events, maintenance CLI, and exit codes;
- capture golden connection files, metadata, setup responses, logs, and certificate fixtures **as bytes**, on both Windows and Linux, so the line-ending and escaping rules in section 6.4 are pinned rather than described;
- capture baseline performance and package sizes;
- inventory historical release packages needed for updater tests;
- approve the supported Windows, macOS, Linux kernel, and CPU-architecture floors for the selected Go toolchain;
- record any override to the four working decisions in section 2.1 before code depending on that decision lands.

Exit criteria:

- the unchanged Python suite and `go test ./...` in `ClipmanCli` pass on Windows, and in Linux CI where applicable;
- `clipman-server-compat reference` can replay the same seed without using real user state;
- every current route, server CLI operation, settings/file mutation, and updater action has a test owner, including every exit code;
- nondeterministic fields are explicitly identified, and the intentional-divergence list in section 16.3 is complete and reviewed;
- stable snapshots and failure artifacts are redacted and reproducible;
- the working decisions in section 2.1 are either retained or replaced with recorded alternatives.

### Phase 1: Go skeleton, configuration, paths, and locking

Work:

- create the Go module and command;
- add `differential` and `go-only` modes to the compatibility driver;
- implement versioning and exit codes;
- implement defaults, settings load/save, unknown-key preservation, and platform paths;
- implement private atomic files;
- implement data-root locking on Windows and Unix;
- implement logging and signal handling;
- add CI for Windows, macOS, and Linux builds.

Exit criteria:

- settings, paths, lock, exit-code, and stable-output differential tests pass through the shared driver;
- sparse startup never rewrites settings;
- killed lock holders do not block restart;
- release binaries cross-compile without CGO.

### Phase 2: Blob store and HTTP synchronization

Work:

- implement database ID parsing, revisions, metadata, keyed locks, streaming upload/download, auth, health, and runtime counters;
- implement backup creation/pruning;
- implement network safety and TLS listener loading;
- add request limits and graceful shutdown.

Exit criteria:

- the full same-runtime CLI scenario passes against Go and matches Python semantically;
- Python-to-Go-to-Python and Go-to-Python-to-Go in-place handoff scenarios preserve the logical history and opaque stored blob behavior;
- concurrent CLI create/update scenarios converge without lost entries, and separate password buckets remain isolated;
- raw conditional-write, first-writer, malformed-request, and `100-continue` differentials pass;
- existing cross-client `.clipdb` and identity fixtures remain green, providing automated coverage for client-specific codecs and bucket derivation;
- race tests pass;
- 64 MiB transfers use bounded memory.

### Phase 3: Administration, onboarding, and certificates

Implementation status: core work and automated acceptance paths implemented on `go-server`; platform wrapper presentation remains part of Phase 5.

Work:

- implement connection files, setup links/page, database listing/deletion/pruning, certificate inspection/generation/renewal, fingerprinting, and temporary CA sharing;
- port wrapper-consumed stable output;
- verify existing RSA CA/key fixtures.

Exit criteria:

- every server-core Python scenario has an owner in the shared driver or focused Go tests;
- the CLI can initialize from a Go-generated `.clpconf`, validate direct private-CA HTTPS, and complete the normal corpus without installing the CA system-wide;
- setup-link concurrency and redaction tests pass;
- an existing private CA can renew a leaf without client retrust;
- certificate operations require neither Python nor OpenSSL.

### Phase 4: Go updater and package manifests

Implementation status: updater security/transaction core, standalone updater command, manifest v2, native asset selection, same-version migration selection, health rollback, and simulated service coordination are implemented. Concrete installed-service adapters and package-script integration are completed alongside Phase 5.

Work:

- implement release discovery, download limits, digest checks, extraction, architecture selection, service coordination, health checks, rollback, and host change;
- define manifest version 2;
- build historical-update simulations;
- add same-version Python-to-Go migration support.

Exit criteria:

- updater unit, malicious-archive, health, and rollback tests pass;
- compatibility `package` mode passes before and after a successful update and after a forced rollback;
- settings/data are unchanged across failed updates;
- systemd, runit, externally managed, Windows, and macOS paths have tested migration stories.

### Phase 5: Platform wrappers and installers

Work:

- embed and launch the Go core from Windows and macOS wrappers;
- update Linux user/system installers and helper commands;
- replace the Docker runtime;
- remove active Python discovery and execution from normal paths;
- update build scripts and artifact naming.

Exit criteria:

- clean Windows and macOS machines with no Python/OpenSSL launch the wrapper/app and pass compatibility `package` mode against the bundled core;
- automated wrapper diagnostics prove wrapper/core version agreement, restart, update, rollback, startup/login registration state, and data preservation;
- Linux service matrices pass;
- multi-architecture container tests pass;
- a repository search finds no production launch path invoking Python.

### Phase 6: Differential, endurance, and security release gate

Work:

- run every compatibility-tool mode, the full seeded CLI corpus, raw differentials, handoff/rollback loops, and package matrices;
- run race, fuzz, slow-client, cancellation, and endurance tests;
- benchmark and tune timeouts/buffers;
- conduct security and packaging reviews;
- complete signing, checksums, SBOM, and release documentation.

Exit criteria:

- no unexplained differential mismatch;
- no uncovered entry remains in the compatibility manifest;
- no race or resource leak;
- performance/resource targets are met and published;
- release artifacts pass clean-machine installation and update tests;
- rollback is demonstrated from the release candidate;
- the small manual desktop checklist in section 16.5 passes; no manual repetition of normal sync scenarios is required.

### Phase 7: Bridge release

Work:

- ship the bridge-aware Python updater and transition combined asset;
- ship native Go assets;
- exercise real update paths while retaining fallback;
- collect issue reports without adding telemetry;
- document manual recovery.

Exit criteria:

- supported historical installs can reach the Go runtime through automatic or documented update flows;
- the installed Go runtime passes compatibility `package` mode after each historical upgrade path;
- failed Go health checks restore a working Python runtime;
- no data or CA-trust migration is required.

### Phase 8: Go default and Python retirement

Work:

- make native assets the documented default;
- retain the bridge combined asset for the promised window;
- remove Python from new Windows/macOS/Linux/container installations;
- archive legacy Python sources/tests only after the rollback window;
- simplify build and update logic after legacy usage is no longer supported.

Exit criteria:

- all supported packages run without Python;
- update documentation no longer sends users through a Python prerequisite;
- the legacy removal does not strand a supported updater version;
- the definition of done below is satisfied.

## 20. Risk register

| Risk | Impact | Mitigation |
|---|---|---|
| HTTP behavior drifts subtly | Released clients conflict or fail | Raw differential cases plus the real `clipman-cli` acceptance corpus on every target |
| Revision timestamps differ by platform | One-time conflicts or repeated polls | Exact fixtures and cross-platform tests; preserve commit-time correctness |
| Existing CA is replaced accidentally | Every client loses TLS trust | Existing-key fixtures; explicit `--new-ca`; backup before certificate writes |
| Streaming changes atomicity | Corruption or inconsistent revisions | Stage, flush, conditional check under lock, backup, atomic replace |
| Open download handles block Windows replace | Upload failures under concurrency | Cross-platform handle/replace tests and snapshot fallback |
| Go binary increases release ZIP size | Larger downloads | Platform/architecture-specific artifacts and compression |
| Old Linux updater cannot install native layout | Stranded installations | Bridge updater, transition asset, same-version migration, historical simulations |
| Wrapper update replaces itself but not core | Version mismatch | Embed core and verify version/digest at startup |
| macOS nested binary fails signing/Gatekeeper | App will not launch | Inside-out signing and clean-machine Gatekeeper tests |
| Windows security software flags new binary | User distrust or blocked install | Code signing, reproducible artifacts, reputation-aware staged release |
| Timeouts reject legitimate slow networks | Sync failures | Benchmarks, configurable but bounded defaults, slow-client tests |
| Keyed locks or goroutines leak | Memory growth | Reference counting, cancellation, race/endurance tests |
| Unknown settings are lost | Forward/backward compatibility break | Raw-property preservation tests |
| New updater corrupts externally managed service state | Service outage | Managed-mode snapshots and no-touch guarantees |
| Exit codes ported from the incorrect earlier table | Wrappers and installers misread refusals as success or crashes | Corrected table in 8.4; fixture-test every listed path |
| Go JSON/line-ending defaults rewrite user files | Noisy diffs, broken byte-comparison tests, `.clpconf` churn | Serialization rules in 6.4; byte-level golden fixtures captured on both Windows and Linux |
| Backup mtime fix is hidden by broad normalization | A real backup regression passes or the bridge behaves unexpectedly | Scoped divergence entry, Python characterization, Go retention assertions, and release notes per 12.6.1 |
| Managed-mode Linux installs cannot receive a Go binary | Silently stranded installations | `MANAGED_PROGRAM_FILES` constraint named in 15.3; two-cycle bridge published early enough |
| Release omits `ClipmanServer-<version>.zip` | Every deployed updater fails loudly, not gracefully | `find_update` constraint named in 15.3; CI asserts the transition asset exists |
| `Machine` or hostname newly exposed on unauthenticated health | Information disclosure on a public listener | Reproduce platform-conditional behavior per 7.7; health payload review |
| Windows bind now fails where Python silently shared the port | Restart loops after upgrade | Documented divergence in 12.1; restart and upgrade tests on Windows |
| CLI-only testing misses a server edge case | Malformed requests, admin operations, or wrappers regress | Coverage manifest assigns those surfaces to raw, filesystem/process, updater, or package tests rather than claiming the CLI covers them |
| Test harness accidentally uses personal state or leaks dummy credentials | Destructive local effects or secret exposure | Mandatory temporary roots, explicit CLI config paths, random per-run credentials, redacted artifacts, and refusal to run on resolved non-temporary data roots |
| Scope expands into future server redesign | Rewrite never ships | Enforce non-goals and separate architecture decisions |

## 21. Documentation work

Update in the same release:

- `ClipmanServer/Manual.html`;
- `README.md` server installation summary;
- `ClipmanServer/clipman-server-settings.example.jsonc` where runtime wording changes;
- Windows and Linux certificate helper instructions;
- Docker examples and image documentation;
- Linux user/system service instructions;
- troubleshooting paths and process names;
- update/rollback recovery instructions;
- package manifest documentation;
- supported OS/architecture minimums and the macOS deployment-target change;
- release notes explaining that data, passwords, tokens, buckets, and connection files do not migrate.

Remove statements that Python 3 or OpenSSL must be installed once the relevant artifact no longer needs them. During the bridge period, label legacy and native instructions clearly rather than presenting contradictory requirements.

Retire or rewrite the standalone `new-clipman-server-cert.sh`, `New-ClipmanServerCert.ps1`, and certificate regression tools so no documented certificate workflow invokes Python or requires OpenSSL after the Go path ships. Trust-store helpers may remain platform-native, but their messages and examples must refer to the Go runtime.

## 22. Definition of done

The rewrite is complete when:

1. The shared server and updater production logic are Go.
2. Windows and macOS wrappers launch a bundled Go core and contain no normal Python execution path.
3. Linux native packages and services run the Go binary without Python.
4. The production container contains no Python runtime or OpenSSL executable.
5. Existing settings, data roots, buckets, metadata, backups, TLS keys/certificates, connection files, and setup state work in place.
6. The full CLI acceptance corpus passes against every packaged server target, in-place Python/Go handoff passes in both directions, and every released client's automated codec/identity/connection fixture remains green; one release-candidate smoke per supported client/platform confirms no client update is required.
7. Differential tests have no unexplained protocol or filesystem mismatch, and the coverage manifest has no unowned contract.
8. Every existing server/updater scenario is covered by the shared black-box driver or a focused Go test; mechanical one-for-one test duplication is not required.
9. `go test -race`, fuzz smoke tests, static analysis, and vulnerability scanning pass.
10. Large uploads/downloads use bounded memory and meet measured acceptance targets.
11. Clean Windows and macOS machines need neither Python nor OpenSSL.
12. Linux amd64/arm64 and supported Raspberry Pi targets install, start, update, roll back, and preserve ownership.
13. Multi-architecture container images run non-root and preserve existing volume data.
14. Updates verify digests, reject unsafe archives, health-check the new process, and roll back program files on failure.
15. Signed/checksummed artifacts, SBOM, notices, manuals, migration notes, and recovery steps ship together.
16. The bridge strategy has been exercised from supported historical releases.
17. No supported installation is stranded when legacy Python assets are retired.
18. The four working decisions — backup mtime (12.6.1), IPv6 listeners (12.1), cross-bucket backup listing (18.1), and `.clpconf` line endings (6.4) — are implemented and stated in the release notes, or an override is recorded with corresponding tests.
19. Every intentional divergence is narrowly scoped in the differential normalizer, and no divergence exists that is not on that list.
20. Exit codes match the corrected table in section 8.4, verified by fixture tests rather than by inspection.

## 23. Recommended first implementation slice

The first pull request should build the reusable oracle before substantial porting code, then prove it against the smallest Go process surface:

1. Add `ClipmanServer/go.mod`, `cmd/clipman-server`, `internal/compat`, `cmd/clipman-server-compat`, and the compatibility coverage manifest.
2. Build `clipman-cli` once and add a Python-only reference scenario using isolated config/data roots: missing-bucket `init`/`status`, `put`, `list --json`, byte-exact `get`, `sync --json`, `rm --yes`, and final status.
3. Add a deterministic dummy-data generator, fixed seed reporting, temporary-root safety checks, structured normalization, and secret-redacted failure artifacts.
4. Add raw Python characterization for revisions, conditional writes, database-ID validation, health shape, backups, metadata events, maintenance commands, and all exit codes. These cases become reusable differentials, not a second Python-only suite.
5. Implement Go version injection, the section 8.4 exit codes, settings load/default/save with unknown-key preservation and byte rules, platform paths, atomic private files, data-root locking, logging, and clean shutdown.
6. Run those process/filesystem cases against Python and Go through `differential` mode; add `go-only` snapshot/assertion mode at the same time.
7. Add CI builds for Windows amd64, Darwin amd64/arm64, Linux amd64/arm64, and optional ARMv7, while executing native tests on Windows and Linux rather than treating cross-compilation as runtime coverage.
8. Record stripped/unstripped server size, CLI/harness version, startup time, and the exact compatibility seed.
9. Do not change a production wrapper, installer, or release artifact in this slice.

The second slice should implement the minimal authenticated `health`/`HEAD`/`GET`/conditional-`PUT` vertical path and immediately enable the same-runtime CLI, cross-runtime handoff, and concurrent-CLI scenarios. From that point on, every server phase adds cases to the same corpus rather than inventing another acceptance workflow.

## 24. External toolchain constraints

Toolchain and platform floors are release inputs, not incidental build details. Verify them again when implementation begins.

The local development baseline was verified with Go 1.26.5 on Windows on 2026-08-10. Go 1.26 is the last release supporting macOS 12, while Go 1.27 requires macOS 13; the native server therefore targets macOS 13 so adopting the next supported Go release does not immediately change the advertised server floor.

- [Go minimum platform requirements](https://go.dev/wiki/MinimumRequirements)
- [Go 1.26 release notes, including the macOS 12 support boundary](https://go.dev/doc/go1.26#darwin)
- [Go release policy and supported patch releases](https://go.dev/doc/devel/release)
