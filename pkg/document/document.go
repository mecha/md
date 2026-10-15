// Package document parses and writes Forge Markdown documents.
package document

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ErrInvalidFrontMatter indicates that a document starts front matter but does
// not contain valid YAML surrounded by delimiter lines.
var ErrInvalidFrontMatter = errors.New("invalid document front matter")

// Document is a Forge Markdown document. Fields contains the YAML front matter
// without imposing a schema, so extension fields survive read-modify-write
// operations. FieldOrder records the source order of parsed fields; callers may
// leave it empty for new documents. Body is the exact content after the closing
// delimiter line.
type Document struct {
	ID         string
	Fields     map[string]any
	FieldOrder []string
	Body       string
}

// Parse parses data as a document with id. Only a delimiter on the first line
// starts YAML front matter; otherwise all data is returned as Body.
func Parse(id string, data []byte) (Document, error) {
	doc := Document{ID: id, Fields: make(map[string]any)}

	start, ok := openingDelimiterEnd(data)
	if !ok {
		doc.Body = string(data)
		return doc, nil
	}

	frontEnd, bodyStart, ok := closingDelimiter(data, start)
	if !ok {
		return Document{}, fmt.Errorf("%w: missing closing delimiter", ErrInvalidFrontMatter)
	}

	front := data[start:frontEnd]
	if len(bytes.TrimSpace(front)) != 0 {
		var root yaml.Node
		if err := yaml.Unmarshal(front, &root); err != nil {
			return Document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
		}
		if err := yaml.Unmarshal(front, &doc.Fields); err != nil {
			return Document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
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
// placed deterministically according to the document type. The Body bytes are
// appended unchanged after the closing delimiter line.
func (d Document) Marshal() ([]byte, error) {
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

func (d Document) orderedFieldNames(fields map[string]any) []string {
	canonical := canonicalFieldOrder(d.ID)
	ranks := make(map[string]int, len(canonical))
	for index, key := range canonical {
		ranks[key] = index
	}
	extensionRank := len(canonical) - 2

	order := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, key := range d.FieldOrder {
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		if _, present := fields[key]; !present {
			continue
		}
		order = append(order, key)
		seen[key] = struct{}{}
	}

	// Insert standard fields in canonical order. Existing keys are only anchors:
	// their relative order is never changed, even when it was non-canonical.
	for _, key := range canonical {
		if _, present := fields[key]; !present {
			continue
		}
		if _, present := seen[key]; present {
			continue
		}
		rank := ranks[key]
		insertAt := 0
		for index, existing := range order {
			existingRank, known := ranks[existing]
			if !known {
				existingRank = extensionRank
			}
			if existingRank < rank {
				insertAt = index + 1
			}
		}
		for insertAt < len(order) {
			existingRank, known := ranks[order[insertAt]]
			if !known {
				existingRank = extensionRank
			}
			if existingRank > rank {
				break
			}
			insertAt++
		}
		order = insertString(order, insertAt, key)
		seen[key] = struct{}{}
	}

	var extensions []string
	for key := range fields {
		if _, present := seen[key]; present {
			continue
		}
		extensions = append(extensions, key)
	}
	sort.Strings(extensions)
	if len(extensions) != 0 {
		insertAt := trailingTimestampStart(order)
		order = insertStrings(order, insertAt, extensions)
	}
	return order
}

func canonicalFieldOrder(id string) []string {
	root, _, _ := strings.Cut(id, "/")
	var fields []string
	switch root {
	case "users":
		fields = []string{"username", "avatar"}
	case "wiki":
		fields = []string{"title", "draft", "tags"}
	case "issues":
		fields = []string{"title", "status", "labels", "assignees", "dependencies", "duplicates", "subtasks"}
	case "pulls":
		fields = []string{"title", "draft", "status", "labels", "assignees", "closes", "base_ref", "base_sha", "head_ref", "head_sha", "merged", "merged_at"}
	case "comments":
		fields = []string{"parent", "review", "commit", "file", "start_line", "end_line", "resolved"}
	}
	fields = append(fields, "author", "created", "updated")
	return fields
}

func trailingTimestampStart(order []string) int {
	index := len(order)
	for index > 0 {
		key := order[index-1]
		if key != "created" && key != "updated" {
			break
		}
		index--
	}
	return index
}

func insertString(values []string, index int, value string) []string {
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}

func insertStrings(values []string, index int, additions []string) []string {
	values = append(values, make([]string, len(additions))...)
	copy(values[index+len(additions):], values[index:len(values)-len(additions)])
	copy(values[index:], additions)
	return values
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
