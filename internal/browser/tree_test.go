package browser

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestNewTreeDoesNotReadRecursively(t *testing.T) {
	fake := newFakeFileSystem()
	rootPath := filepath.Join("workspace", "root")

	tree := mustTree(t, rootPath, fake)
	if got := fake.calls(); len(got) != 0 {
		t.Fatalf("constructor calls = %v, want no filesystem I/O", got)
	}
	if tree.Root().Path() != absoluteTestPath(t, rootPath) {
		t.Fatalf("root path = %q, want absolute path", tree.Root().Path())
	}
	if tree.Root().Loaded() || tree.Root().Loading() {
		t.Fatalf("root state = loaded %v, loading %v, want both false", tree.Root().Loaded(), tree.Root().Loading())
	}
	rows := tree.VisibleRows()
	if len(rows) != 1 || rows[0].Node != tree.Root() || rows[0].Depth != 0 {
		t.Fatalf("initial rows = %#v, want root row at depth 0", rows)
	}
}

func TestTreeReadRejectsInvalidRequestWithoutFilesystemAccess(t *testing.T) {
	fake := newFakeFileSystem()
	tree := mustTree(t, filepath.Join("workspace", "root"), fake)

	result := tree.Read(LoadRequest{})
	if !errors.Is(result.Err, errInvalidLoadRequest) {
		t.Fatalf("Read(invalid) error = %v, want %v", result.Err, errInvalidLoadRequest)
	}
	if got := fake.calls(); len(got) != 0 {
		t.Fatalf("Read(invalid) filesystem calls = %v, want none", got)
	}

	invalidTree := &Tree{}
	result = invalidTree.Read(LoadRequest{Path: "/tmp"})
	if !errors.Is(result.Err, errInvalidLoadRequest) {
		t.Fatalf("Read(nil filesystem) error = %v, want %v", result.Err, errInvalidLoadRequest)
	}
}

func TestTreeLoadsDirectoriesLazilySortsHiddenEntriesAndExcludesGit(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	fake := newFakeFileSystem()
	fake.set(rootPath, []filesystem.Entry{
		{Name: "z-file", Mode: 0},
		{Name: ".hidden-file", Mode: 0},
		{Name: "visible-dir", Mode: fs.ModeDir},
		{Name: ".hidden-dir", Mode: fs.ModeDir},
		{Name: ".git", Mode: fs.ModeDir},
		{Name: "directory-link", Mode: fs.ModeSymlink},
	})
	tree := mustTree(t, rootPath, fake)

	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("first root expand started no load")
	}
	if got := fake.calls(); len(got) != 0 {
		t.Fatalf("Expand() calls = %v, want read deferred to request execution", got)
	}
	if !tree.Root().Expanded() || !tree.Root().Loading() {
		t.Fatalf("root state = expanded %v, loading %v, want both true", tree.Root().Expanded(), tree.Root().Loading())
	}

	if !tree.ApplyLoad(tree.Read(request)) {
		t.Fatal("ApplyLoad() rejected root result")
	}
	if got := fake.calls(); !reflect.DeepEqual(got, []string{tree.Root().Path()}) {
		t.Fatalf("filesystem calls = %v, want root only", got)
	}

	wantNames := []string{".hidden-dir", "visible-dir", ".hidden-file", "directory-link", "z-file"}
	children := tree.Root().Children()
	if len(children) != len(wantNames) {
		t.Fatalf("children length = %d, want %d", len(children), len(wantNames))
	}
	for i, want := range wantNames {
		if children[i].Name() != want {
			t.Errorf("child[%d].Name() = %q, want %q", i, children[i].Name(), want)
		}
		if children[i].Parent() != tree.Root() {
			t.Errorf("child[%d].Parent() = %p, want root %p", i, children[i].Parent(), tree.Root())
		}
		if children[i].Path() != filepath.Join(tree.Root().Path(), want) {
			t.Errorf("child[%d].Path() = %q, want joined absolute path", i, children[i].Path())
		}
	}
	if !children[0].IsDirectory() || !children[1].IsDirectory() {
		t.Fatal("directory entries were not sorted before files")
	}
	if children[3].IsDirectory() || !children[3].IsSymlink() {
		t.Fatalf("directory-link kind = %v, symlink = %v, want symlink only", children[3].Kind(), children[3].IsSymlink())
	}

	rows := tree.VisibleRows()
	if len(rows) != 6 {
		t.Fatalf("visible rows length = %d, want root plus five children", len(rows))
	}
	for i, row := range rows {
		if row.Node == nil || row.Path != row.Node.Path() || row.Depth != 0 && row.Depth != 1 {
			t.Errorf("row[%d] = %#v, want O(1) path/node/depth data", i, row)
		}
		if i == 0 && row.Depth != 0 {
			t.Errorf("root row depth = %d, want 0", row.Depth)
		}
		if i > 0 && row.Depth != 1 {
			t.Errorf("child row[%d] depth = %d, want 1", i, row.Depth)
		}
	}
}

func TestApplyLoadDoesNotMutateInputEntries(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	tree := mustTree(t, rootPath, newFakeFileSystem())
	request, ok := tree.RequestLoad(tree.Root())
	if !ok {
		t.Fatal("RequestLoad(root) started no load")
	}

	entries := []filesystem.Entry{
		{Name: "file", Mode: 0},
		{Name: "directory", Mode: fs.ModeDir},
	}
	wantEntries := append([]filesystem.Entry(nil), entries...)
	if !tree.ApplyLoad(LoadResult{Node: request.Node, Path: request.Path, Entries: entries}) {
		t.Fatal("ApplyLoad() rejected result")
	}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("input entries = %#v, want unchanged %#v", entries, wantEntries)
	}
}

func TestChildrenReturnsDefensiveShallowCopy(t *testing.T) {
	root := newRootNode("/workspace/root")
	child := newChildNode(root, "/workspace/root/child", "child", KindFile)
	root.children = []*Node{child}

	children := root.Children()
	children[0] = nil

	if root.children[0] != child {
		t.Fatalf("internal child = %p, want original child %p", root.children[0], child)
	}
}

func TestTreeSuppressesDuplicateLoadsAndReusesCollapsedCache(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	fake := newFakeFileSystem()
	fake.set(rootPath, []filesystem.Entry{{Name: "child", Mode: fs.ModeDir}})
	fake.set(filepath.Join(absoluteTestPath(t, rootPath), "child"), nil)
	tree := mustTree(t, rootPath, fake)

	first, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("first Expand() started no load")
	}
	if _, ok := tree.Expand(tree.Root()); ok {
		t.Fatal("second Expand() started duplicate load")
	}
	if !tree.Root().Loading() {
		t.Fatal("root Loading() = false while first request is outstanding")
	}
	if !tree.ApplyLoad(tree.Read(first)) {
		t.Fatal("ApplyLoad() rejected first result")
	}
	child := tree.Root().Children()[0]
	childRequest, ok := tree.Expand(child)
	if !ok {
		t.Fatal("child Expand() started no load")
	}
	if _, ok := tree.RequestLoad(child); ok {
		t.Fatal("RequestLoad() started duplicate child load")
	}

	childResult := tree.Read(childRequest)
	if !tree.ApplyLoad(childResult) {
		t.Fatal("ApplyLoad() rejected child result")
	}
	if !tree.Collapse(child) {
		t.Fatal("Collapse(child) = false, want collapse")
	}
	rowsAfterCollapse := tree.VisibleRows()
	if len(rowsAfterCollapse) != 2 {
		t.Fatalf("collapsed rows = %d, want root and child", len(rowsAfterCollapse))
	}
	if _, ok := tree.Expand(child); ok {
		t.Fatal("re-expanding loaded child started I/O")
	}
	if got := fake.calls(); !reflect.DeepEqual(got, []string{tree.Root().Path(), child.Path()}) {
		t.Fatalf("filesystem calls = %v, want one call per directory", got)
	}
	if !child.Loaded() || child.Loading() {
		t.Fatalf("child state = loaded %v, loading %v, want loaded and idle", child.Loaded(), child.Loading())
	}
}

func TestTreeKeepsNestedDirectoriesLazy(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	fake := newFakeFileSystem()
	fake.set(rootPath, []filesystem.Entry{{Name: "dir", Mode: fs.ModeDir}})
	childPath := filepath.Join(absoluteTestPath(t, rootPath), "dir")
	fake.set(childPath, []filesystem.Entry{
		{Name: ".git", Mode: fs.ModeDir},
		{Name: "nested-file", Mode: 0},
	})
	tree := mustTree(t, rootPath, fake)

	rootRequest, _ := tree.Expand(tree.Root())
	if !tree.ApplyLoad(tree.Read(rootRequest)) {
		t.Fatal("ApplyLoad(root) rejected result")
	}
	child := tree.Root().Children()[0]
	rows := tree.VisibleRows()
	if len(rows) != 2 || rows[1].Node != child || rows[1].Depth != 1 {
		t.Fatalf("before nested expand rows = %#v, want only root and child", rows)
	}
	if child.Loaded() {
		t.Fatal("child is loaded before first expansion")
	}
	if got := fake.calls(); !reflect.DeepEqual(got, []string{tree.Root().Path()}) {
		t.Fatalf("calls before nested expand = %v, want root only", got)
	}

	childRequest, ok := tree.Expand(child)
	if !ok {
		t.Fatal("nested Expand() started no load")
	}
	if !tree.ApplyLoad(tree.Read(childRequest)) {
		t.Fatal("ApplyLoad(child) rejected result")
	}
	rows = tree.VisibleRows()
	if len(rows) != 3 || rows[1].Depth != 1 || rows[2].Depth != 2 || rows[2].Node.Name() != "nested-file" {
		t.Fatalf("nested rows = %#v, want root, child, and nested file", rows)
	}
	if got := fake.calls(); !reflect.DeepEqual(got, []string{tree.Root().Path(), child.Path()}) {
		t.Fatalf("calls after nested expand = %v, want root and child", got)
	}
}

func TestTreeNeverExpandsSymlink(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	fake := newFakeFileSystem()
	fake.set(rootPath, []filesystem.Entry{{Name: "link", Mode: fs.ModeSymlink}})
	tree := mustTree(t, rootPath, fake)
	rootRequest, _ := tree.Expand(tree.Root())
	if !tree.ApplyLoad(tree.Read(rootRequest)) {
		t.Fatal("ApplyLoad(root) rejected result")
	}

	link := tree.Root().Children()[0]
	if !link.IsSymlink() || link.IsDirectory() {
		t.Fatalf("link state = symlink %v, directory %v, want symlink and not directory", link.IsSymlink(), link.IsDirectory())
	}
	if _, ok := tree.Expand(link); ok {
		t.Fatal("Expand(symlink) started a load")
	}
	if link.Expanded() || link.Loading() || link.Loaded() {
		t.Fatalf("symlink state = expanded %v, loading %v, loaded %v, want all false", link.Expanded(), link.Loading(), link.Loaded())
	}
	if got := fake.calls(); !reflect.DeepEqual(got, []string{tree.Root().Path()}) {
		t.Fatalf("filesystem calls = %v, want no symlink call", got)
	}
}

func TestTreeRetainsRecoverableDirectoryErrorsAndAllowsRetry(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	fake := newFakeFileSystem()
	fake.set(rootPath, []filesystem.Entry{{Name: "missing", Mode: fs.ModeDir}, {Name: "unreadable", Mode: fs.ModeDir}})
	missingPath := filepath.Join(absoluteTestPath(t, rootPath), "missing")
	unreadablePath := filepath.Join(absoluteTestPath(t, rootPath), "unreadable")
	fake.setError(missingPath, fs.ErrNotExist)
	fake.setError(unreadablePath, fs.ErrPermission)
	tree := mustTree(t, rootPath, fake)
	rootRequest, _ := tree.Expand(tree.Root())
	if !tree.ApplyLoad(tree.Read(rootRequest)) {
		t.Fatal("ApplyLoad(root) rejected result")
	}

	children := tree.Root().Children()
	for _, child := range children {
		request, ok := tree.Expand(child)
		if !ok {
			t.Fatalf("Expand(%q) started no load", child.Name())
		}
		if _, ok := tree.RequestLoad(child); ok {
			t.Fatalf("RequestLoad(%q) started duplicate load", child.Name())
		}
		result := tree.Read(request)
		if !tree.ApplyLoad(result) {
			t.Fatalf("ApplyLoad(%q) rejected recoverable error", child.Name())
		}
		if child.Loading() || child.Loaded() {
			t.Fatalf("%q state = loading %v, loaded %v, want idle and unloaded", child.Name(), child.Loading(), child.Loaded())
		}
		if !errors.Is(child.LoadError(), fake.errors[child.Path()]) {
			t.Fatalf("%q LoadError() = %v, want %v", child.Name(), child.LoadError(), fake.errors[child.Path()])
		}
		retryRequest, ok := tree.RequestLoad(child)
		if !ok {
			t.Fatalf("RequestLoad(%q) did not allow retry", child.Name())
		}
		if !tree.ApplyLoad(tree.Read(retryRequest)) {
			t.Fatalf("ApplyLoad(%q retry) rejected recoverable error", child.Name())
		}
	}
}

func TestVisibleRowsDirtyOnlyChangesOnTreeStructureOperations(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	fake := newFakeFileSystem()
	fake.set(rootPath, []filesystem.Entry{{Name: "file", Mode: 0}})
	tree := mustTree(t, rootPath, fake)
	if !tree.VisibleRowsDirty() {
		t.Fatal("new tree VisibleRowsDirty() = false, want true")
	}
	_ = tree.VisibleRows()
	if tree.VisibleRowsDirty() {
		t.Fatal("VisibleRowsDirty() = true after rebuild, want false")
	}

	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.VisibleRowsDirty() {
		t.Fatal("VisibleRowsDirty() = false after expand, want true")
	}
	_ = tree.VisibleRows()
	if tree.VisibleRowsDirty() {
		t.Fatal("VisibleRowsDirty() = true after expand rebuild, want false")
	}
	if tree.Root().Loading() == false {
		t.Fatal("root Loading() = false while request is outstanding")
	}
	if tree.VisibleRowsDirty() {
		t.Fatal("starting a load dirtied rows without a structure change")
	}

	if !tree.ApplyLoad(tree.Read(request)) {
		t.Fatal("ApplyLoad() rejected result")
	}
	if !tree.VisibleRowsDirty() {
		t.Fatal("VisibleRowsDirty() = false after children were added, want true")
	}
	_ = tree.VisibleRows()
	if tree.VisibleRowsDirty() {
		t.Fatal("VisibleRowsDirty() = true after load rebuild, want false")
	}
}

func TestApplyLoadRejectsStaleResultWithoutClearingCurrentRequest(t *testing.T) {
	rootPath := filepath.Join("workspace", "root")
	fake := newFakeFileSystem()
	tree := mustTree(t, rootPath, fake)
	request, ok := tree.RequestLoad(tree.Root())
	if !ok {
		t.Fatal("RequestLoad(root) started no load")
	}
	if tree.ApplyLoad(LoadResult{Node: tree.Root(), Path: filepath.Join(rootPath, "other")}) {
		t.Fatal("ApplyLoad() accepted mismatched path")
	}
	if !tree.Root().Loading() {
		t.Fatal("root Loading() = false after stale result, want request retained")
	}
	if !tree.ApplyLoad(tree.Read(request)) {
		t.Fatal("ApplyLoad(valid) rejected request")
	}
	if tree.ApplyLoad(tree.Read(request)) {
		t.Fatal("ApplyLoad() accepted duplicate result")
	}
}

func mustTree(t *testing.T, rootPath string, fake *fakeFileSystem) *Tree {
	t.Helper()
	tree, err := NewTree(rootPath, fake)
	if err != nil {
		t.Fatalf("NewTree() error = %v", err)
	}
	return tree
}

func absoluteTestPath(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs(%q) error = %v", path, err)
	}
	return filepath.Clean(absolute)
}

type fakeFileSystem struct {
	directories map[string][]filesystem.Entry
	errors      map[string]error
	readCalls   []string
}

func newFakeFileSystem() *fakeFileSystem {
	return &fakeFileSystem{
		directories: make(map[string][]filesystem.Entry),
		errors:      make(map[string]error),
	}
}

func (f *fakeFileSystem) set(path string, entries []filesystem.Entry) {
	f.directories[absoluteFakePath(path)] = append([]filesystem.Entry(nil), entries...)
}

func (f *fakeFileSystem) setError(path string, err error) {
	f.errors[absoluteFakePath(path)] = err
}

func (f *fakeFileSystem) ReadDir(path string) ([]filesystem.Entry, error) {
	path = absoluteFakePath(path)
	f.readCalls = append(f.readCalls, path)
	if err, ok := f.errors[path]; ok {
		return nil, err
	}
	entries, ok := f.directories[path]
	if !ok {
		return nil, fmt.Errorf("fake directory %q not configured", path)
	}
	return append([]filesystem.Entry(nil), entries...), nil
}

func (f *fakeFileSystem) calls() []string {
	return append([]string(nil), f.readCalls...)
}

func absoluteFakePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}
