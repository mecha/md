package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestArgumentErrors(t *testing.T) {
	tests := [][]string{
		{}, {"unknown"}, {"read"}, {"write", "a", "b"}, {"read", ""},
		{"read", "a", "-f=xml"}, {"read", "a", "-y=x=1"},
		{"read", "a", "-c"}, {"read", "a", "--compact", "-f=yaml"},
		{"read", "a", "-c", "-f=raw"}, {"read", "a", "-f=json", "-c=true"},
		{"write", "a", "-c"}, {"patch", "a", "--compact"},
		{"write", "a", "--prop=x=1"}, {"write", "a", "--mode=auto"},
		{"write", "a", "-r", "-x"}, {"write", "a", "--unset=x"},
		{"patch", "a", "--parents"}, {"patch", "a", "--require"},
		{"patch", "a", "-p"}, {"patch", "a", "-a"}, {"patch", "a", "-c", "-b=ignored"},
		{"patch", "a", "-c", "-a", "-b=x"}, {"patch", "a", "-p", "-a", "-b=x"}, {"patch", "a", "-c", "-c"},
		{"write", "a", "-b=x", "--body=y"}, {"write", "a", "-s", "-s"}, {"write", "a", "-s", "-f=x"},
		{"write", "a", "-f="}, {"patch", "a", "-u="},
		{"write", "a", "-y=x=["}, {"write", "a", "-y=x=!custom value"},
		{"write", "a", "-y=x=1\n---\n2"}, {"write", "a", "-y=x={a: 1, a: 2}"},
		{"write", "a", "-rx"}, {"write", "a", "-s=true"}, {"write", "a", "-x=false"},
	}
	for _, value := range []string{"", "missing-separator", "=value", `""=value`, `"unterminated=value`, `"field"missing=value`, `"\q"=value`} {
		tests = append(tests, []string{"write", "a", "-y", value})
	}
	for _, perms := range []string{"", "888", "1000", "-1", "+600", "0o600", "rwx", "999999999999999999999999999999"} {
		tests = append(tests, []string{"write", "a", "--file-perms", perms})
	}
	for _, command := range []string{"read", "write", "patch"} {
		for _, flag := range commandFlags(command) {
			if flag.value {
				tests = append(tests, []string{command, "a", "--" + flag.name})
			} else {
				tests = append(tests, []string{command, "a", "--" + flag.name + "=true"})
			}
		}
	}
	for _, args := range tests {
		code, out, err := invoke(t, args, "")
		if code != 1 || out != "" || err == "" {
			t.Errorf("accepted %q: %d %q %q", args, code, out, err)
		}
	}
}

func TestQuotedNamesAndPositionals(t *testing.T) {
	opts, help, err := parseArgs([]string{"write", "-y", `"field=with=equals"=value=also=with=equals`, "a=b.md", "--yaml", `'it''s=valid'=true`, "-y", `"escaped\"=key"=null`, "-y", "empty="})
	if err != nil || help || opts.path != "a=b.md" {
		t.Fatalf("args: %#v %v", opts, err)
	}
	if opts.set[0].Name != "field=with=equals" || opts.set[0].Value != "value=also=with=equals" || opts.set[1].Name != "it's=valid" || opts.set[2].Name != `escaped"=key` || opts.set[3].Value != nil {
		t.Fatalf("properties: %#v", opts.set)
	}
	for _, file := range []string{"-", "--help", "-dash.md"} {
		opts, help, err := parseArgs([]string{"read", "--", file})
		if err != nil || help || opts.path != file {
			t.Fatalf("positional %q: %#v %v", file, opts, err)
		}
	}
}

func TestFlagAliases(t *testing.T) {
	for _, command := range []string{"read", "write", "patch"} {
		for _, flag := range commandFlags(command) {
			if flag.short == "" || flag.name == "help" {
				continue
			}
			value := ""
			switch flag.name {
			case "format":
				value = "json"
			case "yaml":
				value = "a=1"
			case "body":
				value = "body"
			case "body-file":
				value = "body.txt"
			case "unset":
				value = "a"
			}
			makeArgs := func(name string, inline bool) []string {
				args := []string{command, "note.md"}
				if flag.name == "compact" {
					args = append(args, "-f=json")
				}
				if flag.name == "append" || flag.name == "prepend" {
					args = append(args, "-b=x")
				}
				if flag.value && inline {
					return append(args, name+"="+value)
				}
				args = append(args, name)
				if flag.value {
					args = append(args, value)
				}
				return args
			}
			long, _, err := parseArgs(makeArgs("--"+flag.name, false))
			if err != nil {
				t.Fatal(err)
			}
			for _, inline := range []bool{false, true} {
				short, _, err := parseArgs(makeArgs("-"+flag.short, inline))
				if err != nil || !reflect.DeepEqual(short, long) {
					t.Fatalf("alias %s: %#v != %#v (%v)", flag.name, short, long, err)
				}
			}
		}
	}
	for _, value := range []string{"0", "0000", "600", "0600", "0777"} {
		if _, err := parsePerms(value); err != nil {
			t.Errorf("permissions %q: %v", value, err)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"read", "-h"}, {"write", "--help"}, {"patch", "-h"}} {
		code, out, err := invoke(t, args, "")
		if code != 0 || err != "" || !strings.Contains(out, "usage: md ") {
			t.Fatalf("help %q: %d %q %q", args, code, out, err)
		}
		if len(args) > 1 {
			for _, flag := range commandFlags(args[0]) {
				if !strings.Contains(out, "--"+flag.name) {
					t.Errorf("help missing --%s", flag.name)
				}
			}
		}
	}
}
