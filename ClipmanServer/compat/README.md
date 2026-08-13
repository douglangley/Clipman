# Clipman Server compatibility harness

`clipman-server-compat` is test tooling and is not included in production server packages.

It supports the four modes defined by `GO_SERVER_REWRITE_PLAN.md`:

- `reference`: probe the Python server and the built Clipman CLI;
- `differential`: probe Python and Go server executables;
- `go-only`: validate the Go executable after Python retirement;
- `package`: validate an installed or wrapped server package.

`package` mode now verifies executable/core version agreement, checks an already-running loopback package endpoint, and drives a disposable real-CLI smoke corpus through `init`, `put`, `get`, `list`, `status --refresh`, `sync`, and `rm`. It reads the bearer token from a file and keeps the deterministic dummy password and client configuration in a temporary directory. The smoke entry is removed at the end; its encrypted bucket and tombstone remain in the isolated package-test server root.

Run it only against an isolated installed package, never a personal server:

```text
clipman-server-compat --mode package \
  --go-server /path/to/installed/clipman-server \
  --clipman-cli /path/to/clipman-cli \
  --server-url http://127.0.0.1:25766 \
  --test-root /isolated/.test-tmp-clipman-server-package-run \
  --token-file /isolated/.test-tmp-clipman-server-package-run/token.txt
```

The test root must already exist, its basename must begin `.test-tmp-clipman-server-package-`, and it must contain `clipman-package-test.json` with `{"purpose":"clipman-server-package-compat","version":1}`. The token file must be inside that root. These checks complement the loopback-only endpoint restriction and prevent a casual invocation against ordinary server state. Add `--ca-cert` for direct private-CA HTTPS. The JSON report records the seed, installed version, operation names, and dummy payload digest without recording the token, password, database ID, or temporary paths.

`historical-releases.json` pins three published Python package generations by release digest and byte-exact critical files: 2.1.1 (early standard installer), 2.4.3 (externally managed updates), and 2.6.3 (runit-aware updater). Supplying `--historical-packages DIR` validates all three original ZIPs, their legacy manifests, update capabilities, package layout, and critical file digests without extracting them. Set `CLIPMAN_HISTORICAL_PACKAGE_DIR` to the same directory when running the focused Go test.

On Windows, `scripts/windows-package-update-acceptance.ps1` creates the marker and an isolated installed layout, installs a generated manifest-v2 native archive through the real updater, runs package mode, forces an unhealthy program replacement, proves program and persistent-state rollback, restarts, and runs package mode again. The latest recorded result is in `windows-package-update-acceptance-latest.json`.

`scripts/windows-cli-acceptance.ps1` runs the larger released-client corpus in an isolated `.test-tmp-windows-cli-*` root. It uses two independent CLI profiles, creates at least 200 real encrypted records, checks bidirectional synchronization and mutations, verifies duplicate/template/query behavior, and performs a Go-to-Python-to-Go handoff over the same server bucket. The script refuses an existing or non-test root so it cannot operate on a normal Clipman data directory. The latest recorded result is in `windows-cli-acceptance-latest.json`.
