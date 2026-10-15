# md

Create and patch Markdown files with YAML front matter, from Go or the command line. Properties are schema-free YAML values; the Markdown body is preserved byte-for-byte unless explicitly replaced.

## Build and test

Use the Go toolchain specified in `go.mod`.

```sh
go build -o ./bin/md ./cmd/md
go install ./cmd/md
go test -race -cover ./...
go vet ./...
```

`go install` installs `md` in `GOBIN` (or `$GOPATH/bin`).

## CLI

```text
md FILE [options]

-p, --prop FIELD=YAML  Set a top-level property (repeatable)
    --unset FIELD     Remove a property (repeatable; ignored when creating)
-m, --mode MODE       auto (default), create, or patch
-b, --body MD         Replace the body with the supplied string
-f, --body-file FILE  Replace the body with file contents
-s, --body-stdin      Replace the body with standard input
-h, --help            Show help
```

Options can precede or follow `FILE`. Value-taking flags accept either separate arguments or `=VALUE`, including short flags (`-m=patch`). Short flags are not bundled. The three body options are mutually exclusive, including repeated uses. Omitting a body option preserves an existing body; `--body ''` clears it.

```sh
# Create a file, or patch it if it already exists.
md note.md -p 'title=My note' -p 'tags=[go, markdown]' -b '# Hello'

# Update properties without changing the body.
md note.md -m patch -p 'draft=false' --unset tags

# Read the replacement body from a file or stdin.
md note.md -f body.txt
printf '# Replacement\n' | md note.md -s

# Refuse to overwrite an existing file.
md new.md --mode create --prop 'title=New note'
```

Property values are parsed as YAML: numbers, booleans, null, lists, and mappings are supported. Quote YAML strings when necessary, e.g. `-p 'code="001"'`. `null` sets a null value; it does not remove the property. Prop/unset operations run in argument order, with unset ignored when creating. Nested values are replaced, not recursively merged; dots in property names are literal.

Assignments split at the first `=` unless the property name is YAML-quoted:

```sh
md note.md -p '"field=with=equals"=value=also=with=equals'
```

`--prop` replaces the prototype's `--set`; `--set` is no longer accepted.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success, including a no-op or help |
| 1 | Invalid arguments, malformed front matter, or other I/O failure |
| 2 | Creation refused because the target already exists |
| 3 | Patch target does not exist |

Success is silent; errors go to stderr.

## Go API

The package is `md/pkg/md` in this module:

```go
func Auto(file string, fields []Field, body *string) error
func Create(file string, fields []Field, body *string) error
func Patch(file string, fields []Field, body *string) error

type Field struct {
    Name  string
    Value any
    Unset bool
}
```

`Auto` chooses create or patch based on whether the target exists. `Create` never overwrites a target; `Patch` requires an existing regular file. Fields are ordered operations, allowing repeated assignments and removals. Values must be YAML-encodable. An unset operation ignores `Value` and is ignored entirely during creation.

```go
package main

import (
    "log"
    "md/pkg/md"
)

func main() {
    body := "# My note\n"
    err := md.Auto("note.md", []md.Field{
        {Name: "title", Value: "My note"},
        {Name: "tags", Value: []string{"go", "markdown"}},
        {Name: "obsolete", Unset: true},
    }, &body)
    if err != nil {
        log.Fatal(err)
    }

    // nil preserves the body. A pointer to an empty string clears it.
    if err := md.Patch("note.md", []md.Field{
        {Name: "draft", Value: false},
    }, nil); err != nil {
        log.Fatal(err)
    }
}
```

Errors support `errors.Is(err, fs.ErrExist)`, `errors.Is(err, fs.ErrNotExist)`, and `errors.Is(err, md.ErrInvalidFrontMatter)`.

## File behavior

- Front matter is recognized only at the beginning of a file, between `---` delimiter lines. Both LF and CRLF input are supported.
- Existing property order is retained. New properties are appended alphabetically; newly created front matter is sorted alphabetically. No application-specific schema or timestamps are imposed.
- Unmentioned properties and the body survive patches. Semantically unchanged patches do not rewrite the file, preserving its exact formatting and modification time.
- Changed front matter is re-encoded as YAML with LF delimiters. Comments, quoting, and original YAML formatting are not preserved on changed files.
- Files without properties are created without front matter. Removing the last property from existing front matter leaves an empty front-matter block.
- Parent directories must already exist. Creation is exclusive, using permissions `0644` subject to umask. Patches use a temporary file in the same directory and an atomic rename, preserving permission bits.
- Symlinks and non-regular patch targets are rejected. Atomic replacement does not preserve inode identity, hard-link relationships, ownership, or extended attributes. There is no locking or concurrent read-modify-write protection; callers must coordinate concurrent writers and use trusted paths.

## Layout

- `pkg/md`: public writing API and internal parsing/serialization.
- `cmd/md`: argument parsing, body input, and exit-code handling.
