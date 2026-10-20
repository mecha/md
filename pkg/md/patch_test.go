package md

import (
	"errors"
	"reflect"
	"testing"
)

func TestApplyProperties(t *testing.T) {
	original := Document{Props: []Property{{"z", 1}, {"a", map[string]any{"keep": true, "old": false}}, {"remove", true}}, Body: "body"}
	patch := DocumentPatch{
		SetProps:   []Property{{"a", map[string]any{"new": true}}, {"c", 1}, {"b", 2}, {"c", 3}, {"remove", false}, {"null", nil}},
		UnsetProps: []string{"remove", "remove", "absent"},
	}
	result, err := patch.Apply(original)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names(result), []string{"z", "a", "c", "b", "null"}) {
		t.Fatalf("order: %v", names(result))
	}
	if result.Props[2].Value != int64(3) || result.Props[4].Value != nil || result.Body != "body" {
		t.Fatalf("result: %#v", result)
	}
	if !reflect.DeepEqual(result.Props[1].Value, map[string]any{"new": true}) {
		t.Fatal("nested mapping was merged")
	}
	if len(original.Props) != 3 || len(original.Props[1].Value.(map[string]any)) != 2 {
		t.Fatal("input changed")
	}
}

func TestApplyBody(t *testing.T) {
	for _, tt := range []struct {
		op         BodyOperation
		body, want string
	}{
		{KeepBody, "ignored", "old"}, {ReplaceBody, "new", "new"}, {ReplaceBody, "", ""},
		{AppendBody, "new", "oldnew"}, {PrependBody, "new", "newold"}, {DeleteBody, "ignored", ""},
	} {
		result, err := (DocumentPatch{BodyOperation: tt.op, Body: tt.body}).Apply(Document{Body: "old"})
		if err != nil || result.Body != tt.want {
			t.Errorf("operation %d: %q %v", tt.op, result.Body, err)
		}
	}
}

func TestApplyDoesNotAliasInputs(t *testing.T) {
	original := Document{Props: []Property{{"existing", map[string]any{"items": []any{"old"}}}}}
	patch := DocumentPatch{SetProps: []Property{{"new", map[string]any{"items": []any{"new"}}}}}
	result, err := patch.Apply(original)
	if err != nil {
		t.Fatal(err)
	}
	result.Props[0].Value.(map[string]any)["items"].([]any)[0] = "changed"
	result.Props[1].Value.(map[string]any)["items"].([]any)[0] = "changed"
	result.Props[0].Name = "changed"
	if original.Props[0].Name != "existing" || original.Props[0].Value.(map[string]any)["items"].([]any)[0] != "old" || patch.SetProps[0].Value.(map[string]any)["items"].([]any)[0] != "new" {
		t.Fatal("inputs share memory")
	}
}

func TestApplyValidation(t *testing.T) {
	for _, patch := range []DocumentPatch{
		{BodyOperation: -1}, {BodyOperation: 100}, {UnsetProps: []string{""}}, {UnsetProps: []string{"\xff"}},
		{SetProps: []Property{{"", nil}}}, {SetProps: []Property{{"a", func() {}}}, UnsetProps: []string{"a"}},
		{SetProps: []Property{{"a", func() {}}, {"a", true}}},
	} {
		if _, err := patch.Apply(Document{}); !errors.Is(err, ErrInvalidPatch) {
			t.Errorf("patch %#v: %v", patch, err)
		}
	}
	if _, err := (DocumentPatch{}).Apply(Document{Props: []Property{{"a", 1}, {"a", 2}}}); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("bad document: %v", err)
	}
	if _, err := (DocumentPatch{SetProps: []Property{{"a", new(int)}}}).Apply(Document{}); !errors.Is(err, ErrInvalidPatch) || !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("invalid set must wrap patch and document errors: %v", err)
	}
	original := Document{Props: []Property{{"a", true}}, Body: "body"}
	result, err := (DocumentPatch{}).Apply(original)
	if err != nil || !reflect.DeepEqual(result, original) {
		t.Fatalf("zero patch: %#v %v", result, err)
	}
}
