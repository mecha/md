// Package yamldata decodes one YAML value without silently accepting extra
// documents or unsupported tags. It is shared by front matter and CLI inputs.
package yamldata

import (
	"bytes"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// Parse returns a document node, or an empty node for empty input.
func Parse(data []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil && err != io.EOF {
		return nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("expected exactly one YAML document")
	}
	if err := validate(&root, make(map[*yaml.Node]bool), make(map[*yaml.Node]bool), 0); err != nil {
		return nil, err
	}
	// Decode the complete value to catch duplicate keys, invalid scalar values,
	// non-comparable keys, and excessive alias expansion before individual use.
	var value any
	if err := root.Decode(&value); err != nil {
		return nil, err
	}
	return &root, nil
}

// Decode returns the parsed value; empty input represents null.
func Decode(data []byte) (any, error) {
	root, err := Parse(data)
	if err != nil {
		return nil, err
	}
	var value any
	err = root.Decode(&value)
	return value, err
}

func validate(n *yaml.Node, active, done map[*yaml.Node]bool, depth int) error {
	if depth > 100 {
		return fmt.Errorf("YAML nesting exceeds 100 levels")
	}
	if active[n] {
		return fmt.Errorf("cyclic YAML alias")
	}
	if done[n] {
		return nil
	}
	active[n] = true
	defer func() {
		delete(active, n)
		done[n] = true
	}()
	if n.Kind == yaml.AliasNode {
		return validate(n.Alias, active, done, depth+1)
	}
	switch n.Tag {
	case "", "!!map", "!!seq", "!!str", "!!bool", "!!int", "!!float", "!!null", "!!timestamp", "!!binary":
	default:
		return fmt.Errorf("unsupported YAML tag %q", n.Tag)
	}
	for _, child := range n.Content {
		if err := validate(child, active, done, depth+1); err != nil {
			return err
		}
	}
	return nil
}
