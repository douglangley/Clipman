# Clipman Server compatibility harness

`clipman-server-compat` is test tooling and is not included in production server packages.

It supports the four modes defined by `GO_SERVER_REWRITE_PLAN.md`:

- `reference`: probe the Python server and the built Clipman CLI;
- `differential`: probe Python and Go server executables;
- `go-only`: validate the Go executable after Python retirement;
- `package`: validate an installed or wrapped server package.

The initial implementation validates the coverage manifest and executable/version wiring. Protocol scenarios are added alongside the corresponding Go server slices.

`scripts/windows-cli-acceptance.ps1` runs the larger released-client corpus in an isolated `.test-tmp-windows-cli-*` root. It uses two independent CLI profiles, creates at least 200 real encrypted records, checks bidirectional synchronization and mutations, verifies duplicate/template/query behavior, and performs a Go-to-Python-to-Go handoff over the same server bucket. The script refuses an existing or non-test root so it cannot operate on a normal Clipman data directory. The latest recorded result is in `windows-cli-acceptance-latest.json`.
