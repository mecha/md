package md

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func readDocument(t *testing.T, path string) document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestPublicAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	body := "# Hello\r\n\x00"
	fields := []Field{{Name: "title", Value: "Hello"}, {Name: "nested", Value: map[string]any{"list": []any{1, true}}}, {Name: "null", Value: nil}, {Name: "title", Unset: true}}
	if err := Create(path, fields, &body); err != nil {
		t.Fatal(err)
	}
	doc := readDocument(t, path)
	if doc.Body != body || doc.Fields["title"] != "Hello" {
		t.Fatalf("created: %#v", doc)
	}
	if value, ok := doc.Fields["null"]; !ok || value != nil {
		t.Fatalf("null: %#v", doc.Fields)
	}
	if err := Create(path, nil, nil); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Create existing: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Patch(path, []Field{{Name: "title", Unset: true}, {Name: "new", Value: 1}, {Name: "new", Unset: true}, {Name: "new", Value: 2}}, nil); err != nil {
		t.Fatal(err)
	}
	patched := readDocument(t, path)
	if _, ok := patched.Fields["title"]; ok {
		t.Fatal("title not removed")
	}
	if patched.Body != body || patched.Fields["new"] != 2 || !reflect.DeepEqual(patched.Fields["nested"], doc.Fields["nested"]) {
		t.Fatalf("patched: %#v", patched)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	empty := ""
	if err := Auto(path, nil, &empty); err != nil {
		t.Fatal(err)
	}
	if readDocument(t, path).Body != "" {
		t.Fatal("body not cleared")
	}
	missing := filepath.Join(t.TempDir(), "new.md")
	if err := Patch(missing, nil, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Patch missing: %v", err)
	}
	if err := Auto(missing, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(missing)
	if err != nil || len(data) != 0 {
		t.Fatalf("empty create: %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files left: %v, %v", entries, err)
	}
}

func TestPatchNoOpPreservesBytesAndIdentity(t *testing.T) {
	for _, fields := range [][]Field{nil, {{Name: "title", Value: "Keep"}}, {{Name: "absent", Unset: true}}} {
		path := filepath.Join(t.TempDir(), "note.md")
		original := []byte("---\r\n# comment\r\ntitle: 'Keep'\r\n---\r\nbody\r\n")
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := Patch(path, fields, nil); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, original) || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
			t.Fatal("no-op rewrote file")
		}
	}
}

func TestPatchFrontMatterAndBodies(t *testing.T) {
	tests := []struct {
		name, original string
		fields         []Field
		body           *string
		want           string
	}{
		{name: "remove last property", original: "---\na: 1\n---\nbody", fields: []Field{{Name: "a", Unset: true}}, want: "---\n---\nbody"},
		{name: "add frontmatter", original: "body\x00", fields: []Field{{Name: "a", Value: 1}}, want: "---\na: 1\n---\nbody\x00"},
		{name: "plain body", original: "old", body: new("new"), want: "new"},
		{name: "empty frontmatter", original: "---\n---\nold", body: new("new"), want: "---\n---\nnew"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note.md")
			if err := os.WriteFile(path, []byte(tt.original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := Patch(path, tt.fields, tt.body); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != tt.want {
				t.Fatalf("got %q, %v; want %q", data, err, tt.want)
			}
		})
	}
}

func TestFileErrors(t *testing.T) {
	dir := t.TempDir()
	for _, apply := range []func(string, []Field, *string) error{Patch, Auto} {
		if err := apply(dir, nil, nil); err == nil {
			t.Fatal("directory accepted")
		}
	}
	path := filepath.Join(dir, "bad.md")
	original := "---\ninvalid: [\n---\nbody"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Patch(path, []Field{{Name: "a", Value: 1}}, nil); !errors.Is(err, ErrInvalidFrontMatter) {
		t.Fatalf("malformed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatal("failed patch modified file")
	}
	for _, target := range []string{path, filepath.Join(dir, "missing")} {
		link := filepath.Join(t.TempDir(), "link.md")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := Create(link, nil, nil); !errors.Is(err, fs.ErrExist) {
			t.Fatalf("create symlink: %v", err)
		}
		if err := Auto(link, nil, nil); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	if err := Create(filepath.Join(dir, "missing", "note.md"), nil, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing parent: %v", err)
	}
	if err := Auto(filepath.Join(path, "note.md"), nil, nil); err == nil {
		t.Fatal("invalid parent accepted")
	}
}

type brokenValue struct{}

func (brokenValue) MarshalYAML() (any, error) { return nil, errors.New("cannot encode") }

func TestMarshalFailureDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	fields := []Field{{Name: "bad", Value: brokenValue{}}}
	if err := Create(path, fields, nil); err == nil {
		t.Fatal("expected marshal error")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("created file: %v", err)
	}
	if err := Create(path, nil, new("original")); err != nil {
		t.Fatal(err)
	}
	if err := Patch(path, fields, nil); err == nil {
		t.Fatal("expected marshal error")
	}
	if readDocument(t, path).Body != "original" {
		t.Fatal("changed file")
	}
}
