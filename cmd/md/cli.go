package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/mecha/md/pkg/md"
	"go.yaml.in/yaml/v3"
)

const usage = `usage: md COMMAND FILE [OPTIONS]

Commands:
  read                  Parse and print a document
  write                 Write a complete document
  patch                 Modify an existing document

Use md COMMAND --help for command options.
  -h, --help            Show help`

const bodyHelp = `  -y, --yaml NAME=YAML   Set a front-matter property; repeatable
  -b, --body MD          Read body content from an argument
  -f, --body-file FILE   Read body content from a file
  -s, --body-stdin       Read body content from standard input`

func helpText(command string) string {
	if command == "" {
		return usage
	}
	header := "usage: md " + command + " FILE [OPTIONS]\n\n"
	switch command {
	case "read":
		header += `  -f, --format FORMAT   Output format: raw (default), yaml, json
  -c, --compact         Compact JSON output; requires --format json`
	case "write":
		header += bodyHelp + `
  -r, --require          Require an existing target
  -x, --exclusive        Create only; fail if the target exists
  -p, --parents          Create missing parent directories
      --file-perms PERM  New file permissions in octal; default 0644
      --dir-perms PERM   New directory permissions in octal; default 0755

Writes the entire document, not a merge. Omitted content is empty.
Require and exclusive are mutually exclusive.`
	case "patch":
		header += bodyHelp + `
  -u, --unset NAME       Remove a property; repeatable; unsets win over sets
  -c, --clear            Clear the body; cannot be combined with body input
  -p, --prepend          Prepend content without a separator
  -a, --append           Append content without a separator

Requires an existing file. Omitted body input preserves the body.
Clear, prepend, and append are mutually exclusive.
Prepend and append require body input.`
	}
	if command != "read" {
		header += `

Body sources are mutually exclusive. Values are parsed as YAML.
Quote names containing '=', e.g. --yaml '"field=with=equals"=value'.`
	}
	return header + "\n\n  -h, --help            Show help"
}

type options struct {
	command, path, format       string
	compact                     bool
	set                         []md.Property
	unset                       []string
	body                        *string
	bodyFile                    string
	bodyStdin                   bool
	operation                   md.BodyOperation
	operationSet                bool
	require, exclusive, parents bool
	filePerms, dirPerms         os.FileMode
}

type flag struct {
	name, short string
	value       bool
}

func commandFlags(command string) []flag {
	flags := []flag{{"help", "h", false}}
	if command == "read" {
		return append(flags, flag{"format", "f", true}, flag{"compact", "c", false})
	}
	flags = append(flags, flag{"yaml", "y", true}, flag{"body", "b", true}, flag{"body-file", "f", true}, flag{"body-stdin", "s", false})
	if command == "write" {
		return append(flags, flag{"require", "r", false}, flag{"exclusive", "x", false}, flag{"parents", "p", false}, flag{"file-perms", "", true}, flag{"dir-perms", "", true})
	}
	return append(flags, flag{"unset", "u", true}, flag{"clear", "c", false}, flag{"prepend", "p", false}, flag{"append", "a", false})
}

func parseArgs(args []string) (options, bool, error) {
	opts := options{format: "raw", filePerms: 0o644, dirPerms: 0o755}
	if len(args) == 0 {
		return opts, false, errors.New("COMMAND is required")
	}
	if args[0] == "-h" || args[0] == "--help" {
		return opts, true, nil
	}
	opts.command = args[0]
	if opts.command != "read" && opts.command != "write" && opts.command != "patch" {
		return opts, false, fmt.Errorf("unknown command %q", opts.command)
	}
	positional := false
	bodySet := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !positional && arg == "--" {
			positional = true
			continue
		}
		if positional || !strings.HasPrefix(arg, "-") || arg == "-" {
			if opts.path != "" || arg == "" {
				return opts, false, errors.New("exactly one nonempty FILE is required")
			}
			opts.path = arg
			continue
		}
		name, value, inline := strings.Cut(arg, "=")
		var spec flag
		found := false
		for _, f := range commandFlags(opts.command) {
			if name == "--"+f.name || (f.short != "" && name == "-"+f.short) {
				spec = f
				found = true
				break
			}
		}
		if !found {
			return opts, false, fmt.Errorf("unknown option %q for %s", name, opts.command)
		}
		if spec.value {
			if !inline {
				i++
				if i >= len(args) {
					return opts, false, fmt.Errorf("option %s requires a value", name)
				}
				value = args[i]
			}
		} else if inline {
			return opts, false, fmt.Errorf("option %s does not take a value", name)
		}
		switch spec.name {
		case "help":
			return opts, true, nil
		case "format":
			if value != "raw" && value != "yaml" && value != "json" {
				return opts, false, fmt.Errorf("invalid output format %q", value)
			}
			opts.format = value
		case "compact":
			opts.compact = true
		case "yaml":
			prop, err := parseProperty(value)
			if err != nil {
				return opts, false, err
			}
			opts.set = append(opts.set, prop)
		case "unset":
			if value == "" {
				return opts, false, errors.New("unset requires a nonempty name")
			}
			opts.unset = append(opts.unset, value)
		case "body", "body-file", "body-stdin":
			if bodySet {
				return opts, false, errors.New("body sources are mutually exclusive")
			}
			bodySet = true
			switch spec.name {
			case "body":
				opts.body = &value
			case "body-file":
				if value == "" {
					return opts, false, errors.New("body-file requires a nonempty path")
				}
				opts.bodyFile = value
			case "body-stdin":
				opts.bodyStdin = true
			}
		case "clear", "prepend", "append":
			if opts.operationSet {
				return opts, false, errors.New("clear, prepend, and append are mutually exclusive")
			}
			opts.operationSet = true
			opts.operation = map[string]md.BodyOperation{"clear": md.DeleteBody, "prepend": md.PrependBody, "append": md.AppendBody}[spec.name]
		case "require":
			opts.require = true
		case "exclusive":
			opts.exclusive = true
		case "parents":
			opts.parents = true
		case "file-perms", "dir-perms":
			perms, err := parsePerms(value)
			if err != nil {
				return opts, false, err
			}
			if spec.name == "file-perms" {
				opts.filePerms = perms
			} else {
				opts.dirPerms = perms
			}
		}
	}
	if opts.path == "" {
		return opts, false, errors.New("FILE is required")
	}
	if opts.compact && opts.format != "json" {
		return opts, false, errors.New("compact requires --format json")
	}
	if opts.require && opts.exclusive {
		return opts, false, errors.New("require and exclusive are mutually exclusive")
	}
	if opts.operation == md.DeleteBody && bodySet {
		return opts, false, errors.New("clear cannot be combined with a body source")
	}
	if (opts.operation == md.AppendBody || opts.operation == md.PrependBody) && !bodySet {
		return opts, false, errors.New("append and prepend require a body source")
	}
	if bodySet && !opts.operationSet {
		opts.operation = md.ReplaceBody
	}
	// Validate all parsed values before reading body input or touching the target.
	_, err := (md.DocumentPatch{SetProps: opts.set, UnsetProps: opts.unset, BodyOperation: opts.operation}).Apply(md.Document{})
	return opts, false, err
}

func parsePerms(value string) (os.FileMode, error) {
	if value == "" {
		return 0, errors.New("permissions must be octal digits between 0000 and 0777")
	}
	for _, digit := range value {
		if digit < '0' || digit > '7' {
			return 0, fmt.Errorf("invalid octal permissions %q", value)
		}
	}
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil || parsed > 0o777 {
		return 0, fmt.Errorf("permissions out of range: %q", value)
	}
	return os.FileMode(parsed), nil
}

func run(args []string, stdin io.Reader, stdout io.Writer) (int, error) {
	opts, help, err := parseArgs(args)
	if help {
		_, err = fmt.Fprintln(stdout, helpText(opts.command))
		if err != nil {
			return 1, err
		}
		return 0, nil
	}
	if err != nil {
		return 1, fmt.Errorf("%w\n%s", err, helpText(validCommand(opts.command)))
	}
	if opts.command == "read" {
		doc, err := md.Read(opts.path)
		if err != nil {
			return fileError(opts, err)
		}
		var data []byte
		switch opts.format {
		case "raw":
			data, err = doc.MarshalText()
		case "yaml":
			data, err = yaml.Marshal(doc)
		case "json":
			if opts.compact {
				data, err = json.Marshal(doc)
			} else {
				data, err = json.MarshalIndent(doc, "", "  ")
			}
			if err == nil {
				data = append(data, '\n')
			}
		}
		if err != nil {
			return 1, err
		}
		n, err := stdout.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		if err != nil {
			return 1, err
		}
		return 0, nil
	}
	body, err := readBody(opts, stdin)
	if err != nil {
		return 1, err
	}
	patch := md.DocumentPatch{SetProps: opts.set, UnsetProps: opts.unset, Body: body, BodyOperation: opts.operation}
	if opts.command == "patch" {
		return fileError(opts, md.Patch(opts.path, patch))
	}
	doc, err := patch.Apply(md.Document{})
	if err != nil {
		return 1, err
	}
	err = md.Write(opts.path, doc, md.RequireFile(opts.require), md.ExclusiveCreate(opts.exclusive), md.CreateParents(opts.parents), md.FilePerms(opts.filePerms), md.DirPerms(opts.dirPerms))
	return fileError(opts, err)
}

func validCommand(command string) string {
	if command == "read" || command == "write" || command == "patch" {
		return command
	}
	return ""
}

func fileError(opts options, err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	if opts.exclusive && errors.Is(err, fs.ErrExist) {
		return 2, err
	}
	if (opts.command == "read" || opts.command == "patch" || opts.require) && errors.Is(err, fs.ErrNotExist) {
		return 3, err
	}
	return 1, err
}

func readBody(opts options, stdin io.Reader) (string, error) {
	if opts.body != nil {
		return *opts.body, nil
	}
	if opts.bodyFile != "" {
		data, err := os.ReadFile(opts.bodyFile)
		if err != nil {
			return "", fmt.Errorf("read body file %q: %w", opts.bodyFile, err)
		}
		return string(data), nil
	}
	if opts.bodyStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read body from stdin: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}
