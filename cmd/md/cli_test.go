package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestAutoCreatesAndPatchesDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issue.md")

	code, _, stderr := invoke(t, []string{
		path,
		"--prop", "id=1",
		"--prop", `title=Crash on Startup`,
		"--prop", `labels=['bug', 'backend']`,
		"--body", "Initial body\n",
	}, "")
	if code != 0 {
		t.Fatalf("create exit = %d, stderr = %q", code, stderr)
	}
	doc := readTestDocument(t, path)
	if doc.Fields["id"] != 1 || doc.Fields["title"] != "Crash on Startup" || doc.Body != "Initial body\n" {
		t.Fatalf("created document = %#v", doc)
	}
	if got := doc.Fields["labels"]; !reflect.DeepEqual(got, []any{"bug", "backend"}) {
		t.Fatalf("labels = %#v", got)
	}

	code, _, stderr = invoke(t, []string{
		path,
		"--prop=status=open",
		"--unset", "title",
		"--body-stdin",
	}, "Replacement body")
	if code != 0 {
		t.Fatalf("patch exit = %d, stderr = %q", code, stderr)
	}
	doc = readTestDocument(t, path)
	if _, present := doc.Fields["title"]; present {
		t.Errorf("title was not unset: %#v", doc.Fields)
	}
	if doc.Fields["status"] != "open" || doc.Body != "Replacement body" {
		t.Fatalf("patched document = %#v", doc)
	}
}

func TestModesAndExitCodes(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.md")
	missing := filepath.Join(dir, "missing.md")
	if err := os.WriteFile(existing, []byte("body\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := invoke(t, []string{existing, "--mode", "create"}, "")
	if code != 2 || !strings.Contains(stderr, "already exists") {
		t.Fatalf("create existing = exit %d, stderr %q", code, stderr)
	}

	code, _, stderr = invoke(t, []string{missing, "--mode=patch"}, "")
	if code != 3 || !strings.Contains(stderr, "does not exist") {
		t.Fatalf("patch missing = exit %d, stderr %q", code, stderr)
	}

	code, _, stderr = invoke(t, []string{missing, "--mode", "create", "--unset", "ignored"}, "")
	if code != 0 {
		t.Fatalf("create missing = exit %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("unset in create produced %q", data)
	}
}

func TestPropAllowsQuotedFieldsContainingEquals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fields.md")
	code, _, stderr := invoke(t, []string{
		path,
		"--prop", `"field=with=equals"=value=also=with=equals`,
		"--prop", `'it''s=valid'=true`,
	}, "")
	if code != 0 {
		t.Fatalf("create exit = %d, stderr = %q", code, stderr)
	}
	doc := readTestDocument(t, path)
	if doc.Fields["field=with=equals"] != "value=also=with=equals" {
		t.Errorf("double-quoted field = %#v", doc.Fields)
	}
	if doc.Fields["it's=valid"] != true {
		t.Errorf("single-quoted field = %#v", doc.Fields)
	}
}

func TestBodySourcesAndPreservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	bodyPath := filepath.Join(dir, "body.md")
	if err := os.WriteFile(bodyPath, []byte("Body from file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := invoke(t, []string{path, "--body-file", bodyPath}, "")
	if code != 0 {
		t.Fatalf("body-file exit = %d, stderr = %q", code, stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Body from file\n" {
		t.Fatalf("body-only file = %q", data)
	}

	code, _, stderr = invoke(t, []string{path, "--prop", "draft=false"}, "")
	if code != 0 {
		t.Fatalf("field patch exit = %d, stderr = %q", code, stderr)
	}
	doc := readTestDocument(t, path)
	if doc.Fields["draft"] != false || doc.Body != "Body from file\n" {
		t.Fatalf("field patch did not preserve body: %#v", doc)
	}
}

func TestArgumentErrorsAndHelp(t *testing.T) {
	tests := [][]string{
		{},
		{"a.md", "--mode", "replace"},
		{"a.md", "--prop", "missing-separator"},
		{"a.md", "--prop", "field=["},
		{"a.md", "--prop", `"unterminated=value`},
		{"a.md", "--prop", `"field"missing=value`},
		{"a.md", "--body", "one", "--body-stdin"},
		{"a.md", "--unknown"},
		{"a.md", "b.md"},
	}
	for _, args := range tests {
		code, _, _ := invoke(t, args, "")
		if code != 1 {
			t.Errorf("args %q exit = %d, want 1", args, code)
		}
	}

	code, stdout, stderr := invoke(t, []string{"--help"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "usage: md FILE [options]") || !strings.Contains(stdout, `--prop '"field=with=equals"=value'`) {
		t.Fatalf("help = exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestNoOpPatchDoesNotRewriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	original := []byte("---\r\ntitle: \"Keep formatting\"\r\n---\r\nbody\r\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := invoke(t, []string{path, "--mode", "patch", "--prop", "title=Keep formatting"}, "")
	if code != 0 {
		t.Fatalf("no-op patch exit = %d, stderr = %q", code, stderr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("no-op changed file:\n%q", got)
	}
}

func invoke(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runMain(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

type testDocument struct {
	Fields map[string]any
	Body   string
}

func readTestDocument(t *testing.T, path string) testDocument {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := testDocument{Fields: make(map[string]any), Body: string(data)}
	if strings.HasPrefix(doc.Body, "---\n") {
		parts := strings.SplitN(doc.Body[4:], "---\n", 2)
		if len(parts) != 2 {
			t.Fatal("missing delimiter")
		}
		if err := yaml.Unmarshal([]byte(parts[0]), &doc.Fields); err != nil {
			t.Fatal(err)
		}
		doc.Body = parts[1]
	}
	return doc
}
