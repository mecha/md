package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortFlags(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(map[bool]string{false: "separate", true: "inline"}[inline], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note.md")
			args := []string{path, "-m", "create", "-p", "title=Short", "-b", "body"}
			if inline {
				args = []string{path, "-m=create", "-p=title=Short", "-b=body"}
			}
			code, _, stderr := invoke(t, args, "")
			if code != 0 {
				t.Fatalf("create: %d %s", code, stderr)
			}
			doc := readTestDocument(t, path)
			if doc.Fields["title"] != "Short" || doc.Body != "body" {
				t.Fatalf("document: %#v", doc)
			}
			bodyFile := filepath.Join(t.TempDir(), "body.txt")
			if err := os.WriteFile(bodyFile, []byte("file body"), 0o600); err != nil {
				t.Fatal(err)
			}
			args = []string{"-m", "patch", path, "-f", bodyFile}
			if inline {
				args = []string{"-m=patch", path, "-f=" + bodyFile}
			}
			code, _, stderr = invoke(t, args, "")
			if code != 0 || readTestDocument(t, path).Body != "file body" {
				t.Fatalf("body file: %d %s", code, stderr)
			}
			code, _, stderr = invoke(t, []string{path, "-s"}, "stdin body\x00")
			if code != 0 || readTestDocument(t, path).Body != "stdin body\x00" {
				t.Fatalf("stdin: %d %s", code, stderr)
			}
		})
	}
}

func TestFlagValidation(t *testing.T) {
	tests := [][]string{
		{"--set", "a=1"}, {"-s=true"}, {"--body-stdin=true"},
		{"--unset", ""}, {"--body-file="}, {"-f="},
		{"-b=one", "--body=two"}, {"-f=body.md", "-s"},
		{"-s", "-f=body.md"}, {"-s", "-b=body"},
		{"-p", ""}, {"-p", "=value"}, {"-p", `""=value`}, {"-p", `"\q"=value`},
	}
	for _, flag := range []string{"-m", "-p", "-b", "-f", "--mode", "--prop", "--unset", "--body", "--body-file"} {
		tests = append(tests, []string{flag})
	}
	for _, args := range tests {
		args = append([]string{"note.md"}, args...)
		if _, _, err := parseArgs(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
	opts, _, err := parseArgs([]string{"a=b.md", "-p", `"escaped\"=key"=null`})
	if err != nil || opts.path != "a=b.md" || opts.operations[0].Name != `escaped"=key` {
		t.Fatalf("quoted key: %#v, %v", opts, err)
	}
	code, stdout, stderr := invoke(t, []string{"-h"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("help: %d %s", code, stderr)
	}
	for _, flag := range []string{"-p, --prop", "-m, --mode", "-b, --body", "-f, --body-file", "-s, --body-stdin"} {
		if !strings.Contains(stdout, flag) {
			t.Errorf("help missing %s", flag)
		}
	}
}

type brokenIO struct{}

func (brokenIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (brokenIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestIOErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	for _, args := range [][]string{{path, "-s"}, {path, "-f", filepath.Join(t.TempDir(), "missing")}, {"-h"}} {
		code, err := run(args, brokenIO{}, brokenIO{})
		if code != 1 || err == nil {
			t.Fatalf("%q: %d %v", args, code, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("created file on error: %v", err)
	}
}

func TestCLIOperationOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	code, _, stderr := invoke(t, []string{path, "-p", "a=1", "--unset", "a"}, "")
	if code != 0 || readTestDocument(t, path).Fields["a"] != 1 {
		t.Fatalf("create unset: %d %s", code, stderr)
	}
	code, _, stderr = invoke(t, []string{path, "--unset", "a", "-p", "a=null", "-b="}, "")
	if code != 0 {
		t.Fatalf("patch: %d %s", code, stderr)
	}
	doc := readTestDocument(t, path)
	if value, ok := doc.Fields["a"]; !ok || value != nil || doc.Body != "" {
		t.Fatalf("null property: %#v", doc)
	}
}
