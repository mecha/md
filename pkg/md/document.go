package md

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"go.yaml.in/yaml/v3"
)

// ErrInvalidFrontMatter indicates that a document starts front matter but does
// not contain valid YAML surrounded by delimiter lines.
var ErrInvalidFrontMatter = errors.New("invalid document front matter")

// document is a Markdown document. Fields contains the YAML front matter
// without imposing a schema, so extension fields survive read-modify-write
// operations. FieldOrder records the source order of parsed fields; callers may
// leave it empty for new documents. Body is the exact content after the closing
// delimiter line.
type document struct {
	Fields     map[string]any
	FieldOrder []string
	Body       string
}

// parse parses data as a document. Only a delimiter on the first line
// starts YAML front matter; otherwise all data is returned as Body.
func parse(data []byte) (document, error) {
	doc := document{Fields: make(map[string]any)}

	start, ok := openingDelimiterEnd(data)
	if !ok {
		doc.Body = string(data)
		return doc, nil
	}

	frontEnd, bodyStart, ok := closingDelimiter(data, start)
	if !ok {
		return document{}, fmt.Errorf("%w: missing closing delimiter", ErrInvalidFrontMatter)
	}

	front := data[start:frontEnd]
	if len(bytes.TrimSpace(front)) != 0 {
		var root yaml.Node
		if err := yaml.Unmarshal(front, &root); err != nil {
			return document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
		}
		if err := yaml.Unmarshal(front, &doc.Fields); err != nil {
			return document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
		}
		if doc.Fields == nil {
			doc.Fields = make(map[string]any)
		}
		doc.FieldOrder = mappingKeyOrder(&root)
	}
	doc.Body = string(data[bodyStart:])
	return doc, nil
}

// Marshal serializes a document using ordered YAML front matter. Existing
// fields retain their parsed relative order, while fields not in FieldOrder are
// appended in alphabetical order. The Body bytes are
// appended unchanged after the closing delimiter line.
func (d document) Marshal() ([]byte, error) {
	fields := d.Fields
	if fields == nil {
		fields = map[string]any{}
	}

	mapping := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, key := range d.orderedFieldNames(fields) {
		keyNode := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
		var valueNode yaml.Node
		if err := valueNode.Encode(fields[key]); err != nil {
			return nil, fmt.Errorf("marshal document front matter field %q: %w", key, err)
		}
		mapping.Content = append(mapping.Content, &keyNode, &valueNode)
	}
	front, err := yaml.Marshal(&mapping)
	if err != nil {
		return nil, fmt.Errorf("marshal document front matter: %w", err)
	}

	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(front)
	out.WriteString("---\n")
	out.WriteString(d.Body)
	return out.Bytes(), nil
}

func mappingKeyOrder(root *yaml.Node) []string {
	if root == nil || len(root.Content) == 0 {
		return nil
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	order := make([]string, 0, len(mapping.Content)/2)
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		order = append(order, mapping.Content[index].Value)
	}
	return order
}

func (d document) orderedFieldNames(fields map[string]any) []string {
	order := make([]string, 0, len(fields))
	seen := make(map[string]bool)
	for _, key := range d.FieldOrder {
		if _, present := fields[key]; present && !seen[key] {
			order = append(order, key)
			seen[key] = true
		}
	}
	var added []string
	for key := range fields {
		if !seen[key] {
			added = append(added, key)
		}
	}
	sort.Strings(added)
	return append(order, added...)
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
		next := len(data)
		contentEnd := len(data)
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
