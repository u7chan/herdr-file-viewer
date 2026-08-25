// Package filesystem defines the small directory-reading boundary used by the
// browser package.
package filesystem

import (
	"io/fs"
	"os"
)

// Entry is the directory metadata needed to build a browser node. Mode is
// taken from DirEntry.Type, so a symlink can be identified without following
// it.
type Entry struct {
	Name string
	Mode fs.FileMode
}

// IsDirectory reports whether the entry itself is a directory. Symlinks are
// deliberately excluded even when their target is a directory.
func (e Entry) IsDirectory() bool {
	return !e.IsSymlink() && e.Mode.IsDir()
}

// IsSymlink reports whether the entry itself is a symbolic link.
func (e Entry) IsSymlink() bool {
	return e.Mode&fs.ModeSymlink != 0
}

// FileSystem is the only filesystem operation needed by the browser. It
// returns directory metadata, not file contents or follow-up stat results.
type FileSystem interface {
	ReadDir(path string) ([]Entry, error)
}

// Local reads directories through the operating system filesystem.
type Local struct{}

// NewLocal constructs the standard-library filesystem adapter.
func NewLocal() Local {
	return Local{}
}

// ReadDir reads one directory without following symlinks in its entries.
func (Local) ReadDir(path string) ([]Entry, error) {
	directory, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(directory))
	for _, entry := range directory {
		entries = append(entries, Entry{
			Name: entry.Name(),
			Mode: entry.Type(),
		})
	}
	return entries, nil
}
