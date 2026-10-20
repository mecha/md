package yamldata

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeRejectsInvalidYAML(t *testing.T) {
	for _, input := range []string{
		"1\n---\n2",
		"1\n---\n[",
		"a: [",
		"a: 1\na: 2",
		"!!int nope",
		"{!!int nope: value}",
		"!custom x",
		"&x [*x]",
		"{[a]: b}",
		"base: &base {a: 1}\nvalue: {<<: *base}",
		strings.Repeat("[", 110) + "1" + strings.Repeat("]", 110),
	} {
		if _, err := Decode([]byte(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestDecodeRejectsEquivalentMappingKeys(t *testing.T) {
	for _, input := range []string{
		"{1: first, 0x1: second}",
		"{true: first, TRUE: second}",
		"{null: first, ~: second}",
		"{1.0: first, 1.00: second}",
		"{.nan: first, .NaN: second}",
		"{hello: first, !!binary aGVsbG8=: second}",
		"{a: &key hello, nested: {*key: first, hello: second}}",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := Decode([]byte(input)); err == nil {
				t.Fatal("accepted equivalent mapping keys")
			}
		})
	}
}

func TestDecodeNesting(t *testing.T) {
	for _, depth := range []int{99, 100, 101} {
		input := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
		_, err := Decode([]byte(input))
		if (err == nil) != (depth <= 100) {
			t.Errorf("depth %d: %v", depth, err)
		}
	}
	for _, depth := range []int{40, 61} {
		// The anchor is validated first along a short path. Reusing it along
		// a longer path must still enforce the expanded value's depth limit.
		input := "base: &base " + strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40) +
			"\ndeep: " + strings.Repeat("[", depth) + "*base" + strings.Repeat("]", depth)
		_, err := Decode([]byte(input))
		if (err == nil) != (depth == 40) {
			t.Errorf("alias at depth %d: %v", depth, err)
		}
	}
}

func TestDecodeValues(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  any
	}{
		{"", nil},
		{"# comment", nil},
		{"null", nil},
		{"[1, true, null]", []any{1, true, nil}},
		{"!!binary aGVsbG8=", "hello"},
		{"a: &x {b: 1}\nc: *x", map[string]any{"a": map[string]any{"b": 1}, "c": map[string]any{"b": 1}}},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Decode([]byte(tt.input))
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decode = %#v, %v; want %#v", got, err, tt.want)
			}
			root, err := Parse([]byte(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			var fromNode any
			if err := root.Decode(&fromNode); err != nil || !reflect.DeepEqual(fromNode, tt.want) {
				t.Fatalf("Parse node decodes to %#v, %v; want %#v", fromNode, err, tt.want)
			}
		})
	}
}
