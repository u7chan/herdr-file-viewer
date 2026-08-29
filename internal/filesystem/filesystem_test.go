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

func TestLocalReadFileHeadReturnsHeadAndTruncation(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "file.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	emptyPath := filepath.Join(directory, "empty.txt")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	for _, test := range []struct {
		name      string
		path      string
		limit     int
		want      string
		truncated bool
	}{
		{name: "short file returns everything", path: path, limit: 100, want: "0123456789"},
		{name: "exact limit is not truncated", path: path, limit: 10, want: "0123456789"},
		{name: "limit cuts the head", path: path, limit: 4, want: "0123", truncated: true},
		{name: "zero limit of a non-empty file is truncated", path: path, limit: 0, want: "", truncated: true},
		{name: "zero limit of an empty file is not truncated", path: emptyPath, limit: 0, want: ""},
		{name: "negative limit behaves like zero", path: path, limit: -1, want: "", truncated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			content, truncated, err := NewLocal().ReadFileHead(test.path, test.limit)
			if err != nil {
				t.Fatalf("ReadFileHead() error = %v", err)
			}
			if string(content) != test.want {
				t.Fatalf("ReadFileHead() content = %q, want %q", content, test.want)
			}
			if truncated != test.truncated {
				t.Fatalf("ReadFileHead() truncated = %v, want %v", truncated, test.truncated)
			}
		})
	}
}

func TestLocalReadFileHeadFollowsSymlinksAndFailsOnMissingFile(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.txt")
	if err := os.WriteFile(target, []byte("linked"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	link := filepath.Join(directory, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	content, truncated, err := NewLocal().ReadFileHead(link, 8)
	if err != nil {
		t.Fatalf("ReadFileHead(link) error = %v", err)
	}
	if string(content) != "linked" || truncated {
		t.Fatalf("ReadFileHead(link) = %q, truncated %v; want linked content", content, truncated)
	}

	if _, _, err := NewLocal().ReadFileHead(filepath.Join(directory, "missing"), 8); err == nil {
		t.Fatal("ReadFileHead(missing) error = nil, want error")
	}
}

func TestLocalReadFileHeadFailsOnDirectory(t *testing.T) {
	directory := t.TempDir()
	if _, _, err := NewLocal().ReadFileHead(directory, 8); err == nil {
		t.Fatal("ReadFileHead(directory) error = nil, want read error")
	}
}
