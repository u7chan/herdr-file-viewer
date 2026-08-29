package browser

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

var errInvalidLoadRequest = errors.New("invalid filesystem load request")

const gitEntryName = ".git"

// LoadRequest identifies one directory read. It is safe to execute the read
// without mutating the Tree and to apply the result later in the owner of the
// tree's update loop.
type LoadRequest struct {
	Node *Node
	Path string
}

// LoadResult is the data returned by a directory read. Err is retained on the
// target node by ApplyLoad and is intentionally recoverable.
type LoadResult struct {
	Node         *Node
	Path         string
	Entries      []filesystem.Entry
	Err          error
	GitStatus    GitStatusResult
	HasGitStatus bool
}

// GitStatus is the status type exposed by the browser layer.
type GitStatus = filesystem.GitStatus

const (
	GitStatusNone      = filesystem.GitStatusNone
	GitStatusModified  = filesystem.GitStatusModified
	GitStatusUntracked = filesystem.GitStatusUntracked
	GitStatusAdded     = filesystem.GitStatusAdded
	GitStatusUnmerged  = filesystem.GitStatusUnmerged
	GitStatusDeleted   = filesystem.GitStatusDeleted
)

// GitStatusResult is the result of the one initial Git status snapshot.
type GitStatusResult struct {
	Entries []filesystem.GitStatusEntry
	Err     error
}

// Tree owns the lazy node graph and its flattened-row cache.
type Tree struct {
	fileSystem      filesystem.FileSystem
	root            *Node
	visible         VisibleRows
	gitStatusLoaded bool
	gitStatuses     map[string]GitStatus
}

// NewTree creates a tree without reading rootPath or any of its descendants.
// rootPath is made absolute so every node path has the same invariant.
func NewTree(rootPath string, fileSystem filesystem.FileSystem) (*Tree, error) {
	if fileSystem == nil {
		return nil, errors.New("filesystem is nil")
	}

	absoluteRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("make root path absolute: %w", err)
	}

	return &Tree{
		fileSystem:  fileSystem,
		root:        newRootNode(filepath.Clean(absoluteRoot)),
		visible:     NewVisibleRows(),
		gitStatuses: make(map[string]GitStatus),
	}, nil
}

// Root returns the permanently retained root node.
func (t *Tree) Root() *Node {
	if t == nil {
		return nil
	}
	return t.root
}

// RequestLoad starts at most one read for an unloaded directory. The returned
// request contains no mutable tree operation; the caller performs it and then
// supplies its result to ApplyLoad.
func (t *Tree) RequestLoad(node *Node) (LoadRequest, bool) {
	if t == nil || !t.contains(node) || !node.IsDirectory() || node.loaded || node.loading {
		return LoadRequest{}, false
	}

	node.loading = true
	node.loadError = nil
	return LoadRequest{Node: node, Path: node.path}, true
}

// Expand marks a directory expanded and starts its first or retry read when
// necessary. Symlinks and files are never made expandable.
func (t *Tree) Expand(node *Node) (LoadRequest, bool) {
	if t == nil || !t.contains(node) || !node.IsDirectory() {
		return LoadRequest{}, false
	}

	if !node.expanded {
		node.expanded = true
		t.visible.MarkDirty()
	}
	return t.RequestLoad(node)
}

// Collapse hides a directory's descendants while retaining its loaded cache.
func (t *Tree) Collapse(node *Node) bool {
	if t == nil || !t.contains(node) || !node.IsDirectory() || !node.expanded {
		return false
	}

	node.expanded = false
	t.visible.MarkDirty()
	return true
}

// Read executes a request without changing any node state. It is intended to
// be called from a command goroutine; ApplyLoad is the mutation boundary.
func (t *Tree) Read(request LoadRequest) LoadResult {
	result := LoadResult{Node: request.Node, Path: request.Path}
	if t == nil || t.fileSystem == nil || request.Node == nil || request.Path == "" {
		result.Err = errInvalidLoadRequest
		return result
	}

	result.Entries, result.Err = t.fileSystem.ReadDir(request.Path)
	return result
}

// ReadInitial reads the root directory and the initial Git snapshot in the
// same command. It performs no tree mutation; the caller applies the result
// through ApplyLoad in its update loop.
func (t *Tree) ReadInitial(request LoadRequest) LoadResult {
	result := t.Read(request)
	if t != nil && request.Node == t.root {
		result.GitStatus = t.ReadGitStatus()
		result.HasGitStatus = true
	}
	return result
}

// ReadGitStatus reads the optional Git status capability without mutating the
// tree. A filesystem without that capability behaves like a clean/non-Git
// directory.
func (t *Tree) ReadGitStatus() GitStatusResult {
	if t == nil || t.fileSystem == nil || t.root == nil {
		return GitStatusResult{}
	}
	reader, ok := t.fileSystem.(filesystem.GitStatusReader)
	if !ok {
		return GitStatusResult{}
	}
	entries, err := reader.ReadGitStatus(t.root.path)
	return GitStatusResult{Entries: entries, Err: err}
}

// ApplyLoad applies a result only to the node that is still waiting for that
// request. Stale or duplicate results are ignored. A filesystem error clears
// loading but leaves the node unloaded and retryable.
func (t *Tree) ApplyLoad(result LoadResult) bool {
	if t == nil || !t.contains(result.Node) || !result.Node.IsDirectory() || !result.Node.loading {
		return false
	}
	if result.Path != result.Node.path {
		return false
	}
	if result.HasGitStatus {
		t.applyGitStatus(result.GitStatus)
	}

	node := result.Node
	node.loading = false
	if result.Err != nil {
		node.loadError = result.Err
		return true
	}

	children := make([]*Node, 0, len(result.Entries))
	entries := slices.Clone(result.Entries)
	slices.SortStableFunc(entries, func(left, right filesystem.Entry) int {
		leftDirectory := left.IsDirectory()
		rightDirectory := right.IsDirectory()
		if leftDirectory != rightDirectory {
			if leftDirectory {
				return -1
			}
			return 1
		}
		return cmp.Compare(left.Name, right.Name)
	})

	for _, entry := range entries {
		if entry.Name == gitEntryName {
			continue
		}
		children = append(children, newChildNode(
			node,
			filepath.Join(node.path, entry.Name),
			entry.Name,
			kindForEntry(entry),
		))
	}

	node.children = children
	node.loaded = true
	node.loadError = nil
	t.visible.MarkDirty()
	return true
}

// GitStatusForPath returns the direct or descendant status assigned to path.
// The aggregate map includes paths that are not currently loaded in the lazy
// tree, so directory colors do not depend on expansion order.
func (t *Tree) GitStatusForPath(path string) GitStatus {
	if t == nil || !t.gitStatusLoaded {
		return GitStatusNone
	}
	path, ok := t.statusPath(path)
	if !ok {
		return GitStatusNone
	}
	return t.gitStatuses[path]
}

func (t *Tree) applyGitStatus(result GitStatusResult) {
	t.gitStatusLoaded = true
	if t.gitStatuses == nil {
		t.gitStatuses = make(map[string]GitStatus)
	}
	clear(t.gitStatuses)
	if result.Err != nil || t.root == nil {
		return
	}

	for _, entry := range result.Entries {
		path, ok := t.statusPath(entry.Path)
		if !ok || entry.Status == GitStatusNone {
			continue
		}
		for {
			t.gitStatuses[path] = combineGitStatuses(t.gitStatuses[path], entry.Status)
			if path == t.root.path {
				break
			}
			parent := filepath.Dir(path)
			if parent == path {
				break
			}
			path = parent
		}
	}
}

func (t *Tree) statusPath(path string) (string, bool) {
	if t == nil || t.root == nil || path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.root.path, path)
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(t.root.path, path)
	if err != nil || relative == ".." || (len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}

func combineGitStatuses(current, next GitStatus) GitStatus {
	if gitStatusPriority(next) > gitStatusPriority(current) {
		return next
	}
	return current
}

func gitStatusPriority(status GitStatus) int {
	switch status {
	case GitStatusUnmerged:
		return 4
	case GitStatusDeleted:
		return 3
	case GitStatusModified:
		return 2
	case GitStatusUntracked, GitStatusAdded:
		return 1
	default:
		return 0
	}
}

// VisibleRows returns the root-first flattening, rebuilding it only after a
// structural invalidation.
func (t *Tree) VisibleRows() []VisibleRow {
	if t == nil {
		return nil
	}
	return t.visible.Rows(t.root)
}

// VisibleRowsDirty reports whether the next VisibleRows call will rebuild the
// flattened cache.
func (t *Tree) VisibleRowsDirty() bool {
	return t != nil && t.visible.Dirty()
}

func (t *Tree) contains(node *Node) bool {
	if t == nil || node == nil {
		return false
	}
	for current := node; current != nil; current = current.parent {
		if current == t.root {
			return true
		}
	}
	return false
}

func kindForEntry(entry filesystem.Entry) NodeKind {
	if entry.IsSymlink() {
		return KindSymlink
	}
	if entry.IsDirectory() {
		return KindDirectory
	}
	return KindFile
}
