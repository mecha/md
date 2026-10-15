package main

import (
	"maps"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"md/pkg/document"
	"go.yaml.in/yaml/v3"
)

const usage = `usage: md FILE [options]

Create or patch a Markdown document with YAML frontmatter.

Options:
  --mode MODE        create, patch, or auto (default: auto)
  --set FIELD=YAML   set a frontmatter field; repeatable
  --unset FIELD      remove a frontmatter field; repeatable; ignored in create mode
  --body MD          replace the Markdown body
  --body-file FILE   replace the body with the contents of FILE
  --body-stdin       replace the body with standard input
  -h, --help         show this help

Modes:
  create             create FILE; exit 2 if it already exists
  patch              patch FILE; exit 3 if it does not exist
  auto               patch FILE if it exists, otherwise create it

FIELD=YAML is split at the first '='. Quote a field as YAML when it contains
'=', for example --set '"field=with=equals"=value'.`

type mode uint8

const (
	modeAuto mode = iota
	modeCreate
	modePatch
)

type fieldOperation struct {
	name  string
	value any
	unset bool
}

type options struct {
	path       string
	mode       mode
	operations []fieldOperation
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

	info, statErr := os.Lstat(opts.path)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return 1, fmt.Errorf("inspect %q: %w", opts.path, statErr)
	}

	effectiveMode := opts.mode
	if effectiveMode == modeAuto {
		if exists {
			effectiveMode = modePatch
		} else {
			effectiveMode = modeCreate
		}
	}
	if effectiveMode == modeCreate && exists {
		return 2, fmt.Errorf("create %q: file already exists", opts.path)
	}
	if effectiveMode == modePatch && !exists {
		return 3, fmt.Errorf("patch %q: file does not exist", opts.path)
	}

	body, bodySet, err := readBody(opts, stdin)
	if err != nil {
		return 1, err
	}
	if effectiveMode == modePatch && len(opts.operations) == 0 && !bodySet {
		return 0, nil
	}

	var original []byte
	doc := document.Document{Fields: make(map[string]any)}
	hadFrontmatter := false
	var originalFields map[string]any
	var originalBody string
	if effectiveMode == modePatch {
		if !info.Mode().IsRegular() {
			return 1, fmt.Errorf("patch %q: not a regular file", opts.path)
		}
		original, err = os.ReadFile(opts.path)
		if err != nil {
			return 1, fmt.Errorf("read %q: %w", opts.path, err)
		}
		doc, err = document.Parse("", original)
		if err != nil {
			return 1, fmt.Errorf("parse %q: %w", opts.path, err)
		}
		hadFrontmatter = hasFrontmatter(original)
		originalFields = make(map[string]any, len(doc.Fields))
		maps.Copy(originalFields, doc.Fields)
		originalBody = doc.Body
	}

	for _, operation := range opts.operations {
		if operation.unset {
			if effectiveMode == modePatch {
				delete(doc.Fields, operation.name)
			}
			continue
		}
		doc.Fields[operation.name] = operation.value
	}
	if bodySet {
		doc.Body = body
	}
	if effectiveMode == modePatch && reflect.DeepEqual(doc.Fields, originalFields) && doc.Body == originalBody {
		return 0, nil
	}

	data, err := marshalDocument(doc, hadFrontmatter)
	if err != nil {
		return 1, err
	}
	if effectiveMode == modePatch && bytes.Equal(data, original) {
		return 0, nil
	}
	if effectiveMode == modeCreate {
		if err := writeNew(opts.path, data); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return 2, fmt.Errorf("create %q: file already exists", opts.path)
			}
			return 1, fmt.Errorf("create %q: %w", opts.path, err)
		}
		return 0, nil
	}
	if err := writeAtomic(opts.path, data, info.Mode().Perm()); err != nil {
		return 1, fmt.Errorf("patch %q: %w", opts.path, err)
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
		case "--mode":
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
		case "--set":
			value, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			field, yamlValue, err := parseSetArgument(value)
			if err != nil {
				return options{}, false, fmt.Errorf("%w\n%s", err, usage)
			}
			var parsed any
			if err := yaml.Unmarshal([]byte(yamlValue), &parsed); err != nil {
				return options{}, false, fmt.Errorf("invalid YAML for field %q: %w", field, err)
			}
			opts.operations = append(opts.operations, fieldOperation{name: field, value: parsed})
		case "--unset":
			field, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			if field == "" {
				return options{}, false, fmt.Errorf("option --unset requires a non-empty field\n%s", usage)
			}
			opts.operations = append(opts.operations, fieldOperation{name: field, unset: true})
		case "--body":
			value, err := nextValue()
			if err != nil {
				return options{}, false, err
			}
			if bodySource != "" {
				return options{}, false, fmt.Errorf("body options are mutually exclusive\n%s", usage)
			}
			bodySource = "body"
			opts.body = &value
		case "--body-file":
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
		case "--body-stdin":
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

func parseSetArgument(argument string) (field, value string, err error) {
	if argument == "" {
		return "", "", errors.New("option --set requires FIELD=YAML")
	}
	if argument[0] != '\'' && argument[0] != '"' {
		field, value, found := strings.Cut(argument, "=")
		if !found || field == "" {
			return "", "", errors.New("option --set requires FIELD=YAML")
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
		return "", "", errors.New("quoted --set field must be followed by '='")
	}
	if err := yaml.Unmarshal([]byte(argument[:closing+1]), &field); err != nil {
		return "", "", fmt.Errorf("invalid quoted --set field: %w", err)
	}
	if field == "" {
		return "", "", errors.New("option --set requires a non-empty field")
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

func hasFrontmatter(data []byte) bool {
	return bytes.HasPrefix(data, []byte("---\n")) || bytes.HasPrefix(data, []byte("---\r\n"))
}

func marshalDocument(doc document.Document, preserveEmptyFrontmatter bool) ([]byte, error) {
	if len(doc.Fields) == 0 {
		if preserveEmptyFrontmatter {
			return []byte("---\n---\n" + doc.Body), nil
		}
		return []byte(doc.Body), nil
	}
	data, err := doc.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal document: %w", err)
	}
	return data, nil
}

func writeNew(path string, data []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func writeAtomic(path string, data []byte, permissions fs.FileMode) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".md-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(permissions); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
