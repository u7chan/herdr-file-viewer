package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalReadDirPreservesEntryTypeBits(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "child-dir"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file"), []byte("content"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkPath := filepath.Join(directory, "link-to-dir")
	if err := os.Symlink(filepath.Join(directory, "child-dir"), linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	entries, err := NewLocal().ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	byName := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	if !byName["child-dir"].IsDirectory() {
		t.Fatalf("child-dir = %#v, want directory", byName["child-dir"])
	}
	if byName["file"].IsDirectory() || byName["file"].IsSymlink() {
		t.Fatalf("file = %#v, want non-directory, non-symlink", byName["file"])
	}
	if !byName["link-to-dir"].IsSymlink() {
		t.Fatalf("link-to-dir = %#v, want symlink", byName["link-to-dir"])
	}
	if byName["link-to-dir"].Mode&fs.ModeDir != 0 {
		t.Fatalf("link-to-dir mode = %v, want no directory bit", byName["link-to-dir"].Mode)
	}
}

func TestLocalReadDirReturnsOperatingSystemError(t *testing.T) {
	_, err := NewLocal().ReadDir(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("ReadDir(missing) error = nil, want error")
	}
}
