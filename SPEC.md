# md specification

## Scope

`md` reads, writes, and patches Markdown files with optional YAML front matter.
Properties are ordered and schema-free. All merges are top-level only.
This API replaces `Auto`, `Create`, and the previous `Patch` signature. The CLI
requires a `read`, `write`, or `patch` command; legacy flags are not supported.

## Package API

```go
package md

import "os"

type Document struct {
    Props []Property `json:"props" yaml:"props"`
    Body  string     `json:"body" yaml:"body"`
}

type Property struct {
    Name  string `json:"name" yaml:"name"`
    Value any    `json:"value" yaml:"value"`
}

type DocumentPatch struct {
    SetProps      []Property
    UnsetProps    []string
    Body          string
    BodyOperation BodyOperation
}

type BodyOperation int

const (
    KeepBody BodyOperation = iota
    ReplaceBody
    AppendBody
    PrependBody
    DeleteBody
)

func (p DocumentPatch) Apply(d Document) (Document, error)
func Read(file string) (Document, error)
func Write(file string, doc Document, options ...WriteOption) error
func Patch(file string, patch DocumentPatch) error

// MarshalText serializes normalized Markdown; MarshalJSON and MarshalYAML
// serialize the structured {props: [...], body: ...} representation.
func (d Document) MarshalText() ([]byte, error)
func (d Document) MarshalJSON() ([]byte, error)
func (d Document) MarshalYAML() (any, error)

type WriteOption func(w *writeOptions)

type writeOptions struct {
    requireFile     bool
    exclusiveCreate bool
    createParents   bool
    filePerms       os.FileMode
    dirPerms        os.FileMode
}

var defaultOptions = writeOptions{
    requireFile:     false,
    exclusiveCreate: false,
    createParents:   false,
    filePerms:       0o644,
    dirPerms:        0o755,
}

func RequireFile(require bool) WriteOption
func ExclusiveCreate(exclusive bool) WriteOption
func CreateParents(create bool) WriteOption
func FilePerms(perms os.FileMode) WriteOption
func DirPerms(perms os.FileMode) WriteOption
```

### Documents and validation

- Names are nonempty strings, unique within a `Document`.
- Values are parsed YAML data: null, strings, booleans, numbers, sequences,
  mappings, and timestamps. Null is distinct from an absent property.
- Nested values are supported. Mapping keys must be representable as comparable
  scalar Go values. YAML aliases resolve to values; cycles are rejected.
- Comments, quoting, anchor names, and other YAML presentation details are not
  part of the model. Unsupported YAML tags, including YAML merge keys (`<<`),
  are rejected rather than discarded. Nesting is limited to 100 levels.
- Go callers provide data values, not pointers, channels, functions, arbitrary
  structs, or objects requiring custom serialization. `time.Time` is supported
  for YAML timestamps. Scalar, slice, array, and map values are normalized to
  ordinary data values; concrete numeric/container Go types need not survive.
- `Read`, `Apply`, and serialization validate their inputs. Invalid Go values
  cannot be prevented by `any`, but are rejected before filesystem mutation.
- `Apply` returns an independent document, including nested containers, and
  does not mutate either input.

### Patching

Apply sets in slice order, then unsets, then the body operation.

- Updating a property preserves its position.
- New properties append in first-occurrence order.
- Repeated sets use the last value without moving the property.
- Unsetting a missing property is a no-op; repeated unsets are harmless.
- Unsets win when a name appears in both lists, regardless of CLI argument order.
- Unset names must be nonempty.
- Nested values are replaced entirely, not recursively merged.

| Operation | Result |
| --- | --- |
| KeepBody | Preserve body; ignore patch body |
| ReplaceBody | Replace with patch body, including empty content |
| AppendBody | Existing body followed by patch body |
| PrependBody | Patch body followed by existing body |
| DeleteBody | Empty body; ignore patch body |

Append/prepend add no delimiter or newline. The zero-value patch is a no-op.
Unknown body operations are errors.

### Reading and serialization

- Only a leading `---` delimiter line starts front matter. A closing `---` line
  is required. LF and CRLF delimiters and a closing delimiter at EOF are accepted.
- Front matter must be empty (including comments only) or a mapping with unique,
  nonempty string keys. Multiple YAML documents and duplicate mapping keys are
  rejected. Explicit scalar null is not an empty front-matter mapping.
- Files without front matter are entirely body. Body bytes are never normalized.
- Markdown serialization preserves `Props` order, emits normalized YAML with LF
  delimiters, and adds no application-specific properties or timestamps.
- No properties means no front-matter block, including after removing the last
  property. A no-op file patch remains untouched even if its existing format
  differs from normalized serialization. With no properties, a body beginning
  with a front-matter delimiter is emitted literally; reading that output will
  interpret the delimiter as front matter. No escaping block is inserted.
- Structured JSON/YAML always use `{props: [...], body: ...}`. Empty properties
  serialize as `[]`, not null. Values are decoded values, not YAML source strings.
- JSON rejects values it cannot faithfully represent, including non-string
  mapping keys, non-finite numbers, and invalid UTF-8 text. Timestamps use RFC3339
  strings. Raw Markdown preserves arbitrary body bytes; YAML escapes body control
  characters when representable, and rejects invalid UTF-8.

### Filesystem behavior

| Options | Missing target | Existing target |
| --- | --- | --- |
| Defaults | Create | Replace |
| RequireFile(true) | Error | Replace |
| ExclusiveCreate(true) | Create exclusively | Error |

- RequireFile and ExclusiveCreate together are invalid.
- Repeated options use the last value; nil options are invalid.
- CreateParents creates missing parents unless RequireFile is active, in which
  case it is ignored. Created directories are not rolled back after a failure.
- Validate document and options before filesystem mutation.
- Exclusive creation uses an OS-level exclusive operation, not only a precheck.
- New file/directory permissions default to 0644/0755 and are subject to umask.
  Permission options accept only ordinary bits (0000–0777).
- Existing file permissions are preserved; directory permissions are unchanged.
- Replacements use a temporary file in the same directory and atomic rename.
- Symlinks and non-regular file targets are rejected. Exclusive mode reports
  an existing target (including a symlink) as already existing.
- Read and Patch require existing regular files. Patch never creates parents.
- Semantically unchanged patches do not rewrite files. Property order is semantic;
  YAML presentation and concrete Go numeric types are not.
- There is no locking or transaction across read–modify–write. Callers coordinate
  concurrent writers and use trusted paths. Atomic replacement prevents partial
  replacement content, not lost updates. It does not preserve inode identity,
  hard-link relationships, ownership, or extended attributes.

Errors support `errors.Is` with `fs.ErrExist`, `fs.ErrNotExist`,
`md.ErrInvalidFrontMatter`, `md.ErrInvalidDocument`, `md.ErrInvalidPatch`, and
`md.ErrInvalidOptions`, as appropriate. Invalid patch property values may wrap
both ErrInvalidPatch and ErrInvalidDocument.

## CLI

```text
usage: md COMMAND FILE [OPTIONS]

Commands:
  read                  Parse and print a document
  write                 Write a complete document
  patch                 Modify an existing document

Read options:
  -f, --format FORMAT   Output format: raw (default), yaml, json

Write options:
  -y, --yaml NAME=YAML   Set a front-matter property; repeatable
  -b, --body MD          Read the body from an argument
  -f, --body-file FILE   Read the body from a file
  -s, --body-stdin       Read the body from standard input
  -r, --require          Require an existing target
  -x, --exclusive        Create only; fail if the target exists
  -p, --parents          Create missing parent directories
      --file-perms PERM  New file permissions in octal; default 0644
      --dir-perms PERM   New directory permissions in octal; default 0755

Patch options:
  -y, --yaml NAME=YAML   Set a front-matter property; repeatable
  -u, --unset NAME       Remove a front-matter property; repeatable
  -b, --body MD          Read patch content from an argument
  -f, --body-file FILE   Read patch content from a file
  -s, --body-stdin       Read patch content from standard input
  -c, --clear            Clear the body
  -p, --prepend          Prepend patch content without a separator
  -a, --append           Append patch content without a separator

Global options:
  -h, --help             Show general or command-specific help
```

### Arguments

- Command comes first; command options may precede or follow FILE.
- Exactly one file is required except for help. Options are command-specific.
- Value-taking flags accept separate arguments or `=VALUE`, including short
  flags. Short flags are not bundled; boolean flags do not accept values.
- `--` ends option parsing, allowing filenames beginning with `-`.
- `FILE=-` is literal; stdin is used only via --body-stdin.
- Body sources are mutually exclusive, including repeated use of one source.
- Each NAME=YAML argument contains exactly one YAML value; empty means null.
  Assignments split at the first `=` unless the name is YAML-quoted:
  `--yaml '"field=with=equals"=value=also=with=equals'`.
- Invalid input is rejected before output or filesystem mutation.
- Permissions consist only of octal digits, optionally with leading zeros,
  with a value no greater than 0777.

### Commands

`read` parses, validates, and serializes. Raw output is normalized Markdown, not
an exact copy of the input. YAML and JSON output use the ordered structured
representation, e.g.:

```json
{"props":[{"name":"title","value":"My note"},{"name":"draft","value":false}],"body":"# Hello\n"}
```

`write` writes an entire document: omitted properties mean none; an omitted body
means empty. Repeated assignments retain the first position and last value.
Require and exclusive flags are mutually exclusive; parents is ignored with
require. It is not a merge operation.

`patch` requires an existing target. No body source or operation preserves the
body; a body source alone replaces it. Append/prepend require a body source.
Clear/append/prepend are mutually exclusive, including repeated uses. Clear
cannot be combined with a body source. Sets precede unsets regardless of
argument order. No requested changes succeeds without rewriting.

### Output and exit codes

Read content and help go to stdout; write/patch are silent on success. Errors go
to stderr. Output is fully serialized before anything is written to stdout.

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Invalid arguments, validation failure, or other I/O error |
| 2 | Exclusive creation refused because target exists |
| 3 | Required target missing: read, patch, or write --require |

A missing body-input file or a missing parent during ordinary creation is an I/O
error (1), not a missing-required-target error.
