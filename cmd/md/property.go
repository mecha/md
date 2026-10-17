package main

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
	"md/internal/yamldata"
	"md/pkg/md"
)

func parseProperty(argument string) (md.Property, error) {
	name, source, err := parsePropArgument(argument)
	if err != nil {
		return md.Property{}, err
	}
	value, err := yamldata.Decode([]byte(source))
	if err != nil {
		return md.Property{}, fmt.Errorf("invalid YAML for property %q: %w", name, err)
	}
	return md.Property{Name: name, Value: value}, nil
}

func parsePropArgument(argument string) (field, value string, err error) {
	if argument == "" {
		return "", "", errors.New("option --yaml requires NAME=YAML")
	}
	if argument[0] != '\'' && argument[0] != '"' {
		field, value, found := strings.Cut(argument, "=")
		if !found || field == "" {
			return "", "", errors.New("option --yaml requires NAME=YAML")
		}
		return field, value, nil
	}

	quote := argument[0]
	closing := -1
	for index := 1; index < len(argument); index++ {
		switch quote {
		case '\'':
			if argument[index] != '\'' {
				continue
			}
			if index+1 < len(argument) && argument[index+1] == '\'' {
				index++
				continue
			}
			closing = index
		case '"':
			if argument[index] == '\\' {
				index++
				continue
			}
			if argument[index] == '"' {
				closing = index
			}
		}
		if closing >= 0 {
			break
		}
	}
	if closing < 0 || closing+1 >= len(argument) || argument[closing+1] != '=' {
		return "", "", errors.New("quoted --yaml field must be followed by '='")
	}
	if err := yaml.Unmarshal([]byte(argument[:closing+1]), &field); err != nil {
		return "", "", fmt.Errorf("invalid quoted --yaml field: %w", err)
	}
	if field == "" {
		return "", "", errors.New("option --yaml requires a non-empty field")
	}
	return field, argument[closing+2:], nil
}
