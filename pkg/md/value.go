package md

import (
	"fmt"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/mecha/md/internal/yamldata"
	"go.yaml.in/yaml/v3"
)

func cloneDocument(d Document) (Document, error) {
	result := Document{Props: make([]Property, 0, len(d.Props)), Body: d.Body}
	seen := make(map[string]bool, len(d.Props))
	for _, prop := range d.Props {
		if seen[prop.Name] {
			return Document{}, fmt.Errorf("%w: duplicate property name %q", ErrInvalidDocument, prop.Name)
		}
		seen[prop.Name] = true
		cloned, err := cloneProperty(prop)
		if err != nil {
			return Document{}, err
		}
		result.Props = append(result.Props, cloned)
	}
	return result, nil
}

// cloneProperty normalizes data values without invoking user-defined marshalers.
// It is shared by documents and patches, where repeated property sets are legal.
func cloneProperty(prop Property) (Property, error) {
	if prop.Name == "" || !utf8.ValidString(prop.Name) {
		return Property{}, fmt.Errorf("%w: property name must be a nonempty UTF-8 string", ErrInvalidDocument)
	}
	// Account for the surrounding front-matter mapping in the depth limit.
	value, err := cloneValue(reflect.ValueOf(prop.Value), make(map[visit]bool), 1)
	if err != nil {
		return Property{}, fmt.Errorf("%w: property %q: %v", ErrInvalidDocument, prop.Name, err)
	}
	// Normalized Go keys can still collide in YAML (for example int(1)
	// and uint(1)). Check the encoding so Write cannot create unreadable data.
	encoded, err := yaml.Marshal(value)
	if err == nil {
		_, err = yamldata.Parse(encoded)
	}
	if err != nil {
		return Property{}, fmt.Errorf("%w: property %q: %v", ErrInvalidDocument, prop.Name, err)
	}
	return Property{Name: prop.Name, Value: value}, nil
}

type visit struct {
	typ    reflect.Type
	ptr    uintptr
	length int
}

func cloneValue(v reflect.Value, active map[visit]bool, depth int) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		return cloneValue(v.Elem(), active, depth)
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		timestamp := v.Interface().(time.Time)
		if _, err := timestamp.MarshalText(); err != nil {
			return nil, fmt.Errorf("invalid timestamp: %w", err)
		}
		return timestamp, nil
	}
	if v.Kind() == reflect.Map || v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		depth++
		if depth > yamldata.MaxDepth {
			return nil, fmt.Errorf("value nesting exceeds %d levels", yamldata.MaxDepth)
		}
	}
	if v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		id := visit{typ: v.Type(), ptr: v.Pointer(), length: v.Len()}
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
			value, err := cloneValue(v.Index(i), active, depth)
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
			key, err := cloneValue(iter.Key(), active, depth)
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
			value, err := cloneValue(iter.Value(), active, depth)
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

// validateJSONValue checks normalized values; encoding/json checks numbers and
// timestamps. In particular, don't let it silently stringify non-string keys.
func validateJSONValue(value any) error {
	switch v := value.(type) {
	case map[any]any:
		return fmt.Errorf("JSON requires string mapping keys")
	case map[string]any:
		for _, child := range v {
			if err := validateJSONValue(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateJSONValue(child); err != nil {
				return err
			}
		}
	}
	return nil
}
