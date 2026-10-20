package md

import (
	"bytes"
	"fmt"

	"github.com/mecha/md/internal/yamldata"
	"go.yaml.in/yaml/v3"
)

func parse(data []byte) (Document, error) {
	start, ok := openingDelimiterEnd(data)
	if !ok {
		return Document{Props: []Property{}, Body: string(data)}, nil
	}
	frontEnd, bodyStart, ok := closingDelimiter(data, start)
	if !ok {
		return Document{}, fmt.Errorf("%w: missing closing delimiter", ErrInvalidFrontMatter)
	}
	root, err := yamldata.Parse(data[start:frontEnd])
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
	}
	doc := Document{Props: []Property{}, Body: string(data[bodyStart:])}
	if len(root.Content) == 0 {
		return doc, nil
	}
	mapping := root.Content[0]
	// yaml represents comments-only input as an implicit empty null scalar.
	if mapping.Kind == yaml.ScalarNode && mapping.Tag == "!!null" && mapping.Value == "" && mapping.Style == 0 {
		return doc, nil
	}
	if mapping.Kind != yaml.MappingNode {
		return Document{}, fmt.Errorf("%w: expected a mapping", ErrInvalidFrontMatter)
	}
	doc.Props = make([]Property, 0, len(mapping.Content)/2)
	for i := 0; i < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" {
			return Document{}, fmt.Errorf("%w: property names must be nonempty strings", ErrInvalidFrontMatter)
		}
		var value any
		if err := mapping.Content[i+1].Decode(&value); err != nil {
			return Document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
		}
		prop, err := cloneProperty(Property{Name: key.Value, Value: value})
		if err != nil {
			return Document{}, fmt.Errorf("%w: %w", ErrInvalidFrontMatter, err)
		}
		doc.Props = append(doc.Props, prop)
	}
	return doc, nil
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
