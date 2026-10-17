package md

import (
	"fmt"
	"unicode/utf8"
)

// BodyOperation specifies how a patch changes the Markdown body.
type BodyOperation int

const (
	KeepBody BodyOperation = iota
	ReplaceBody
	AppendBody
	PrependBody
	DeleteBody
)

// DocumentPatch applies all sets, then all unsets, then a body operation.
// The zero value preserves all properties and the body.
type DocumentPatch struct {
	SetProps      []Property
	UnsetProps    []string
	Body          string
	BodyOperation BodyOperation
}

// Apply validates and returns an independent document. Nested values are
// replaced, not merged. Unsets win over sets regardless of their order.
func (p DocumentPatch) Apply(d Document) (Document, error) {
	if p.BodyOperation < KeepBody || p.BodyOperation > DeleteBody {
		return Document{}, fmt.Errorf("%w: unknown body operation %d", ErrInvalidPatch, p.BodyOperation)
	}
	result, err := cloneDocument(d)
	if err != nil {
		return Document{}, err
	}
	positions := make(map[string]int, len(result.Props))
	for i, prop := range result.Props {
		positions[prop.Name] = i
	}
	for _, prop := range p.SetProps {
		// Validate separately because repeated sets are legal, unlike duplicate
		// property names in a document.
		validated, err := cloneDocument(Document{Props: []Property{prop}})
		if err != nil {
			return Document{}, fmt.Errorf("%w: %w", ErrInvalidPatch, err)
		}
		prop = validated.Props[0]
		if i, ok := positions[prop.Name]; ok {
			result.Props[i] = prop
		} else {
			positions[prop.Name] = len(result.Props)
			result.Props = append(result.Props, prop)
		}
	}
	unset := make(map[string]bool, len(p.UnsetProps))
	for _, name := range p.UnsetProps {
		if name == "" || !utf8.ValidString(name) {
			return Document{}, fmt.Errorf("%w: unset name must be a nonempty UTF-8 string", ErrInvalidPatch)
		}
		unset[name] = true
	}
	kept := make([]Property, 0, len(result.Props))
	for _, prop := range result.Props {
		if !unset[prop.Name] {
			kept = append(kept, prop)
		}
	}
	result.Props = kept
	switch p.BodyOperation {
	case ReplaceBody:
		result.Body = p.Body
	case AppendBody:
		result.Body += p.Body
	case PrependBody:
		result.Body = p.Body + result.Body
	case DeleteBody:
		result.Body = ""
	}
	return result, nil
}
