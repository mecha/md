# md

Read, write, and patch Markdown files with ordered YAML front matter.

## CLI

### Installation

```sh
go install github.com/mecha/md/cmd/md@latest
```

Ensure `GOBIN` (or `$GOPATH/bin`, usually `~/go/bin`) is on your `PATH`.

### Basic usage

```sh
# Create a note; fail if it already exists.
md write note.md --exclusive -y 'title=My note' -b '# Hello'

# Create or overwrite a note
md write note.md -y 'title=My note' -b '# Hello'

# Read as Markdown or structured JSON.
md read note.md
md read note.md --format json

# Update properties without replacing the body.
md patch note.md -y 'draft=false' -u obsolete

# Append to the body.
printf '\n## More\n' | md patch note.md --append --body-stdin
```

The `write` command replaces the whole document, while `patch` preserves any
unmentioned content. Reading Markdown normalizes YAML formatting rather than
copying bytes exactly, like `cat` would.

See the [CLI reference](docs/cli.md) for all options, property semantics, and
exit codes, or run `md --help`.

## Package

### Usage

```go
package main

import (
    "log"

    "github.com/mecha/md/pkg/md"
)

func main() {
    doc, err := md.Read("note.md")
    if err != nil {
        log.Fatal(err)
    }
    log.Print(doc.Body)

    err = md.Patch("note.md", md.DocumentPatch{
        SetProps: []md.Property{{Name: "draft", Value: false}},
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

See the [API guide](docs/api.md) for writing documents, in-memory patches,
serialization, options, and errors. See [file behavior and limits](docs/files.md)
for formatting and filesystem information.
