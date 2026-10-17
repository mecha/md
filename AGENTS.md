# Project context

`md` is a standalone Go library and CLI for Markdown files with YAML front matter.

## Read first

- [SPEC.md](SPEC.md) is the authoritative package and CLI contract. Read the
  relevant sections before changing behavior; flag discrepancies rather than
  silently redefining the contract.
- [README.md](README.md) covers usage, examples, build/test commands, limitations,
  and migration from the prototype. Do not restore legacy APIs or flags unless
  explicitly requested.
- [go.mod](go.mod) defines the module, toolchain requirement, and dependencies.

## Code map

- `pkg/md/document.go`: public document model, validation, parsing, serialization.
- `pkg/md/patch.go`: in-memory patch application.
- `pkg/md/md.go`: filesystem operations and write options.
- `internal/yamldata/`: shared YAML decoding and validation for files and CLI input.
- `cmd/md/`: CLI adapter; argument parsing in `cli.go`, property assignments in
  `property.go`, process entry point in `main.go`.
- Tests live beside their implementations; parser fuzzing is in
  `pkg/md/document_test.go`.

## Working conventions

- Keep document and filesystem semantics in the library, not duplicated in the CLI.
- For intentional contract changes, update SPEC.md, implementation, tests, CLI
  help, and README examples together as applicable.
- Add regression tests for behavior changes, including failure paths and absence
  of filesystem side effects. Use temporary directories for file tests.
- Format changed Go files with `gofmt`; run the build, race/coverage tests, and
  vet commands in README.md before finishing code changes. Report checks not run.
- In addition to vet, run `go fix -diff ./...` to check for modern Go idioms
  supported by the project's toolchain (e.g. integer-range loops, `maps.Copy`,
  and `WaitGroup.Go`). This previews changes without editing files and exits
  nonzero when fixes are suggested. Review and apply relevant suggestions,
  then rerun the checks; do not rely on compiler success or formatting alone.
- Keep this file a navigation and workflow guide; put API details in SPEC.md and
  user-facing guidance in README.md.
