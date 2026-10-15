// Package md creates and patches Markdown files with YAML front matter.
package md

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
)

// Field is a top-level property operation. Operations are applied in order.
// Unset removes the property in Patch and is ignored in Create.
// Value may be any YAML-encodable value; nil represents YAML null.
type Field struct {
	Name  string
	Value any
	Unset bool
}

type mode uint8

const (
	modeAuto mode = iota
	modeCreate
	modePatch
)

// Auto patches file if it exists, otherwise creates it.
func Auto(file string, fields []Field, body *string) error {
	return apply(file, fields, body, modeAuto)
}

// Create creates file exclusively, returning an error wrapping fs.ErrExist if
// it already exists. A nil body produces an empty body. Parents must exist.
func Create(file string, fields []Field, body *string) error {
	return apply(file, fields, body, modeCreate)
}

// Patch updates an existing regular file, returning an error wrapping
// fs.ErrNotExist if missing. A nil body preserves the body; a pointer to ""
// clears it. Unmentioned properties are preserved. Unchanged files are not
// rewritten. Changed files are replaced atomically, preserving permissions.
func Patch(file string, fields []Field, body *string) error {
	return apply(file, fields, body, modePatch)
}

func apply(file string, fields []Field, body *string, requested mode) error {
	info, statErr := os.Lstat(file)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("inspect %q: %w", file, statErr)
	}

	effectiveMode := requested
	if effectiveMode == modeAuto {
		if exists {
			effectiveMode = modePatch
		} else {
			effectiveMode = modeCreate
		}
	}
	if effectiveMode == modeCreate && exists {
		return fmt.Errorf("create %q: %w", file, fs.ErrExist)
	}
	if effectiveMode == modePatch && !exists {
		return fmt.Errorf("patch %q: %w", file, fs.ErrNotExist)
	}

	var err error
	var original []byte
	doc := document{Fields: make(map[string]any)}
	hadFrontmatter := false
	var originalFields map[string]any
	var originalBody string
	if effectiveMode == modePatch {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("patch %q: not a regular file", file)
		}
		original, err = os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %q: %w", file, err)
		}
		doc, err = parse(original)
		if err != nil {
			return fmt.Errorf("parse %q: %w", file, err)
		}
		hadFrontmatter = hasFrontmatter(original)
		originalFields = make(map[string]any, len(doc.Fields))
		maps.Copy(originalFields, doc.Fields)
		originalBody = doc.Body
	}

	for _, operation := range fields {
		if operation.Unset {
			if effectiveMode == modePatch {
				delete(doc.Fields, operation.Name)
			}
			continue
		}
		doc.Fields[operation.Name] = operation.Value
	}
	if body != nil {
		doc.Body = *body
	}
	if effectiveMode == modePatch && reflect.DeepEqual(doc.Fields, originalFields) && doc.Body == originalBody {
		return nil
	}

	data, err := marshalDocument(doc, hadFrontmatter)
	if err != nil {
		return err
	}
	if effectiveMode == modePatch && bytes.Equal(data, original) {
		return nil
	}
	if effectiveMode == modeCreate {
		if err := writeNew(file, data); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("create %q: %w", file, fs.ErrExist)
			}
			return fmt.Errorf("create %q: %w", file, err)
		}
		return nil
	}
	if err := writeAtomic(file, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("patch %q: %w", file, err)
	}
	return nil
}

func hasFrontmatter(data []byte) bool {
	return bytes.HasPrefix(data, []byte("---\n")) || bytes.HasPrefix(data, []byte("---\r\n"))
}

func marshalDocument(doc document, preserveEmptyFrontmatter bool) ([]byte, error) {
	if len(doc.Fields) == 0 {
		if preserveEmptyFrontmatter {
			return []byte("---\n---\n" + doc.Body), nil
		}
		return []byte(doc.Body), nil
	}
	data, err := doc.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal document: %w", err)
	}
	return data, nil
}

func writeNew(path string, data []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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

func writeAtomic(path string, data []byte, permissions fs.FileMode) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".md-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
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
	return os.Rename(tempName, path)
}
