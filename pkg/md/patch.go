package md

import (
	"fmt"
	"slices"
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
	patch, err := p.normalized()
	if err != nil {
		return Document{}, err
	}
	doc, err := cloneDocument(d)
	if err != nil {
		return Document{}, err
	}
	return patch.applyTo(doc), nil
}

// normalized validates every requested operation, even sets that are later
// overwritten or unset, and copies set values to keep them independent of inputs.
func (p DocumentPatch) normalized() (DocumentPatch, error) {
	if p.BodyOperation < KeepBody || p.BodyOperation > DeleteBody {
		return DocumentPatch{}, fmt.Errorf("%w: unknown body operation %d", ErrInvalidPatch, p.BodyOperation)
	}
	sets := make([]Property, len(p.SetProps))
	for i, prop := range p.SetProps {
		cloned, err := cloneProperty(prop)
		if err != nil {
			return DocumentPatch{}, fmt.Errorf("%w: %w", ErrInvalidPatch, err)
		}
		sets[i] = cloned
	}
	for _, name := range p.UnsetProps {
		if name == "" || !utf8.ValidString(name) {
			return DocumentPatch{}, fmt.Errorf("%w: unset name must be a nonempty UTF-8 string", ErrInvalidPatch)
		}
	}
	p.SetProps = sets
	return p, nil
}

// applyTo takes ownership of a validated document and the normalized set values.
// Keeping this separate lets file patches validate once, before any filesystem I/O.
func (p DocumentPatch) applyTo(doc Document) Document {
	positions := make(map[string]int, len(doc.Props))
	for i, prop := range doc.Props {
		positions[prop.Name] = i
	}
	for _, prop := range p.SetProps {
		if i, ok := positions[prop.Name]; ok {
			doc.Props[i] = prop
		} else {
			positions[prop.Name] = len(doc.Props)
			doc.Props = append(doc.Props, prop)
		}
	}
	if len(p.UnsetProps) > 0 {
		unset := make(map[string]bool, len(p.UnsetProps))
		for _, name := range p.UnsetProps {
			unset[name] = true
		}
		doc.Props = slices.DeleteFunc(doc.Props, func(prop Property) bool {
			return unset[prop.Name]
		})
	}
	switch p.BodyOperation {
	case ReplaceBody:
		doc.Body = p.Body
	case AppendBody:
		doc.Body += p.Body
	case PrependBody:
		doc.Body = p.Body + doc.Body
	case DeleteBody:
		doc.Body = ""
	}
	return doc
}
