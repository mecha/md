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

// WriteOption configures Write. Use the option constructors below.
type WriteOption func(w *writeOptions)

type writeOptions struct {
	requireFile     bool
	exclusiveCreate bool
	createParents   bool
	filePerms       os.FileMode
	dirPerms        os.FileMode
}

var defaultOptions = writeOptions{filePerms: 0o644, dirPerms: 0o755}

// RequireFile makes Write fail if the target is missing. It cannot be combined
// with ExclusiveCreate(true). Parent creation is ignored when enabled.
func RequireFile(require bool) WriteOption { return func(w *writeOptions) { w.requireFile = require } }

// ExclusiveCreate prevents overwriting an existing target using OS-level
// exclusive creation. It cannot be combined with RequireFile(true).
func ExclusiveCreate(exclusive bool) WriteOption {
	return func(w *writeOptions) { w.exclusiveCreate = exclusive }
}

// CreateParents creates missing parent directories for new files.
func CreateParents(create bool) WriteOption {
	return func(w *writeOptions) { w.createParents = create }
}

// FilePerms sets new-file permissions, subject to umask (default 0644).
func FilePerms(perms os.FileMode) WriteOption { return func(w *writeOptions) { w.filePerms = perms } }

// DirPerms sets new-directory permissions, subject to umask (default 0755).
func DirPerms(perms os.FileMode) WriteOption { return func(w *writeOptions) { w.dirPerms = perms } }

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
	opts := defaultOptions
	for _, option := range options {
		if option == nil {
			return fmt.Errorf("%w: nil option", ErrInvalidOptions)
		}
		option(&opts)
	}
	if opts.requireFile && opts.exclusiveCreate {
		return fmt.Errorf("%w: require and exclusive are mutually exclusive", ErrInvalidOptions)
	}
	if opts.filePerms & ^os.FileMode(0o777) != 0 || opts.dirPerms & ^os.FileMode(0o777) != 0 {
		return fmt.Errorf("%w: permissions must be between 0000 and 0777", ErrInvalidOptions)
	}
	data, err := doc.MarshalText()
	if err != nil {
		return err
	}
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

// Patch updates an existing regular file. Semantically unchanged patches do
// not rewrite the file. There is no transaction across reading and writing.
func Patch(file string, patch DocumentPatch) error {
	// Validate the patch even when the target is missing.
	if _, err := patch.Apply(Document{}); err != nil {
		return err
	}
	doc, err := Read(file)
	if err != nil {
		return err
	}
	updated, err := patch.Apply(doc)
	if err != nil {
		return err
	}
	before, err := doc.MarshalText()
	if err != nil {
		return err
	}
	after, err := updated.MarshalText()
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	return Write(file, updated, RequireFile(true))
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
	defer func() { _ = temp.Close(); _ = os.Remove(temp.Name()) }()
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
