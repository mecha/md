# Project context

`md` is a standalone Go library and CLI for Markdown files with YAML front matter.

## Read first

- [README.md](README.md) covers installation and basic CLI/package usage.
- [docs/cli.md](docs/cli.md), [docs/api.md](docs/api.md), and
  [docs/files.md](docs/files.md) document CLI semantics, the Go API,
  and shared behavior and limits. Read the relevant sections before changing
  behavior; flag discrepancies rather than silently redefining the contract.
- Do not restore legacy APIs or flags unless explicitly requested.
- [go.mod](go.mod) defines the module, toolchain requirement, and dependencies.

## Code map

- `pkg/md/document.go`: public document model and serialization.
- `pkg/md/frontmatter.go`: front-matter parsing and delimiter handling.
- `pkg/md/value.go`: value normalization, validation, and independent copies.
- `pkg/md/patch.go`: patch validation and in-memory application.
- `pkg/md/md.go`: filesystem operations; write options are in `options.go`.
- `internal/yamldata/`: shared YAML decoding and validation for files and CLI input.
- `cmd/md/`: CLI adapter; argument parsing in `cli.go`, property assignments in
  `property.go`, process entry point in `main.go`.
- Tests live beside their implementations; parser fuzzing is in
  `pkg/md/document_test.go`.

## Build and checks

Use the Go toolchain specified in `go.mod`.

```sh
go build -o ./bin/md ./cmd/md
go test -race -cover ./...
go vet ./...
go fix -diff ./...
```

For a local CLI install, run `go install ./cmd/md`.

## Working conventions

- Keep document and filesystem semantics in the library, not duplicated in the CLI.
- For intentional contract changes, update the relevant docs, implementation,
  tests, CLI help, and README examples together as applicable.
- Add regression tests for behavior changes, including failure paths and absence
  of filesystem side effects. Use temporary directories for file tests.
- Format changed Go files with `gofmt`; run the build, race/coverage tests, and
  vet commands above before finishing code changes. Report checks not run.
- In addition to vet, run `go fix -diff ./...` to check for modern Go idioms
  supported by the project's toolchain (e.g. integer-range loops, `maps.Copy`,
  and `WaitGroup.Go`). This previews changes without editing files and exits
  nonzero when fixes are suggested. Review and apply relevant suggestions,
  then rerun the checks; do not rely on compiler success or formatting alone.
- Keep this file a navigation and workflow guide; put detailed reference material
  in docs/ and keep README.md focused on getting started.
