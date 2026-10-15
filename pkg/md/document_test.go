package md

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParsePreservesBodyAndUnknownFields(t *testing.T) {
	body := "# Heading\n\n---\nbody delimiter stays\n\x00"
	data := []byte("---\nknown: value\nextension:\n  nested: [1, two]\n---\n" + body)

	doc, err := parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Body != body {
		t.Fatalf("Parse() body %q", doc.Body)
	}
	wantExtension := map[string]any{"nested": []any{1, "two"}}
	if !reflect.DeepEqual(doc.Fields["extension"], wantExtension) {
		t.Errorf("extension = %#v, want %#v", doc.Fields["extension"], wantExtension)
	}

	encoded, err := doc.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := parse(encoded)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	if roundTrip.Body != body {
		t.Errorf("round-trip body = %q, want exact %q", roundTrip.Body, body)
	}
	if !reflect.DeepEqual(roundTrip.Fields, doc.Fields) {
		t.Errorf("round-trip fields = %#v, want %#v", roundTrip.Fields, doc.Fields)
	}
}

func TestParseMarshalPreservesExistingFieldOrderAndNestedValues(t *testing.T) {
	body := "body\nwithout normalization\x00"
	data := []byte("---\nupdated: \"2024-01-02T03:04:05Z\"\nextension:\n  zeta: [1, {deep: true}]\n  link: \"[[issues/7]]\"\ntitle: Existing\ncreated: \"2024-01-01T03:04:05Z\"\n---\n" + body)

	doc, err := parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	wantOrder := []string{"updated", "extension", "title", "created"}
	if !reflect.DeepEqual(doc.FieldOrder, wantOrder) {
		t.Fatalf("FieldOrder = %v, want %v", doc.FieldOrder, wantOrder)
	}
	doc.Fields["draft"] = true
	doc.Fields["alpha"] = map[string]any{"items": []any{"[[wiki/Page]]", 2}}

	encoded, err := doc.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := parse(encoded)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	wantMarshaledOrder := []string{"updated", "extension", "title", "created", "alpha", "draft"}
	if !reflect.DeepEqual(roundTrip.FieldOrder, wantMarshaledOrder) {
		t.Errorf("round-trip FieldOrder = %v, want %v", roundTrip.FieldOrder, wantMarshaledOrder)
	}
	if !reflect.DeepEqual(roundTrip.Fields, doc.Fields) {
		t.Errorf("round-trip fields = %#v, want %#v", roundTrip.Fields, doc.Fields)
	}
	if roundTrip.Body != body {
		t.Errorf("round-trip body = %q, want exact %q", roundTrip.Body, body)
	}
}

func TestMarshalNewDocumentUsesSortedFields(t *testing.T) {
	doc := document{
		Fields: map[string]any{
			"updated": "2024-01-02T03:04:05Z",
			"zeta":    true,
			"author":  "[[users/alice]]",
			"merged":  true,
			"title":   "Ordered",
			"alpha":   "[[issues/1]]",
			"created": "2024-01-01T03:04:05Z",
			"status":  "closed",
		},
	}
	encoded, err := doc.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	parsed, err := parse(encoded)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	want := []string{"alpha", "author", "created", "merged", "status", "title", "updated", "zeta"}
	if !reflect.DeepEqual(parsed.FieldOrder, want) {
		t.Errorf("FieldOrder = %v, want %v\n%s", parsed.FieldOrder, want, encoded)
	}
}

func TestMarshalTreatsAliasesAsExtensionData(t *testing.T) {
	doc := document{
		Fields: map[string]any{
			"updated": "2024-01-02T03:04:05Z",
			"aliases": []string{"legacy-name"},
			"author":  "[[users/alice]]",
			"alpha":   true,
			"created": "2024-01-01T03:04:05Z",
			"title":   "Existing extension",
		},
	}
	encoded, err := doc.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	parsed, err := parse(encoded)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	want := []string{"aliases", "alpha", "author", "created", "title", "updated"}
	if !reflect.DeepEqual(parsed.FieldOrder, want) {
		t.Errorf("FieldOrder = %v, want %v\n%s", parsed.FieldOrder, want, encoded)
	}
	if !reflect.DeepEqual(parsed.Fields["aliases"], []any{"legacy-name"}) {
		t.Errorf("aliases extension = %#v", parsed.Fields["aliases"])
	}
}

func TestParseOnlyRecognizesLeadingFrontMatter(t *testing.T) {
	data := "intro\n---\nfield: value\n---\nend\n"
	doc, err := parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Body != data || len(doc.Fields) != 0 {
		t.Errorf("Parse() = body %q fields %#v; want entire input as body", doc.Body, doc.Fields)
	}
}

func TestParseCRLFFrontMatterAndMalformedFrontMatter(t *testing.T) {
	doc, err := parse([]byte("---\r\nname: A\r\n---\r\nbody\r\n"))
	if err != nil {
		t.Fatalf("Parse(CRLF) error = %v", err)
	}
	if doc.Body != "body\r\n" || doc.Fields["name"] != "A" {
		t.Errorf("Parse(CRLF) = %#v", doc)
	}

	_, err = parse([]byte("---\nunterminated: true\n"))
	if !errors.Is(err, ErrInvalidFrontMatter) {
		t.Errorf("Parse(unterminated) error = %v, want ErrInvalidFrontMatter", err)
	}
	_, err = parse([]byte("---\ninvalid: [\n---\n"))
	if !errors.Is(err, ErrInvalidFrontMatter) || !strings.Contains(err.Error(), "yaml") {
		t.Errorf("Parse(invalid YAML) error = %v", err)
	}
}
