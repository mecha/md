# CLI reference

```text
md read FILE [OPTIONS]
md write FILE [OPTIONS]
md patch FILE [OPTIONS]
```

Use `md --help` or `md COMMAND --help` for help. Options can precede or follow
`FILE` after the command. Value-taking flags accept separate arguments or
`=VALUE`. Use `--` before a filename beginning with `-`. Short flags are not
bundled. `-` is a literal filename, not implicit stdin/stdout.

### Read

```sh
md read note.md                  # Validate and print normalized Markdown
md read note.md -f yaml          # Structured YAML
md read note.md --format=json    # Pretty-printed JSON
md read note.md -f json -c       # Compact JSON (also --compact)
```

`-f, --format` accepts `raw` (default), `yaml`, or `json`. Raw output is
read-and-serialize, not a byte-for-byte copy: YAML comments and presentation are
removed, property order is retained, and the body is unchanged.

JSON is pretty-printed by default. `-c, --compact` emits single-line JSON with a
trailing newline and requires `-f json`; it is rejected with raw or YAML output.
This read option does not change `patch -c`, which still clears the body.

Both structured formats separate the body from an ordered property list:

```json
{
  "props": [
    {"name": "title", "value": "My note"},
    {"name": "draft", "value": false}
  ],
  "body": "# Hello\n"
}
```

Empty properties serialize as `[]`. JSON output fails rather than silently
coercing non-string mapping keys, non-finite numbers, or invalid UTF-8.

### Write

**Write replaces the whole document.** Omitted properties and body are empty.

```sh
# Create exclusively: never overwrite an existing target.
md write note.md -x -y 'title=My note' -y 'tags=[go, markdown]' -b '# Hello'

# Replace an existing file; fail if it is missing.
md write note.md --require --body-file body.txt

# Create missing parents and restrict permissions for new paths.
md write notes/private/note.md --parents --file-perms 0600 --dir-perms 0700
```

| Option | Meaning |
| --- | --- |
| `-y, --yaml NAME=YAML` | Set a property; repeatable |
| `-b, --body MD` | Body from an argument |
| `-f, --body-file FILE` | Body from a file |
| `-s, --body-stdin` | Body from stdin |
| `-r, --require` | Fail if the target is missing |
| `-x, --exclusive` | Fail if the target already exists |
| `-p, --parents` | Create missing parent directories |
| `--file-perms PERM` | New file permissions in octal; default `0644` |
| `--dir-perms PERM` | New directory permissions in octal; default `0755` |

By default, write creates or replaces the target. Require and exclusive are
mutually exclusive; parents is ignored with require. Permission settings apply
only to newly created paths and are subject to umask.

### Patch

Patch requires an existing file. Unmentioned properties and the body survive.

```sh
md patch note.md -y 'draft=false' -u obsolete
printf '\n## More\n' | md patch note.md --append --body-stdin
md patch note.md --prepend --body '# Introduction'
md patch note.md --clear
```

| Option | Meaning |
| --- | --- |
| `-y, --yaml NAME=YAML` | Set a property; repeatable |
| `-u, --unset NAME` | Remove a property; repeatable |
| `-b, --body MD` | Patch body from an argument |
| `-f, --body-file FILE` | Patch body from a file |
| `-s, --body-stdin` | Patch body from stdin |
| `-c, --clear` | Clear the body |
| `-p, --prepend` | Prepend patch body without a separator |
| `-a, --append` | Append patch body without a separator |

A body source alone replaces the body; omitting it preserves the body. Append
and prepend require a body source. Clear, append, and prepend are mutually
exclusive. Clear cannot accompany a body source. Body sources are mutually
exclusive, including repeated use of the same option. `--body ''` explicitly
replaces the body with an empty string.

### Property semantics

Values are parsed as YAML, not stored as source strings:

```sh
md patch note.md -y 'count=3' -y 'draft=false' -y 'tags=[go, markdown]'
md patch note.md -y 'config={theme: dark, retries: 2}'
md patch note.md -y 'code="001"' -y 'nullable=null'
md patch note.md -y '"field=with=equals"=value=also=with=equals'
```

- Null (including an empty assignment) sets a null value; use unset to remove it.
- Names are nonempty strings. Dots in names are literal.
- Updating a property retains its position. New properties append in argument
  order. Repeated assignments use the last value without moving the property.
- All sets happen **before all unsets**, regardless of argument order. Unset wins.
- Nested values are replaced, not recursively merged.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success, including help and no-op patches |
| 1 | Invalid input, validation failure, or other I/O error |
| 2 | Exclusive creation refused: target exists |
| 3 | Missing required target: read, patch, or write --require |

A missing body-input file or missing parent during ordinary creation is code 1.
Read output and help go to stdout; errors go to stderr. Write and patch are
silent on success.

See also [file behavior and limits](files.md) and the [Go API](api.md).
