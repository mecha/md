# Go API

Import `github.com/mecha/md/pkg/md`.

| Function | Purpose |
| --- | --- |
| `Read(file string) (Document, error)` | Read an existing Markdown file |
| `Write(file string, doc Document, options ...WriteOption) error` | Create or replace a complete document |
| `Patch(file string, patch DocumentPatch) error` | Update an existing document |

`Document` contains ordered `Props []Property` and a `Body string`. Each
`Property` has a `Name string` and a `Value any`.

`doc.Prop(key string) (Property, bool)` returns the first property with an exact,
case-sensitive name match, or `Property{}, false` if absent. A null value still
returns `true`. The returned property is a shallow copy; nested values are shared
with the document.

`doc.PropOr(key, fallback)` infers the return type from the fallback:

```go
title := doc.PropOr("title", "Untitled") // string
draft := doc.PropOr("draft", false)     // bool
count := doc.PropOr("count", int64(0))  // int64
```

Missing, null, or type-mismatched values return the fallback. Values are not
converted or cloned: an `int64` property does not match an `int` fallback, and
returned maps and slices share their underlying data with the document. As with
`Prop`, names match exactly and the first match wins. Use an explicitly typed
fallback or type argument when passing `nil`.

## Example


```go
package main

import (
    "log"
    "github.com/mecha/md/pkg/md"
)

func main() {
    doc := md.Document{
        Props: []md.Property{
            {Name: "title", Value: "My note"},
            {Name: "tags", Value: []string{"go", "markdown"}},
        },
        Body: "# Hello\n",
    }

    if err := md.Write("note.md", doc, md.ExclusiveCreate(true)); err != nil {
        log.Fatal(err)
    }

    if err := md.Patch("note.md", md.DocumentPatch{
        SetProps: []md.Property{{Name: "draft", Value: false}},
        UnsetProps: []string{"obsolete"},
        Body: "\n## More\n",
        BodyOperation: md.AppendBody,
    }); err != nil {
        log.Fatal(err)
    }

    doc, err := md.Read("note.md")
    if err != nil {
        log.Fatal(err)
    }
    normalized, err := doc.MarshalText()
    if err != nil {
        log.Fatal(err)
    }
    _ = normalized
}
```

`DocumentPatch.Apply(doc)` returns an independent document and an error, without
I/O or mutation of either input. A zero-value patch does nothing. Body operations
are `KeepBody`, `ReplaceBody`, `AppendBody`, `PrependBody`, and `DeleteBody`.

Write options are `RequireFile(bool)`, `ExclusiveCreate(bool)`,
`CreateParents(bool)`, `FilePerms(os.FileMode)`, and `DirPerms(os.FileMode)`.
Repeated options use the last value.

`Document.MarshalText()` produces Markdown. `json.Marshal(doc)` and
`yaml.Marshal(doc)` produce the structured representation shown above.

Property values support data scalars, sequences, mappings, and `time.Time`
timestamps. Concrete numeric/container Go types may be normalized. Arbitrary
objects, pointers, unsupported YAML tags, cycles, duplicate property names, and
invalid values are rejected. Alias values are resolved; YAML merge-key syntax
(`<<`) is not supported. Nesting is limited to 100 levels.

Errors support `errors.Is` with `fs.ErrExist`, `fs.ErrNotExist`,
`md.ErrInvalidFrontMatter`, `md.ErrInvalidDocument`, `md.ErrInvalidPatch`, and
`md.ErrInvalidOptions`.

## Write behavior

By default, `Write` creates or replaces the target. Omitted properties and body
are empty. `RequireFile(true)` fails if the target is missing;
`ExclusiveCreate(true)` fails if it exists. These options are mutually exclusive.
`CreateParents(true)` creates missing parents, but is ignored with require.
New file and directory permissions default to `0644` and `0755`, respectively,
and are subject to umask. Permission options accept only bits `0000`–`0777`.
Nil options are invalid.

## Patch behavior

Sets are applied in slice order, followed by unsets. Updating a property retains
its position; new properties append. Repeated sets use the last value without
moving the property. Unsets win over sets; unsetting a missing property is a
no-op. Nested values are replaced rather than recursively merged.

| Body operation | Result |
| --- | --- |
| `KeepBody` | Preserve body; ignore patch body |
| `ReplaceBody` | Replace with patch body, including empty content |
| `AppendBody` | Existing body followed by patch body |
| `PrependBody` | Patch body followed by existing body |
| `DeleteBody` | Empty body; ignore patch body |

Append and prepend add no separator. Unknown operations are errors.

See [file behavior and limits](files.md) for serialization and filesystem
constraints.
