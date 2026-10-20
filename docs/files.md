# File behavior and limits

- Front matter is recognized only at the beginning, between `---` delimiter
  lines. LF and CRLF input are accepted. Body bytes are not normalized.
- Serialization preserves property order but not YAML comments, quoting, or
  anchors. A document with no properties is emitted as body only. If that body
  begins with front-matter delimiters, a subsequent read will interpret them as
  front matter; no escaping block is inserted.
- No-op patches leave the original bytes, inode, and modification time intact.
- Symlink and non-regular file targets are rejected. Parent paths must be trusted.
- Exclusive creation uses an OS-level exclusive operation. Replacing a file uses
  an atomic rename and preserves permission bits, not inode identity, ownership,
  extended attributes, or hard-link relationships.
- There is no locking across read–modify–write. Coordinate concurrent writers
  externally. Directories created before a later failure are not rolled back.

