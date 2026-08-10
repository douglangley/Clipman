# Clipman Server compatibility harness

`clipman-server-compat` is test tooling and is not included in production server packages.

It supports the four modes defined by `GO_SERVER_REWRITE_PLAN.md`:

- `reference`: probe the Python server and the built Clipman CLI;
- `differential`: probe Python and Go server executables;
- `go-only`: validate the Go executable after Python retirement;
- `package`: validate an installed or wrapped server package.

The initial implementation validates the coverage manifest and executable/version wiring. Protocol scenarios are added alongside the corresponding Go server slices.
