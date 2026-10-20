// Package md reads, writes, and patches Markdown files with ordered YAML front
// matter. Property values are parsed data; merges are top-level only.
package md

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Read parses an existing regular file. Symlink targets are rejected.
func Read(file string) (Document, error) {
	if _, err := regularFile(file); err != nil {
		return Document{}, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Document{}, fmt.Errorf("read %q: %w", file, err)
	}
	doc, err := parse(data)
	if err != nil {
		return Document{}, fmt.Errorf("read %q: %w", file, err)
	}
	return doc, nil
}

// Write replaces the entire document, creating the target by default. Existing
// files are replaced atomically, preserving permissions. Options and contents
// are validated before filesystem mutation. Callers coordinate concurrent access.
func Write(file string, doc Document, options ...WriteOption) error {
	opts, err := resolveWriteOptions(options)
	if err != nil {
		return err
	}
	data, err := doc.MarshalText()
	if err != nil {
		return err
	}
	return writeFile(file, data, opts)
}

// writeFile writes already-serialized data using validated options.
func writeFile(file string, data []byte, opts writeOptions) error {
	info, err := os.Lstat(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect %q: %w", file, err)
	}
	if err == nil {
		if opts.exclusiveCreate {
			return fmt.Errorf("create %q: %w", file, fs.ErrExist)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("write %q: not a regular file", file)
		}
		if err := writeAtomic(file, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("write %q: %w", file, err)
		}
		return nil
	}
	if opts.requireFile {
		return fmt.Errorf("write %q: %w", file, fs.ErrNotExist)
	}
	if opts.createParents {
		if err := os.MkdirAll(filepath.Dir(file), opts.dirPerms); err != nil {
			return fmt.Errorf("create parents for %q: %w", file, err)
		}
	}
	if err := writeNew(file, data, opts.filePerms); err != nil {
		return fmt.Errorf("create %q: %w", file, err)
	}
	return nil
}

// Patch updates an existing file. Patches that result in no semantic changes do
// not rewrite the file.
func Patch(file string, patch DocumentPatch) error {
	// Validate the patch even when the target is missing.
	patch, err := patch.normalized()
	if err != nil {
		return err
	}
	doc, err := Read(file)
	if err != nil {
		return err
	}
	before, err := doc.marshalText()
	if err != nil {
		return err
	}
	updated := patch.applyTo(doc)
	after, err := updated.marshalText()
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	return writeFile(file, after, writeOptions{requireFile: true})
}

func regularFile(file string) (os.FileInfo, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, fmt.Errorf("inspect %q: %w", file, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("inspect %q: not a regular file", file)
	}
	return info, nil
}

func writeNew(path string, data []byte, perms os.FileMode) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perms)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func writeAtomic(path string, data []byte, permissions fs.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".md-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
	}()
	if err := temp.Chmod(permissions); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
