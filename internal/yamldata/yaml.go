// Package yamldata decodes one YAML value without silently accepting extra
// documents or unsupported tags. It is shared by front matter and CLI inputs.
package yamldata

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"go.yaml.in/yaml/v3"
)

// MaxDepth limits the number of nested mappings and sequences in a value.
const MaxDepth = 100

// Parse returns a validated document node, or an empty node for empty input.
func Parse(data []byte) (*yaml.Node, error) {
	root, _, err := parse(data)
	return root, err
}

// Decode returns the parsed value; empty input represents null.
func Decode(data []byte) (any, error) {
	_, value, err := parse(data)
	return value, err
}

func parse(data []byte) (*yaml.Node, any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil && err != io.EOF {
		return nil, nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("expected exactly one YAML document")
	}
	if _, err := validate(&root, make(map[*yaml.Node]nodeState), 0); err != nil {
		return nil, nil, err
	}
	// Decode the complete value to catch invalid scalars and excessive alias
	// expansion before individual nodes are used by callers.
	var value any
	if err := root.Decode(&value); err != nil {
		return nil, nil, err
	}
	return &root, value, nil
}

type nodeState struct {
	active bool
	height int
}

// validate caches subtree heights, not just whether a node has been visited:
// an alias can reuse an already-validated subtree along a deeper path.
func validate(n *yaml.Node, states map[*yaml.Node]nodeState, depth int) (int, error) {
	if state, ok := states[n]; ok {
		if state.active {
			return 0, fmt.Errorf("cyclic YAML alias")
		}
		if depth+state.height > MaxDepth {
			return 0, fmt.Errorf("YAML nesting exceeds %d levels", MaxDepth)
		}
		return state.height, nil
	}
	switch n.Tag {
	case "", "!!map", "!!seq", "!!str", "!!bool", "!!int", "!!float", "!!null", "!!timestamp", "!!binary":
	default:
		return 0, fmt.Errorf("unsupported YAML tag %q", n.Tag)
	}
	level := 0
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
		level = 1
	}
	if depth+level > MaxDepth {
		return 0, fmt.Errorf("YAML nesting exceeds %d levels", MaxDepth)
	}
	states[n] = nodeState{active: true}
	children := n.Content
	if n.Kind == yaml.AliasNode {
		children = []*yaml.Node{n.Alias}
	}
	height := level
	for _, child := range children {
		childHeight, err := validate(child, states, depth+level)
		if err != nil {
			return 0, err
		}
		height = max(height, level+childHeight)
	}
	if n.Kind == yaml.MappingNode {
		if err := validateMappingKeys(n); err != nil {
			return 0, err
		}
	}
	states[n] = nodeState{height: height}
	return height, nil
}

func validateMappingKeys(n *yaml.Node) error {
	// The YAML decoder checks source spellings, but keys such as 1 and 0x1
	// decode to the same Go key and would silently overwrite one another.
	seen := make(map[any]int, len(n.Content)/2)
	type nanKey struct{}
	for i := 0; i < len(n.Content); i += 2 {
		key := n.Content[i]
		for key.Kind == yaml.AliasNode {
			key = key.Alias
		}
		if key.Kind != yaml.ScalarNode {
			return fmt.Errorf("mapping key must be scalar")
		}
		var value any
		if err := key.Decode(&value); err != nil {
			return err
		}
		if f, ok := value.(float64); ok && math.IsNaN(f) {
			value = nanKey{}
		}
		if line, exists := seen[value]; exists {
			return fmt.Errorf("line %d: mapping key %q already defined at line %d", n.Content[i].Line, key.Value, line)
		}
		seen[value] = n.Content[i].Line
	}
	return nil
}
