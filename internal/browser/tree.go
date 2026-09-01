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
// tree's update loop. IgnoreCandidates carries the root-relative paths for
// one .gitignore check; it is collected on the owner goroutine so a read
// command never walks the node graph.
type LoadRequest struct {
	Node             *Node
	Path             string
	IgnoreCandidates []string
}

// LoadResult is the data returned by a directory read. Err is retained on the
// target node by ApplyLoad and is intentionally recoverable.
type LoadResult struct {
	Node            *Node
	Path            string
	Entries         []filesystem.Entry
	Err             error
	GitStatus       GitStatusResult
	HasGitStatus    bool
	GitIgnore       GitIgnoreResult
	HasGitIgnore    bool
	WorktreeInfo    WorktreeInfoResult
	HasWorktreeInfo bool
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

// GitStatusResult is the result of one Git status snapshot.
type GitStatusResult struct {
	Entries []filesystem.GitStatusEntry
	Err     error
}

// WorktreeInfoResult is the result of one Git branch/worktree snapshot.
type WorktreeInfoResult struct {
	Info filesystem.WorktreeInfo
	Err  error
}

// GitIgnoreResult is the result of one .gitignore match pass. Candidates are
// the paths that were tested (relative to the root); Ignored is the subset
// that matched, so callers can cache both negative and positive results.
type GitIgnoreResult struct {
	Candidates []string
	Ignored    []string
	Err        error
}

// Tree owns the lazy node graph and its flattened-row cache.
type Tree struct {
	fileSystem        filesystem.FileSystem
	root              *Node
	visible           VisibleRows
	gitStatusLoaded   bool
	gitStatuses       map[string]GitStatus
	gitRepository     bool
	gitIgnoreLoaded   bool
	gitIgnore         map[string]bool
	gitWorktreeLoaded bool
	gitWorktreeInfo   filesystem.WorktreeInfo
	gitWorktreeErr    error
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
		gitIgnore:   make(map[string]bool),
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

// Reload invalidates every directory that has cached children or a failed
// read, and returns requests to re-read them. Cached children stay visible
// until the fresh read is applied, so the node graph stays connected and
// expansion state survives; directories with an outstanding in-flight read
// are left to their pending result. The caller issues the returned requests
// and applies the results through ApplyLoad in its update loop.
func (t *Tree) Reload() []LoadRequest {
	if t == nil || t.root == nil {
		return nil
	}

	var requests []LoadRequest
	stack := []*Node{t.root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.loaded || node.loadError != nil {
			node.loading = true
			node.loaded = false
			node.loadError = nil
			request := LoadRequest{Node: node, Path: node.path}
			if node == t.root {
				// Collected here, on the owner goroutine, so the read
				// command never walks the node graph (see LoadRequest).
				request.IgnoreCandidates = t.collectNodePaths()
			}
			requests = append(requests, request)
		}
		for _, child := range node.children {
			if child.IsDirectory() {
				stack = append(stack, child)
			}
		}
	}
	return requests
}

// Read executes a request without changing any node state. Lazy reads also
// capture the current Git status so entries discovered after the initial load
// can be displayed with their current status. It is intended to be called from
// a command goroutine; ApplyLoad is the mutation boundary.
func (t *Tree) Read(request LoadRequest) LoadResult {
	result := t.readFilesystem(request)
	if result.Err != nil || request.Node == t.root {
		return result
	}

	result.GitStatus = t.ReadGitStatus()
	result.HasGitStatus = true
	return result
}

func (t *Tree) readFilesystem(request LoadRequest) LoadResult {
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
	return t.readDirectory(request)
}

// ReadReload executes a reload request. A directory read is plain; the root
// request also refreshes the Git status so a reload reflects filesystem and
// VCS changes together. It performs no tree mutation.
func (t *Tree) ReadReload(request LoadRequest) LoadResult {
	return t.readDirectory(request)
}

func (t *Tree) readDirectory(request LoadRequest) LoadResult {
	result := t.readFilesystem(request)
	if t != nil && request.Node == t.root {
		result.GitStatus = t.ReadGitStatus()
		result.HasGitStatus = true
		// A failed snapshot proves there is no usable working tree, so the
		// check-ignore and worktree subprocesses would only fail again; skip
		// them and let ApplyLoad clear the caches.
		if result.GitStatus.Err != nil {
			result.GitIgnore = GitIgnoreResult{}
			result.HasGitIgnore = true
			return result
		}
		// Fresh entries are candidates even though ApplyLoad has not placed
		// them in the node graph yet; the first load must be able to test
		// the root's children. Each entry name is one level below the root.
		candidates := append([]string(nil), request.IgnoreCandidates...)
		for _, entry := range result.Entries {
			candidates = append(candidates, filepath.ToSlash(entry.Name))
		}
		result.GitIgnore = t.ReadGitIgnore(candidates)
		result.HasGitIgnore = true
		// The branch/worktree snapshot is root-level state, refreshed with
		// the status snapshot on the initial load and every reload. Files
		// without the capability never mark it present, so a zero result
		// cannot make the info line visible.
		if _, ok := t.fileSystem.(filesystem.GitWorktreeReader); ok {
			result.WorktreeInfo = t.ReadWorktreeInfo()
			result.HasWorktreeInfo = true
		}
	}
	return result
}

// ReadGitStatus reads the optional Git status capability without mutating
// the tree. A filesystem without that capability behaves like a clean/
// non-Git directory.
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

// ReadWorktreeInfo reads the optional Git branch/worktree capability without
// mutating the tree. A filesystem without that capability behaves like no
// info.
func (t *Tree) ReadWorktreeInfo() WorktreeInfoResult {
	if t == nil || t.fileSystem == nil || t.root == nil {
		return WorktreeInfoResult{}
	}
	reader, ok := t.fileSystem.(filesystem.GitWorktreeReader)
	if !ok {
		return WorktreeInfoResult{}
	}
	info, err := reader.ReadWorktreeInfo(t.root.path)
	return WorktreeInfoResult{Info: info, Err: err}
}

// ReadGitIgnore runs one batched .gitignore check for the given
// root-relative candidate paths. Candidates are expected to come from the
// owner goroutine (LoadRequest.IgnoreCandidates or fresh root entries).
func (t *Tree) ReadGitIgnore(candidates []string) GitIgnoreResult {
	if t == nil || len(candidates) == 0 || t.fileSystem == nil || t.root == nil {
		return GitIgnoreResult{Candidates: candidates}
	}
	reader, ok := t.fileSystem.(filesystem.GitIgnoreReader)
	if !ok {
		return GitIgnoreResult{Candidates: candidates}
	}
	ignored, err := reader.ReadGitIgnore(t.root.path, candidates)
	return GitIgnoreResult{Candidates: candidates, Ignored: ignored, Err: err}
}

// relativePath converts an absolute path inside the root to its
// slash-separated root-relative form.
func (t *Tree) relativePath(path string) (string, bool) {
	if t == nil || t.root == nil || path == "" {
		return "", false
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(t.root.path, path)
	if err != nil || relative == "." || relative == ".." ||
		(len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

// collectNodePaths returns every node path relative to the root for one
// batched .gitignore check. It must only be called from the tree owner's
// goroutine (Reload) because it walks the node graph.
func (t *Tree) collectNodePaths() []string {
	var paths []string
	var walk func(node *Node)
	walk = func(node *Node) {
		if node == nil {
			return
		}
		if relative, ok := t.relativePath(node.path); ok {
			paths = append(paths, relative)
		}
		for _, child := range node.children {
			walk(child)
		}
	}
	walk(t.root)
	return paths
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
	if result.HasWorktreeInfo {
		t.applyWorktree(result.WorktreeInfo)
	}
	if result.HasGitIgnore {
		t.applyGitIgnore(result.GitIgnore)
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

	// Reuse the node of the same path and kind so a reload keeps expansion
	// state and any already-applied descendant reads, regardless of the order
	// in which the reload results arrive.
	previous := make(map[string]*Node, len(node.children))
	for _, old := range node.children {
		previous[old.path] = old
	}
	for _, entry := range entries {
		if entry.Name == gitEntryName {
			continue
		}
		kind := kindForEntry(entry)
		path := filepath.Join(node.path, entry.Name)
		child := previous[path]
		if child == nil || child.kind != kind {
			child = newChildNode(node, path, entry.Name, kind)
		}
		children = append(children, child)
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

// GitReady reports whether the root is a usable Git working tree, meaning
// the letter column is reserved for every row.
func (t *Tree) GitReady() bool {
	return t != nil && t.gitRepository
}

// WorktreeInfo returns the last branch/worktree snapshot. ok is false until
// the snapshot was applied successfully on a Git-aware filesystem, keeping
// the sticky info line hidden outside repositories.
func (t *Tree) WorktreeInfo() (filesystem.WorktreeInfo, bool) {
	if t == nil || !t.gitWorktreeLoaded || t.gitWorktreeErr != nil {
		return filesystem.WorktreeInfo{}, false
	}
	return t.gitWorktreeInfo, true
}

// IsIgnored reports whether path matches .gitignore rules. Tracked files
// never match. Paths that were not present in the last full snapshot inherit
// the nearest tested ancestor; the next snapshot settles them exactly.
func (t *Tree) IsIgnored(path string) bool {
	if t == nil || !t.gitIgnoreLoaded {
		return false
	}
	path, ok := t.statusPath(path)
	if !ok {
		return false
	}
	for {
		if ignored, ok := t.gitIgnore[path]; ok {
			return ignored
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

// applyGitIgnore replaces the whole snapshot: every candidate is recorded as
// either ignored or not, so a later "not ignored" result for the same path
// cannot be confused with "untested".
func (t *Tree) applyGitIgnore(result GitIgnoreResult) {
	t.gitIgnoreLoaded = true
	if t.gitIgnore == nil {
		t.gitIgnore = make(map[string]bool)
	}
	clear(t.gitIgnore)
	if result.Err != nil || t.root == nil {
		return
	}

	for _, candidate := range result.Candidates {
		if path, ok := t.statusPath(candidate); ok {
			t.gitIgnore[path] = false
		}
	}
	for _, match := range result.Ignored {
		if path, ok := t.statusPath(match); ok {
			t.gitIgnore[path] = true
		}
	}
}

func (t *Tree) applyGitStatus(result GitStatusResult) {
	t.gitStatusLoaded = true
	if t.gitStatuses == nil {
		t.gitStatuses = make(map[string]GitStatus)
	}
	clear(t.gitStatuses)
	if result.Err != nil || t.root == nil {
		t.gitRepository = false
		return
	}
	t.gitRepository = true

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

func (t *Tree) applyWorktree(result WorktreeInfoResult) {
	t.gitWorktreeLoaded = true
	t.gitWorktreeInfo = result.Info
	t.gitWorktreeErr = result.Err
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
