package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
	"md/pkg/md"
)

func invoke(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runMain(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}
func success(t *testing.T, args []string, stdin string) string {
	t.Helper()
	code, out, err := invoke(t, args, stdin)
	if code != 0 || err != "" {
		t.Fatalf("%q: code %d, stderr %s", args, code, err)
	}
	return out
}
func readDoc(t *testing.T, path string) md.Document {
	t.Helper()
	doc, err := md.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestCLIWritePatchRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	out := success(t, []string{"write", path, "-y", "z=1", "--yaml=a={old: true}", "-y", "z=2", "-b", "body"}, "")
	if out != "" {
		t.Fatal("write was not silent")
	}
	doc := readDoc(t, path)
	if doc.Body != "body" || len(doc.Props) != 2 || doc.Props[0].Name != "z" || doc.Props[0].Value != int64(2) {
		t.Fatalf("created: %#v", doc)
	}
	success(t, []string{"patch", path, "-u", "z", "-y", "z=3", "-y", "a={new: true}", "-y", "b=null"}, "")
	doc = readDoc(t, path)
	if len(doc.Props) != 2 || doc.Props[0].Name != "a" || doc.Props[1].Name != "b" || doc.Props[1].Value != nil || doc.Body != "body" {
		t.Fatalf("patched: %#v", doc)
	}
	if !reflect.DeepEqual(doc.Props[0].Value, map[string]any{"new": true}) {
		t.Fatal("deep merge occurred")
	}
	raw := success(t, []string{"read", path}, "")
	if !strings.HasPrefix(raw, "---\na:") || !strings.HasSuffix(raw, "body") {
		t.Fatalf("raw: %q", raw)
	}
	for _, format := range []string{"json", "yaml"} {
		out = success(t, []string{"read", "-f=" + format, path}, "")
		var wire struct {
			Props []md.Property `json:"props" yaml:"props"`
			Body  string        `json:"body" yaml:"body"`
		}
		var err error
		if format == "json" {
			err = json.Unmarshal([]byte(out), &wire)
		} else {
			err = yaml.Unmarshal([]byte(out), &wire)
		}
		if err != nil || wire.Body != "body" || len(wire.Props) != 2 || wire.Props[0].Name != "a" {
			t.Fatalf("%s: %s %v", format, out, err)
		}
	}
	success(t, []string{"write", path}, "")
	if doc = readDoc(t, path); len(doc.Props) != 0 || doc.Body != "" {
		t.Fatal("write did not replace document")
	}
	if out = success(t, []string{"read", path, "--format", "json"}, ""); !strings.Contains(out, `"props": []`) {
		t.Fatalf("empty props: %s", out)
	}
}

func TestBodySourcesAndOperations(t *testing.T) {
	for _, long := range []bool{false, true} {
		flags := map[string]string{"body": "-b", "file": "-f", "stdin": "-s", "append": "-a", "prepend": "-p", "clear": "-c"}
		if long {
			flags = map[string]string{"body": "--body", "file": "--body-file", "stdin": "--body-stdin", "append": "--append", "prepend": "--prepend", "clear": "--clear"}
		}
		path := filepath.Join(t.TempDir(), "note.md")
		input := filepath.Join(t.TempDir(), "body.txt")
		if err := os.WriteFile(input, []byte("FILE\r\n\x00"), 0o600); err != nil {
			t.Fatal(err)
		}
		success(t, []string{"write", path, flags["file"], input}, "")
		if readDoc(t, path).Body != "FILE\r\n\x00" {
			t.Fatal("file body changed")
		}
		success(t, []string{"patch", path, flags["append"], flags["stdin"]}, "END")
		success(t, []string{"patch", path, flags["prepend"], flags["body"] + "=START"}, "")
		if readDoc(t, path).Body != "STARTFILE\r\n\x00END" {
			t.Fatal("append/prepend inserted separator")
		}
		success(t, []string{"patch", path, flags["body"], "replacement"}, "")
		if readDoc(t, path).Body != "replacement" {
			t.Fatal("replacement failed")
		}
		success(t, []string{"patch", path, flags["clear"]}, "")
		if readDoc(t, path).Body != "" {
			t.Fatal("clear failed")
		}
		success(t, []string{"patch", path, flags["body"] + "="}, "")
	}
}

func TestExitCodesAndWriteOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.md")
	missing := filepath.Join(dir, "missing.md")
	success(t, []string{"write", path, "-x", "-b=original"}, "")
	for _, tt := range []struct {
		args []string
		code int
	}{
		{[]string{"write", path, "--exclusive"}, 2},
		{[]string{"write", missing, "-r"}, 3},
		{[]string{"read", missing}, 3}, {[]string{"patch", missing}, 3},
		{[]string{"write", filepath.Join(dir, "absent", "new.md")}, 1},
		{[]string{"patch", path, "-f", missing}, 1}, {[]string{"write", missing, "--require", "-f", missing}, 1},
		{[]string{"read", dir}, 1}, {[]string{"write", dir}, 1},
	} {
		code, out, err := invoke(t, tt.args, "")
		if code != tt.code || out != "" || err == "" {
			t.Errorf("%q: code=%d stdout=%q stderr=%q", tt.args, code, out, err)
		}
	}
	success(t, []string{"write", path, "--require", "--body=new"}, "")
	if readDoc(t, path).Body != "new" {
		t.Fatal("required write failed")
	}
	nested := filepath.Join(dir, "a", "b", "note.md")
	success(t, []string{"write", nested, "--parents", "--file-perms=600", "--dir-perms", "0700"}, "")
	for file, want := range map[string]os.FileMode{nested: 0o600, filepath.Dir(nested): 0o700} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("permissions: %v %v", info, err)
		}
	}
	success(t, []string{"write", filepath.Join(dir, "c", "note.md"), "-p"}, "")
}

func TestReadNormalizationAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	original := "---\r\n# comment\r\nz: 'value'\r\na: false\r\n---\r\nbody\r\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	out := success(t, []string{"read", path}, "")
	if out != "---\nz: value\na: false\n---\nbody\r\n" {
		t.Fatalf("normalized: %q", out)
	}
	success(t, []string{"patch", path, "-y", "z=value"}, "")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatal("read or no-op modified file")
	}
	for _, input := range []string{"---\na: [\n---\n", "---\na: {1: value}\n---\n", "---\na: .inf\n---\n", "\xff"} {
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := invoke(t, []string{"read", path, "-f=json"}, "")
		if code != 1 || out != "" || stderr == "" {
			t.Fatalf("JSON invalid %q: %d %q %q", input, code, out, stderr)
		}
	}
}

type brokenIO struct{}

func (brokenIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (brokenIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

func TestIOErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	for _, args := range [][]string{{"write", path, "-s"}, {"-h"}, {"write", path, "-f", path}} {
		code, err := run(args, brokenIO{}, brokenIO{})
		if code != 1 || err == nil {
			t.Fatalf("%q: %d %v", args, code, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created file on input error")
	}
	success(t, []string{"write", path, "-b=content"}, "")
	if code, err := run([]string{"read", path}, nil, brokenIO{}); code != 1 || err == nil {
		t.Fatalf("output failure: %d %v", code, err)
	}
	if code, err := run([]string{"read", path}, nil, shortWriter{}); code != 1 || err == nil {
		t.Fatalf("short output: %d %v", code, err)
	}
}
