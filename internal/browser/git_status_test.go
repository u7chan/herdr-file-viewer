package browser

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestTreeMapsGitStatusesToLoadedAndUnloadedPaths(t *testing.T) {
	root := t.TempDir()
	fake := &gitStatusFileSystem{fakeFileSystem: newFakeFileSystem()}
	fake.set(root, []filesystem.Entry{
		{Name: "directory", Mode: fs.ModeDir},
		{Name: "clean", Mode: 0},
	})
	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "directory/modified", Status: filesystem.GitStatusModified},
		{Path: "directory/untracked", Status: filesystem.GitStatusUntracked},
		{Path: "deleted", Status: filesystem.GitStatusDeleted},
		{Path: "../outside", Status: filesystem.GitStatusUnmerged},
	}

	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	result := tree.ReadInitial(request)
	if fake.statusCalls != 1 {
		t.Fatalf("ReadInitial() Git status calls = %d, want 1", fake.statusCalls)
	}
	if got := tree.GitStatusForPath(root); got != GitStatusNone {
		t.Fatalf("status before ApplyLoad = %v, want none", got)
	}
	if !tree.ApplyLoad(result) {
		t.Fatal("ApplyLoad() rejected initial result")
	}

	directory := tree.Root().Children()[0]
	if got := tree.GitStatusForPath(directory.Path()); got != GitStatusModified {
		t.Fatalf("directory aggregate status = %v, want modified", got)
	}
	if got := tree.GitStatusForPath(filepath.Join(root, "deleted")); got != GitStatusDeleted {
		t.Fatalf("unloaded deleted status = %v, want deleted", got)
	}
	if got := tree.GitStatusForPath(filepath.Join(root, "clean")); got != GitStatusNone {
		t.Fatalf("clean status = %v, want none", got)
	}
	if got := tree.GitStatusForPath(filepath.Join(root, "..", "outside")); got != GitStatusNone {
		t.Fatalf("outside status = %v, want none", got)
	}

	if fake.statusCalls != 1 {
		t.Fatalf("status calls after ApplyLoad = %d, want 1", fake.statusCalls)
	}
}

func TestTreeGitStatusErrorDisablesColoring(t *testing.T) {
	root := t.TempDir()
	fake := &gitStatusFileSystem{
		fakeFileSystem: newFakeFileSystem(),
		statusError:    errors.New("not a git worktree"),
	}
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.ReadInitial(request)) {
		t.Fatal("ApplyLoad() rejected initial result")
	}
	if got := tree.GitStatusForPath(filepath.Join(root, "file")); got != GitStatusNone {
		t.Fatalf("non-Git status = %v, want none", got)
	}
}

type gitStatusFileSystem struct {
	*fakeFileSystem
	statuses    []filesystem.GitStatusEntry
	statusError error
	statusCalls int
}

func mustGitStatusTree(t *testing.T, root string, fileSystem filesystem.FileSystem) *Tree {
	t.Helper()
	tree, err := NewTree(root, fileSystem)
	if err != nil {
		t.Fatalf("NewTree() error = %v", err)
	}
	return tree
}

func (f *gitStatusFileSystem) ReadGitStatus(string) ([]filesystem.GitStatusEntry, error) {
	f.statusCalls++
	if f.statusError != nil {
		return nil, f.statusError
	}
	return append([]filesystem.GitStatusEntry(nil), f.statuses...), nil
}
