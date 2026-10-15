package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"md/pkg/md"

	"go.yaml.in/yaml/v3"
)

const usage = `usage: md FILE [options]

Create or patch a Markdown document with YAML frontmatter.

Options:
  -m, --mode MODE        create, patch, or auto (default: auto)
  -p, --prop FIELD=YAML  set a frontmatter field; repeatable
      --unset FIELD     remove a field; repeatable; ignored in create mode
  -b, --body MD          replace the Markdown body
  -f, --body-file FILE   replace the body with the contents of FILE
  -s, --body-stdin       replace the body with standard input
  -h, --help             show this help

Modes:
  create             create FILE; exit 2 if it already exists
  patch              patch FILE; exit 3 if it does not exist
  auto               patch FILE if it exists, otherwise create it

FIELD=YAML is split at the first '='. Quote a field as YAML when it contains
'=', for example --prop '"field=with=equals"=value'.`

type mode uint8

const (
	modeAuto mode = iota
	modeCreate
	modePatch
)

type options struct {
	path       string
	mode       mode
	operations []md.Field
	body       *string
	bodyFile   string
	bodyStdin  bool
}

func run(args []string, stdin io.Reader, stdout io.Writer) (int, error) {
	opts, help, err := parseArgs(args)
	if help {
		_, writeErr := fmt.Fprintln(stdout, usage)
		if writeErr != nil {
			return 1, writeErr
		}
		return 0, nil
	}
	if err != nil {
		return 1, err
	}

	body, bodySet, err := readBody(opts, stdin)
	if err != nil {
		return 1, err
	}
	var replacement *string
	if bodySet {
		replacement = &body
	}
	var apply func(string, []md.Field, *string) error
	switch opts.mode {
	case modeCreate:
		apply = md.Create
	case modePatch:
		apply = md.Patch
	default:
		apply = md.Auto
	}
	err = apply(opts.path, opts.operations, replacement)
	if errors.Is(err, fs.ErrExist) {
		return 2, err
	}
	if errors.Is(err, fs.ErrNotExist) && opts.mode == modePatch {
		// A missing body file is handled above; only a missing target is exit 3.
		return 3, err
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func parseArgs(args []string) (options, bool, error) {
	opts := options{mode: modeAuto}
	var bodySource string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-h" || arg == "--help" {
			return options{}, true, nil
		}

		name, inline, hasInline := strings.Cut(arg, "=")
		nextValue := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			index++
			if index >= len(args) {
				return "", fmt.Errorf("option %s requires a value\n%s", name, usage)
			}
			return args[index], nil
		}

		switch name {
		case "--mode", "-m":
			value, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			switch value {
			case "auto":
				opts.mode = modeAuto
			case "create":
				opts.mode = modeCreate
			case "patch":
				opts.mode = modePatch
			default:
				return options{}, false, fmt.Errorf("invalid mode %q; expected create, patch, or auto\n%s", value, usage)
			}
		case "--prop", "-p":
			value, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			field, yamlValue, err := parsePropArgument(value)
			if err != nil {
				return options{}, false, fmt.Errorf("%w\n%s", err, usage)
			}
			var parsed any
			if err := yaml.Unmarshal([]byte(yamlValue), &parsed); err != nil {
				return options{}, false, fmt.Errorf("invalid YAML for field %q: %w", field, err)
			}
			opts.operations = append(opts.operations, md.Field{Name: field, Value: parsed})
		case "--unset":
			field, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			if field == "" {
				return options{}, false, fmt.Errorf("option --unset requires a non-empty field\n%s", usage)
			}
			opts.operations = append(opts.operations, md.Field{Name: field, Unset: true})
		case "--body", "-b":
			value, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			if bodySource != "" {
				return options{}, false, fmt.Errorf("body options are mutually exclusive\n%s", usage)
			}
			bodySource = "body"
			opts.body = &value
		case "--body-file", "-f":
			value, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			if value == "" {
				return options{}, false, fmt.Errorf("option --body-file requires a non-empty file\n%s", usage)
			}
			if bodySource != "" {
				return options{}, false, fmt.Errorf("body options are mutually exclusive\n%s", usage)
			}
			bodySource = "file"
			opts.bodyFile = value
		case "--body-stdin", "-s":
			if hasInline {
				return options{}, false, fmt.Errorf("option --body-stdin does not take a value\n%s", usage)
			}
			if bodySource != "" {
				return options{}, false, fmt.Errorf("body options are mutually exclusive\n%s", usage)
			}
			bodySource = "stdin"
			opts.bodyStdin = true
		default:
			if strings.HasPrefix(arg, "-") {
				return options{}, false, fmt.Errorf("unknown option %q\n%s", arg, usage)
			}
			if hasInline {
				// A positional path may contain '='; use the original argument.
				name = arg
			}
			if opts.path != "" {
				return options{}, false, fmt.Errorf("multiple files specified: %q and %q\n%s", opts.path, name, usage)
			}
			opts.path = name
		}
	}
	if opts.path == "" {
		return options{}, false, fmt.Errorf("FILE is required\n%s", usage)
	}
	return opts, false, nil
}

func parsePropArgument(argument string) (field, value string, err error) {
	if argument == "" {
		return "", "", errors.New("option --prop requires FIELD=YAML")
	}
	if argument[0] != '\'' && argument[0] != '"' {
		field, value, found := strings.Cut(argument, "=")
		if !found || field == "" {
			return "", "", errors.New("option --prop requires FIELD=YAML")
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
		return "", "", errors.New("quoted --prop field must be followed by '='")
	}
	if err := yaml.Unmarshal([]byte(argument[:closing+1]), &field); err != nil {
		return "", "", fmt.Errorf("invalid quoted --prop field: %w", err)
	}
	if field == "" {
		return "", "", errors.New("option --prop requires a non-empty field")
	}
	return field, argument[closing+2:], nil
}

func readBody(opts options, stdin io.Reader) (string, bool, error) {
	if opts.body != nil {
		return *opts.body, true, nil
	}
	if opts.bodyFile != "" {
		data, err := os.ReadFile(opts.bodyFile)
		if err != nil {
			return "", false, fmt.Errorf("read body file %q: %w", opts.bodyFile, err)
		}
		return string(data), true, nil
	}
	if opts.bodyStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", false, fmt.Errorf("read body from stdin: %w", err)
		}
		return string(data), true, nil
	}
	return "", false, nil
}
