package md

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestDocumentValueNesting(t *testing.T) {
	for _, depth := range []int{49, 98, 99, 100} {
		var value any = "leaf"
		for range depth {
			value = []any{value}
		}
		doc := Document{Props: []Property{{Name: "nested", Value: value}}}
		encoded, err := doc.MarshalText()
		// The front-matter mapping is one level, in addition to the value.
		if depth >= 100 {
			if !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("depth %d: %v", depth, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		roundTrip, err := parse(encoded)
		if err != nil || !reflect.DeepEqual(roundTrip, doc) {
			t.Fatalf("depth %d round trip: %v", depth, err)
		}
		if _, err := json.Marshal(doc); err != nil {
			t.Fatalf("depth %d JSON: %v", depth, err)
		}
		if _, err := yaml.Marshal(doc); err != nil {
			t.Fatalf("depth %d YAML: %v", depth, err)
		}
	}
}

func TestSerializationDoesNotMutateValues(t *testing.T) {
	items := []any{map[string]any{"nested": []any{1, "two"}}}
	doc := Document{Props: []Property{{Name: "items", Value: items}}}
	want := []any{map[string]any{"nested": []any{1, "two"}}}
	for _, marshal := range []func() ([]byte, error){
		doc.MarshalText,
		func() ([]byte, error) { return json.Marshal(doc) },
		func() ([]byte, error) { return yaml.Marshal(doc) },
	} {
		if _, err := marshal(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(items, want) {
			t.Fatalf("serialization mutated input: %#v", items)
		}
	}
}
