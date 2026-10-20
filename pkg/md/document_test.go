package md

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestDocumentProp(t *testing.T) {
	props := []Property{{"title", "Note"}, {"draft", false}, {"nullable", nil}, {"config.theme", "dark"}}
	for _, tt := range []struct {
		name  string
		doc   Document
		key   string
		want  Property
		found bool
	}{
		{"zero document", Document{}, "title", Property{}, false},
		{"empty properties", Document{Props: []Property{}}, "title", Property{}, false},
		{"first", Document{Props: props}, "title", props[0], true},
		{"false value", Document{Props: props}, "draft", props[1], true},
		{"null value", Document{Props: props}, "nullable", props[2], true},
		{"last and literal dots", Document{Props: props}, "config.theme", props[3], true},
		{"missing", Document{Props: props}, "missing", Property{}, false},
		{"case sensitive", Document{Props: props}, "Title", Property{}, false},
		{"empty key", Document{Props: props}, "", Property{}, false},
		{"first duplicate", Document{Props: []Property{{"title", "first"}, {"title", "second"}}}, "title", Property{"title", "first"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, found := tt.doc.Prop(tt.key)
			if found != tt.found || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Prop(%q) = %#v, %t; want %#v, %t", tt.key, got, found, tt.want, tt.found)
			}
		})
	}
}

func TestDocumentPropReturnsShallowCopy(t *testing.T) {
	value := map[string]any{"theme": "dark"}
	doc := Document{Props: []Property{{Name: "config", Value: value}}}
	prop, ok := doc.Prop("config")
	if !ok {
		t.Fatal("property not found")
	}
	prop.Name = "renamed"
	if doc.Props[0].Name != "config" {
		t.Fatal("changing returned property name changed the document")
	}
	prop.Value.(map[string]any)["theme"] = "light"
	if value["theme"] != "light" {
		t.Fatal("nested value was cloned")
	}
}

func TestDocumentRoundTrip(t *testing.T) {
	body := "# Heading\r\n\n---\nbody delimiter\x00\xff"
	source := "---\r\nz: &list [1, {deep: true}]\r\na: *list\r\nnull: null\r\n---\r\n" + body
	// YAML null is not a string key unless explicitly quoted.
	source = strings.Replace(source, "null: null", "'null': null", 1)
	doc, err := parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Body != body || !reflect.DeepEqual(names(doc), []string{"z", "a", "null"}) {
		t.Fatalf("document: %#v", doc)
	}
	if !reflect.DeepEqual(doc.Props[0].Value, doc.Props[1].Value) {
		t.Fatal("alias not resolved")
	}
	encoded, err := doc.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "&list") || strings.Contains(string(encoded), "*list") {
		t.Fatal("anchors not normalized")
	}
	roundTrip, err := parse(encoded)
	if err != nil || !reflect.DeepEqual(roundTrip, doc) {
		t.Fatalf("round trip: %#v, %v", roundTrip, err)
	}
}

func TestParseBoundaries(t *testing.T) {
	for _, source := range []string{"", "plain\n---\na: b\n---\n", "---", "\xef\xbb\xbf---\na: b\n---\n"} {
		doc, err := parse([]byte(source))
		if err != nil || doc.Body != source || len(doc.Props) != 0 {
			t.Fatalf("plain %q: %#v %v", source, doc, err)
		}
	}
	for _, source := range []string{"---\n---", "---\r\n---\r\n", "---\n# comment\n---\n", "---\n{}\n---\n"} {
		doc, err := parse([]byte(source))
		if err != nil || len(doc.Props) != 0 || doc.Body != "" {
			t.Fatalf("empty %q: %#v %v", source, doc, err)
		}
		data, err := doc.MarshalText()
		if err != nil || len(data) != 0 {
			t.Fatalf("empty serialization: %q %v", data, err)
		}
	}
	for _, source := range []string{
		"---\na: b", "---\na: [\n---\n", "---\nnull\n---\n", "---\n~\n---\n",
		"---\n[]\n---\n", "---\nstring\n---\n", "---\na: 1\na: 2\n---\n",
		"---\na: {x: 1, x: 2}\n---\n", "---\n1: number\n---\n", "---\n'': empty\n---\n",
		"---\na: !custom x\n---\n", "---\na: &a [*a]\n---\n", "---\na: *missing\n---\n",
		"---\na: {[x]: y}\n---\n", "---\na: !!int nope\n---\n",
		"---\na: {1: first, 0x1: second}\n---\n",
		"---\na: {null: first, ~: second}\n---\n",
		"---\nbase: &base {a: 1}\nvalue: {<<: *base}\n---\n",
	} {
		if _, err := parse([]byte(source)); !errors.Is(err, ErrInvalidFrontMatter) {
			t.Errorf("accepted %q: %v", source, err)
		}
	}
}

func TestSerialization(t *testing.T) {
	doc := Document{Props: []Property{{"z", true}, {"a", []any{1, "two", nil}}}, Body: "hello\n\x00"}
	raw, err := doc.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "---\nz: true\na:") || !strings.HasSuffix(string(raw), doc.Body) {
		t.Fatalf("raw: %q", raw)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"props":[{"name":"z","value":true},{"name":"a","value":[1,"two",null]}],"body":"hello\n\u0000"}`
	if string(encoded) != want {
		t.Fatalf("json: %s", encoded)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var structured struct {
		Props []Property `yaml:"props"`
		Body  string     `yaml:"body"`
	}
	if err := yaml.Unmarshal(data, &structured); err != nil {
		t.Fatal(err)
	}
	if structured.Body != doc.Body || len(structured.Props) != 2 || structured.Props[0].Name != "z" {
		t.Fatalf("yaml: %s", data)
	}
	for _, empty := range []Document{{}, {Props: []Property{}}} {
		data, err := json.Marshal(empty)
		if err != nil || string(data) != `{"props":[],"body":""}` {
			t.Fatalf("empty JSON: %s, %v", data, err)
		}
		data, err = yaml.Marshal(empty)
		if err != nil || string(data) != "props: []\nbody: \"\"\n" {
			t.Fatalf("empty YAML: %s, %v", data, err)
		}
	}
}

func TestValueValidationAndFormats(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	slice := make([]any, 1)
	slice[0] = slice
	var deep any = "end"
	for range 110 {
		deep = []any{deep}
	}
	bad := []any{func() {}, make(chan int), new(int), struct{ X int }{1}, complex(1, 2), cycle, slice, deep, "\xff", map[any]any{[1]int{1}: "array key"}, map[any]any{int(1): "signed", uint(1): "unsigned"}, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}
	for _, value := range bad {
		doc := Document{Props: []Property{{"bad", value}}}
		if _, err := doc.MarshalText(); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("accepted %T: %v", value, err)
		}
		if _, err := json.Marshal(doc); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("JSON accepted %T: %v", value, err)
		}
		if _, err := yaml.Marshal(doc); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("YAML accepted %T: %v", value, err)
		}
	}
	for _, props := range [][]Property{{{"", 1}}, {{"\xff", 1}}, {{"a", 1}, {"a", 2}}} {
		if _, err := (Document{Props: props}).MarshalText(); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("invalid properties accepted: %v", err)
		}
	}
	for _, value := range []any{math.Inf(1), math.NaN(), map[any]any{1: "number key"}, []any{map[any]any{false: nil}}, map[string]any{"nested": map[any]any{nil: "null key"}}} {
		doc := Document{Props: []Property{{"value", value}}}
		if _, err := doc.MarshalText(); err != nil {
			t.Errorf("valid YAML %T: %v", value, err)
		}
		if _, err := json.Marshal(doc); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("JSON accepted %v: %v", value, err)
		}
	}
	doc := Document{Body: "\xff"}
	if _, err := doc.MarshalText(); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(doc); err == nil {
		t.Fatal("JSON accepted invalid UTF8")
	}
	if _, err := yaml.Marshal(doc); err == nil {
		t.Fatal("YAML accepted invalid UTF8")
	}
	timestamp := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, value := range []any{timestamp, []string{"x"}, [2]int{1, 2}, map[string]int{"a": 2}, uint64(math.MaxUint64), float32(1.5), []any(nil), map[string]any(nil)} {
		doc := Document{Props: []Property{{"value", value}}}
		data, err := doc.MarshalText()
		if err != nil {
			t.Fatalf("%T: %v", value, err)
		}
		if _, err := parse(data); err != nil {
			t.Fatalf("parse %T: %v", value, err)
		}
		if _, err := json.Marshal(doc); err != nil {
			t.Fatalf("JSON %T: %v", value, err)
		}
	}
}

func names(d Document) []string {
	result := make([]string, len(d.Props))
	for i, p := range d.Props {
		result[i] = p.Name
	}
	return result
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"plain", "---\na: [1, true]\n---\nbody", "---\r\n---\r\n", "---\na: &x [*x]\n---\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := parse(data)
		if err != nil {
			return
		}
		normalized, err := doc.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		// Removing empty front matter can expose a body delimiter. With no
		// properties we deliberately emit only the body, not an escape block.
		if _, opening := openingDelimiterEnd(normalized); len(doc.Props) == 0 && opening {
			return
		}
		again, err := parse(normalized)
		if err != nil {
			t.Fatal(err)
		}
		normalizedAgain, err := again.MarshalText()
		if err != nil || string(normalized) != string(normalizedAgain) {
			t.Fatalf("unstable serialization: %q -> %q (%v)", normalized, normalizedAgain, err)
		}
	})
}
