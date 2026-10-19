package md

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/mecha/md/internal/yamldata"
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

func parse(data []byte) (Document, error) {
	doc := Document{Props: []Property{}, Body: string(data)}
	start, ok := openingDelimiterEnd(data)
	if !ok {
		return doc, nil
	}
	frontEnd, bodyStart, ok := closingDelimiter(data, start)
	if !ok {
		return Document{}, fmt.Errorf("%w: missing closing delimiter", ErrInvalidFrontMatter)
	}
	root, err := yamldata.Parse(data[start:frontEnd])
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
	}
	if len(root.Content) > 0 {
		mapping := root.Content[0]
		// yaml represents comments-only input as an implicit empty null scalar.
		empty := mapping.Kind == yaml.ScalarNode && mapping.Tag == "!!null" && mapping.Value == "" && mapping.Style == 0
		if !empty {
			if mapping.Kind != yaml.MappingNode {
				return Document{}, fmt.Errorf("%w: expected a mapping", ErrInvalidFrontMatter)
			}
			for i := 0; i < len(mapping.Content); i += 2 {
				key := mapping.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" {
					return Document{}, fmt.Errorf("%w: property names must be nonempty strings", ErrInvalidFrontMatter)
				}
				var value any
				if err := mapping.Content[i+1].Decode(&value); err != nil {
					return Document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
				}
				doc.Props = append(doc.Props, Property{Name: key.Value, Value: value})
			}
		}
	}
	doc.Body = string(data[bodyStart:])
	result, err := cloneDocument(doc)
	if err != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrInvalidFrontMatter, err)
	}
	return result, nil
}

// MarshalText returns normalized Markdown with YAML front matter. Property
// order and body bytes are preserved; YAML presentation is not.
func (d Document) MarshalText() ([]byte, error) {
	doc, err := cloneDocument(d)
	if err != nil {
		return nil, err
	}
	if len(doc.Props) == 0 {
		return []byte(doc.Body), nil
	}
	mapping := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, prop := range doc.Props {
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
	return []byte("---\n" + string(front) + "---\n" + doc.Body), nil
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
	for i := range doc.Props {
		doc.Props[i].Value, err = jsonValue(doc.Props[i].Value)
		if err != nil {
			return nil, fmt.Errorf("%w: property %q: %v", ErrInvalidDocument, doc.Props[i].Name, err)
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

func cloneDocument(d Document) (Document, error) {
	result := Document{Props: make([]Property, 0, len(d.Props)), Body: d.Body}
	seen := make(map[string]bool, len(d.Props))
	for _, prop := range d.Props {
		if prop.Name == "" || !utf8.ValidString(prop.Name) || seen[prop.Name] {
			return Document{}, fmt.Errorf("%w: empty, invalid, or duplicate property name %q", ErrInvalidDocument, prop.Name)
		}
		seen[prop.Name] = true
		value, err := cloneValue(reflect.ValueOf(prop.Value), make(map[visit]bool), 0)
		if err != nil {
			return Document{}, fmt.Errorf("%w: property %q: %v", ErrInvalidDocument, prop.Name, err)
		}
		// Normalized Go keys can still collide in YAML (for example int(1)
		// and uint(1)). Validate the encoding too, so Apply and Write cannot
		// produce documents that Read would reject.
		encoded, err := yaml.Marshal(value)
		if err == nil {
			_, err = yamldata.Parse(encoded)
		}
		if err != nil {
			return Document{}, fmt.Errorf("%w: property %q: %v", ErrInvalidDocument, prop.Name, err)
		}
		result.Props = append(result.Props, Property{Name: prop.Name, Value: value})
	}
	return result, nil
}

type visit struct {
	typ    reflect.Type
	ptr    uintptr
	length int
}

func cloneValue(v reflect.Value, active map[visit]bool, depth int) (any, error) {
	if depth > 100 {
		return nil, fmt.Errorf("value nesting exceeds 100 levels")
	}
	if !v.IsValid() {
		return nil, nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		return cloneValue(v.Elem(), active, depth+1)
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		timestamp := v.Interface().(time.Time)
		if _, err := timestamp.MarshalText(); err != nil {
			return nil, fmt.Errorf("invalid timestamp: %w", err)
		}
		return timestamp, nil
	}
	if v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		id := visit{v.Type(), v.Pointer(), v.Len()}
		if active[id] {
			return nil, fmt.Errorf("cyclic value")
		}
		active[id] = true
		defer delete(active, id)
	}
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return nil, fmt.Errorf("value is not UTF-8")
		}
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.Array, reflect.Slice:
		values := make([]any, v.Len())
		for i := range values {
			value, err := cloneValue(v.Index(i), active, depth+1)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return values, nil
	case reflect.Map:
		values := make(map[any]any, v.Len())
		stringsOnly := true
		iter := v.MapRange()
		for iter.Next() {
			key, err := cloneValue(iter.Key(), active, depth+1)
			if err != nil {
				return nil, err
			}
			if key != nil && !reflect.TypeOf(key).Comparable() {
				return nil, fmt.Errorf("mapping key must be scalar")
			}
			if _, ok := key.(string); !ok {
				stringsOnly = false
			}
			if _, exists := values[key]; exists {
				return nil, fmt.Errorf("duplicate normalized mapping key")
			}
			value, err := cloneValue(iter.Value(), active, depth+1)
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
		if stringsOnly {
			result := make(map[string]any, len(values))
			for key, value := range values {
				result[key.(string)] = value
			}
			return result, nil
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported value type %s", v.Type())
	}
}

func jsonValue(value any) (any, error) {
	switch v := value.(type) {
	case map[any]any:
		return nil, fmt.Errorf("JSON requires string mapping keys")
	case map[string]any:
		for key, child := range v {
			normalized, err := jsonValue(child)
			if err != nil {
				return nil, err
			}
			v[key] = normalized
		}
	case []any:
		for i, child := range v {
			normalized, err := jsonValue(child)
			if err != nil {
				return nil, err
			}
			v[i] = normalized
		}
	}
	return value, nil
}

func openingDelimiterEnd(data []byte) (int, bool) {
	if bytes.HasPrefix(data, []byte("---\n")) {
		return 4, true
	}
	if bytes.HasPrefix(data, []byte("---\r\n")) {
		return 5, true
	}
	return 0, false
}

func closingDelimiter(data []byte, start int) (frontEnd, bodyStart int, ok bool) {
	for lineStart := start; lineStart <= len(data); {
		lineEnd := bytes.IndexByte(data[lineStart:], '\n')
		next, contentEnd := len(data), len(data)
		if lineEnd >= 0 {
			contentEnd = lineStart + lineEnd
			next = contentEnd + 1
		}
		if contentEnd > lineStart && data[contentEnd-1] == '\r' {
			contentEnd--
		}
		if bytes.Equal(data[lineStart:contentEnd], []byte("---")) {
			return lineStart, next, true
		}
		if lineEnd < 0 {
			break
		}
		lineStart = next
	}
	return 0, 0, false
}
