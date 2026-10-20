package md

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func mustRead(t *testing.T, path string) Document {
	t.Helper()
	doc, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWriteModes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		options  []WriteOption
		existing bool
		want     error
	}{
		{"default new", nil, false, nil}, {"default replace", nil, true, nil},
		{"required new", []WriteOption{RequireFile(true)}, false, fs.ErrNotExist},
		{"required existing", []WriteOption{RequireFile(true)}, true, nil},
		{"exclusive new", []WriteOption{ExclusiveCreate(true)}, false, nil},
		{"exclusive existing", []WriteOption{ExclusiveCreate(true)}, true, fs.ErrExist},
		{"last option wins", []WriteOption{RequireFile(true), RequireFile(false), ExclusiveCreate(true)}, false, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note.md")
			if tt.existing {
				mustWrite(t, path, "original")
			}
			err := Write(path, Document{Body: "replacement"}, tt.options...)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Write: %v, want %v", err, tt.want)
			}
			if tt.want == nil && mustRead(t, path).Body != "replacement" {
				t.Fatal("not written")
			}
			if tt.want != nil && tt.existing && mustRead(t, path).Body != "original" {
				t.Fatal("modified on failure")
			}
		})
	}
}

func TestPermissionsAndParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "note.md")
	if err := Write(path, Document{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing parent: %v", err)
	}
	if err := Write(path, Document{}, RequireFile(true), CreateParents(true)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("required: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("required write created parents")
	}
	if err := Write(path, Document{Body: "new"}, CreateParents(true), FilePerms(0o600), DirPerms(0o700)); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]fs.FileMode{path: 0o600, filepath.Dir(path): 0o700, filepath.Join(dir, "a"): 0o700} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("permissions %s: %v %v", file, info, err)
		}
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, Document{Body: "replace"}, FilePerms(0o777), DirPerms(0o777), CreateParents(true)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("existing mode: %v %v", info, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files left: %v %v", entries, err)
	}
}

func TestInvalidWritesHaveNoSideEffects(t *testing.T) {
	for _, options := range [][]WriteOption{
		{nil}, {RequireFile(true), ExclusiveCreate(true)}, {FilePerms(0o1000)}, {DirPerms(os.ModeDir)},
	} {
		path := filepath.Join(t.TempDir(), "parent", "note.md")
		options = append(options, CreateParents(true))
		if err := Write(path, Document{}, options...); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options: %v", err)
		}
		if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("created parents")
		}
	}
	path := filepath.Join(t.TempDir(), "parent", "note.md")
	invalid := Document{Props: []Property{{"a", make(chan int)}}}
	if err := Write(path, invalid, CreateParents(true)); !errors.Is(err, ErrInvalidDocument) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("created parents for invalid document")
	}
	path = filepath.Join(t.TempDir(), "existing.md")
	mustWrite(t, path, "original")
	if err := Write(path, invalid); !errors.Is(err, ErrInvalidDocument) || mustRead(t, path).Body != "original" {
		t.Fatal("invalid write modified target")
	}
}

func TestFileTargetErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.md")
	mustWrite(t, file, "body")
	missing := filepath.Join(dir, "missing.md")
	if _, err := Read(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read missing: %v", err)
	}
	if err := Patch(missing, DocumentPatch{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("patch missing: %v", err)
	}
	if err := Patch(missing, DocumentPatch{BodyOperation: 999}); !errors.Is(err, ErrInvalidPatch) {
		t.Fatal(err)
	}
	targets := []string{dir, filepath.Join(file, "child")}
	for _, destination := range []string{file, missing} {
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(destination, link); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, link)
		if err := Write(link, Document{}, ExclusiveCreate(true)); !errors.Is(err, fs.ErrExist) {
			t.Fatalf("exclusive link: %v", err)
		}
	}
	for _, target := range targets {
		if _, err := Read(target); err == nil {
			t.Errorf("read accepted %s", target)
		}
		if err := Write(target, Document{}); err == nil {
			t.Errorf("write accepted %s", target)
		}
		if err := Patch(target, DocumentPatch{}); err == nil {
			t.Errorf("patch accepted %s", target)
		}
	}
	bad := filepath.Join(dir, "bad.md")
	for _, original := range []string{
		"---\na: [\n---\nbody",
		"---\na: {1: first, 0x1: second}\n---\nbody",
	} {
		mustWrite(t, bad, original)
		if err := Patch(bad, DocumentPatch{BodyOperation: DeleteBody}); !errors.Is(err, ErrInvalidFrontMatter) {
			t.Fatalf("invalid front matter: %v", err)
		}
		data, err := os.ReadFile(bad)
		if err != nil || string(data) != original {
			t.Fatal("bad file changed")
		}
	}
	// Write intentionally replaces even malformed existing content.
	if err := Write(bad, Document{Body: "fixed"}); err != nil {
		t.Fatal(err)
	}
}

func TestPatchFiles(t *testing.T) {
	for _, tt := range []struct {
		source string
		patch  DocumentPatch
		want   string
	}{
		{"body\x00", DocumentPatch{SetProps: []Property{{"a", 1}}}, "---\na: 1\n---\nbody\x00"},
		{"---\na: 1\n---\nbody", DocumentPatch{UnsetProps: []string{"a"}}, "body"},
		{"---\n---\nold", DocumentPatch{BodyOperation: ReplaceBody, Body: "new"}, "new"},
		{"body", DocumentPatch{BodyOperation: AppendBody, Body: "end"}, "bodyend"},
		{"body", DocumentPatch{BodyOperation: PrependBody, Body: "start"}, "startbody"},
		{"body", DocumentPatch{BodyOperation: DeleteBody}, ""},
	} {
		path := filepath.Join(t.TempDir(), "note.md")
		mustWrite(t, path, tt.source)
		if err := Patch(path, tt.patch); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != tt.want {
			t.Fatalf("patch got %q %v, want %q", data, err, tt.want)
		}
	}
}

func TestNoOpPatchPreservesFile(t *testing.T) {
	for _, patch := range []DocumentPatch{{}, {SetProps: []Property{{"title", "Keep"}, {"number", uint8(1)}}}, {UnsetProps: []string{"absent"}}, {BodyOperation: AppendBody}} {
		path := filepath.Join(t.TempDir(), "note.md")
		original := "---\r\n# comment\r\ntitle: 'Keep'\r\nnumber: 1\r\n---\r\nbody\r\n"
		mustWrite(t, path, original)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := Patch(path, patch); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != original || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
			t.Fatal("no-op rewrote file")
		}
	}
}

func TestInvalidPatchesHaveNoSideEffects(t *testing.T) {
	for _, tt := range []struct {
		name  string
		patch DocumentPatch
	}{
		{"body operation", DocumentPatch{BodyOperation: -1}},
		{"unset name", DocumentPatch{UnsetProps: []string{""}}},
		{"property name", DocumentPatch{SetProps: []Property{{Name: "", Value: true}}}},
		{"overwritten value", DocumentPatch{SetProps: []Property{{Name: "a", Value: new(int)}, {Name: "a", Value: true}}}},
		{"unset value", DocumentPatch{SetProps: []Property{{Name: "a", Value: new(int)}}, UnsetProps: []string{"a"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			missing := filepath.Join(dir, "missing", "note.md")
			if err := Patch(missing, tt.patch); !errors.Is(err, ErrInvalidPatch) {
				t.Fatalf("missing target: %v", err)
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Fatalf("invalid patch created paths: %v, %v", entries, err)
			}

			path := filepath.Join(dir, "note.md")
			const original = "---\r\n# keep this comment\r\na: 1\r\n---\r\nbody\r\n"
			mustWrite(t, path, original)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Patch(path, tt.patch); !errors.Is(err, ErrInvalidPatch) {
				t.Fatalf("existing target: %v", err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != original || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
				t.Fatal("invalid patch modified target")
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
				t.Fatalf("invalid patch left temporary files: %v, %v", entries, err)
			}
		})
	}
}

func TestAtomicWriteCleansUpAfterRenameFailure(t *testing.T) {
	dir := t.TempDir()
	// Renaming a regular temporary file over a directory must fail.
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("replacement"), 0o600); err == nil {
		t.Fatal("replaced a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "target" || !entries[0].IsDir() {
		t.Fatalf("temporary files left after failure: %v, %v", entries, err)
	}
}

func TestExclusiveCreationConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			results <- Write(path, Document{Body: "complete"}, ExclusiveCreate(true))
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, fs.ErrExist) {
			t.Fatal(err)
		}
	}
	if successes != 1 || mustRead(t, path).Body != "complete" {
		t.Fatalf("exclusive successes: %d", successes)
	}
}
