package md

import (
	"fmt"
	"os"
)

// WriteOption configures Write. Use the option constructors below.
type WriteOption func(w *writeOptions)

type writeOptions struct {
	requireFile     bool
	exclusiveCreate bool
	createParents   bool
	filePerms       os.FileMode
	dirPerms        os.FileMode
}

// RequireFile makes Write fail if the target is missing. It cannot be combined
// with ExclusiveCreate(true). Parent creation is ignored when enabled.
func RequireFile(require bool) WriteOption { return func(w *writeOptions) { w.requireFile = require } }

// ExclusiveCreate prevents overwriting an existing target using OS-level
// exclusive creation. It cannot be combined with RequireFile(true).
func ExclusiveCreate(exclusive bool) WriteOption {
	return func(w *writeOptions) { w.exclusiveCreate = exclusive }
}

// CreateParents creates missing parent directories for new files.
func CreateParents(create bool) WriteOption {
	return func(w *writeOptions) { w.createParents = create }
}

// FilePerms sets new-file permissions, subject to umask (default 0644).
func FilePerms(perms os.FileMode) WriteOption { return func(w *writeOptions) { w.filePerms = perms } }

// DirPerms sets new-directory permissions, subject to umask (default 0755).
func DirPerms(perms os.FileMode) WriteOption { return func(w *writeOptions) { w.dirPerms = perms } }

func resolveWriteOptions(options []WriteOption) (writeOptions, error) {
	opts := writeOptions{filePerms: 0o644, dirPerms: 0o755}
	for _, option := range options {
		if option == nil {
			return writeOptions{}, fmt.Errorf("%w: nil option", ErrInvalidOptions)
		}
		option(&opts)
	}
	if opts.requireFile && opts.exclusiveCreate {
		return writeOptions{}, fmt.Errorf("%w: require and exclusive are mutually exclusive", ErrInvalidOptions)
	}
	if opts.filePerms & ^os.ModePerm != 0 || opts.dirPerms & ^os.ModePerm != 0 {
		return writeOptions{}, fmt.Errorf("%w: permissions must be between 0000 and 0777", ErrInvalidOptions)
	}
	return opts, nil
}
