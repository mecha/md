package md

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

var (
	// ErrInvalidFrontMatter identifies malformed YAML front matter.
	ErrInvalidFrontMatter = errors.New("invalid front matter")
	// ErrInvalidDocument identifies invalid names, duplicate properties, or values.
	ErrInvalidDocument = errors.New("invalid document")
	// ErrInvalidPatch identifies an invalid patch operation or property.
	ErrInvalidPatch = errors.New("invalid patch")
	// ErrInvalidOptions identifies invalid write options.
	ErrInvalidOptions = errors.New("invalid write options")
)

// Document contains ordered properties and the exact Markdown body bytes.
type Document struct {
	Props []Property `json:"props" yaml:"props"`
	Body  string     `json:"body" yaml:"body"`
}

// Property is a named, parsed YAML value. Names are nonempty and unique in a
// Document. Values are data, not objects with custom serialization behavior.
type Property struct {
	Name  string `json:"name" yaml:"name"`
	Value any    `json:"value" yaml:"value"`
}

// Prop returns the first property whose name exactly matches key.
// If no property matches, it returns Property{} and false.
// The returned property is a shallow copy; nested values are not cloned.
func (d Document) Prop(key string) (Property, bool) {
	for _, prop := range d.Props {
		if prop.Name == key {
			return prop, true
		}
	}
	return Property{}, false
}

// MarshalText returns normalized Markdown with YAML front matter. Property
// order and body bytes are preserved; YAML presentation is not.
func (d Document) MarshalText() ([]byte, error) {
	doc, err := cloneDocument(d)
	if err != nil {
		return nil, err
	}
	return doc.marshalText()
}

// marshalText serializes a document whose properties have already been validated.
func (d Document) marshalText() ([]byte, error) {
	if len(d.Props) == 0 {
		return []byte(d.Body), nil
	}
	mapping := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, prop := range d.Props {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: prop.Name}
		var value yaml.Node
		if err := value.Encode(prop.Value); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
		}
		mapping.Content = append(mapping.Content, key, &value)
	}
	front, err := yaml.Marshal(&mapping)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	data := make([]byte, 0, len(front)+len(d.Body)+8)
	data = append(data, "---\n"...)
	data = append(data, front...)
	data = append(data, "---\n"...)
	data = append(data, d.Body...)
	return data, nil
}

// MarshalJSON implements the ordered {props: [...], body: ...} representation.
// Values with non-string mapping keys, non-finite numbers, or invalid UTF-8
// cannot be represented faithfully and return an error.
func (d Document) MarshalJSON() ([]byte, error) {
	doc, err := cloneDocument(d)
	if err != nil {
		return nil, err
	}
	if !utf8.ValidString(doc.Body) {
		return nil, fmt.Errorf("%w: JSON body is not UTF-8", ErrInvalidDocument)
	}
	for _, prop := range doc.Props {
		if err := validateJSONValue(prop.Value); err != nil {
			return nil, fmt.Errorf("%w: property %q: %v", ErrInvalidDocument, prop.Name, err)
		}
	}
	type wireDocument Document
	data, err := json.Marshal(wireDocument(doc))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	return data, nil
}

// MarshalYAML implements the ordered structured representation, not Markdown.
func (d Document) MarshalYAML() (any, error) {
	doc, err := cloneDocument(d)
	if err != nil {
		return nil, err
	}
	if !utf8.ValidString(doc.Body) {
		return nil, fmt.Errorf("%w: YAML body is not UTF-8", ErrInvalidDocument)
	}
	type wireDocument Document
	return wireDocument(doc), nil
}
