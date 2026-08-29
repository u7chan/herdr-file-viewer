package browser

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestTreeAppliesGitIgnoreSnapshotAndInheritsAncestors(t *testing.T) {
	root := t.TempDir()
	fake := &gitIgnoreFileSystem{fakeFileSystem: newFakeFileSystem()}
	fake.set(root, []filesystem.Entry{
		{Name: "dist", Mode: fs.ModeDir},
		{Name: "notes.md", Mode: 0},
	})
	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "notes.md", Status: filesystem.GitStatusModified},
	}
	fake.ignoreMatches = map[string][]string{
		root: {"dist", "dist/a.log"},
	}

	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.ReadInitial(request)) {
		t.Fatal("ApplyLoad() rejected initial result")
	}

	if got := tree.IsIgnored(filepath.Join(root, "dist")); !got {
		t.Fatal("ignored directory not reported as ignored")
	}
	if got := tree.IsIgnored(filepath.Join(root, "dist", "a.log")); !got {
		t.Fatal("ignored file inside ignored directory not reported")
	}
	// A path that was not in the snapshot inherits its nearest tested
	// ancestor: dist is ignored, so any future child is ignored too.
	if got := tree.IsIgnored(filepath.Join(root, "dist", "sub", "deep.txt")); !got {
		t.Fatal("untested path under ignored directory did not inherit ignore state")
	}
	// Tested-but-clean paths stay clean even when a status exists.
	if got := tree.IsIgnored(filepath.Join(root, "notes.md")); got {
		t.Fatal("tracked/status file reported as ignored")
	}
	if got := tree.IsIgnored(filepath.Join(root, "untested.txt")); got {
		t.Fatal("untested path under the root reported as ignored")
	}
	if got := tree.IsIgnored(root); got {
		t.Fatal("root reported as ignored")
	}
}

func TestReadGitIgnoreBatchesAllLoadedNodePaths(t *testing.T) {
	root := t.TempDir()
	fake := &gitIgnoreFileSystem{fakeFileSystem: newFakeFileSystem()}
	fake.set(root, []filesystem.Entry{
		{Name: "dir", Mode: fs.ModeDir},
		{Name: "top.txt", Mode: 0},
	})
	fake.set(filepath.Join(root, "dir"), []filesystem.Entry{{Name: "inner.go", Mode: 0}})

	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.ReadInitial(request)) {
		t.Fatal("ApplyLoad() rejected initial result")
	}

	first := fake.lastIgnoreInput
	if len(first) != 2 || first[0] != "dir" || first[1] != "top.txt" {
		t.Fatalf("initial ignore candidates = %v, want [dir top.txt]", first)
	}

	directory := tree.Root().Children()[0]
	dirRequest, ok := tree.Expand(directory)
	if !ok {
		t.Fatal("Expand(dir) started no load")
	}
	if !tree.ApplyLoad(tree.Read(dirRequest)) {
		t.Fatal("ApplyLoad() rejected directory read")
	}

	reloadRequests := tree.Reload()
	if len(reloadRequests) != 2 {
		t.Fatalf("Reload() requests = %d, want 2", len(reloadRequests))
	}
	for _, reloadRequest := range reloadRequests {
		if !tree.ApplyLoad(tree.ReadReload(reloadRequest)) {
			t.Fatalf("ApplyLoad() rejected reload of %s", reloadRequest.Path)
		}
	}

	last := fake.lastIgnoreInput
	seen := make(map[string]bool, len(last))
	for _, candidate := range last {
		seen[candidate] = true
	}
	for _, want := range []string{"dir", "dir/inner.go", "top.txt"} {
		if !seen[want] {
			t.Fatalf("reload ignore candidates = %v, missing %q", last, want)
		}
	}
}

func TestReloadCarriesIgnoreCandidatesOnlyOnTheRootRequest(t *testing.T) {
	root := t.TempDir()
	fake := &gitIgnoreFileSystem{fakeFileSystem: newFakeFileSystem()}
	fake.set(root, []filesystem.Entry{
		{Name: "dir", Mode: fs.ModeDir},
		{Name: "top.txt", Mode: 0},
	})
	fake.set(filepath.Join(root, "dir"), []filesystem.Entry{{Name: "inner.go", Mode: 0}})

	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.ReadInitial(request)) {
		t.Fatal("ApplyLoad() rejected initial result")
	}
	dirRequest, ok := tree.Expand(tree.Root().Children()[0])
	if !ok {
		t.Fatal("Expand(dir) started no load")
	}
	if !tree.ApplyLoad(tree.Read(dirRequest)) {
		t.Fatal("ApplyLoad() rejected directory read")
	}

	requests := tree.Reload()
	var rootRequest, childRequest *LoadRequest
	for index := range requests {
		if requests[index].Node == tree.Root() {
			rootRequest = &requests[index]
		} else {
			childRequest = &requests[index]
		}
	}
	if rootRequest == nil || childRequest == nil {
		t.Fatalf("Reload() requests = %d, want root and child", len(requests))
	}
	seen := make(map[string]bool, len(rootRequest.IgnoreCandidates))
	for _, candidate := range rootRequest.IgnoreCandidates {
		seen[candidate] = true
	}
	for _, want := range []string{"dir", "dir/inner.go", "top.txt"} {
		if !seen[want] {
			t.Fatalf("root IgnoreCandidates = %v, missing %q", rootRequest.IgnoreCandidates, want)
		}
	}
	if len(childRequest.IgnoreCandidates) != 0 {
		t.Fatalf("child reload request carries candidates: %v", childRequest.IgnoreCandidates)
	}
}

func TestTreeGitIgnoreErrorDisablesIgnoreColoring(t *testing.T) {
	root := t.TempDir()
	fake := &gitIgnoreFileSystem{
		fakeFileSystem: newFakeFileSystem(),
		statusError:    errors.New("not a git worktree"),
		ignoreError:    errors.New("not a git worktree"),
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
	if got := tree.IsIgnored(filepath.Join(root, "file")); got {
		t.Fatal("non-Git directory reported a file as ignored")
	}
	if tree.GitReady() {
		t.Fatal("non-Git directory reported as a repository")
	}
}

type gitIgnoreFileSystem struct {
	*fakeFileSystem
	statuses        []filesystem.GitStatusEntry
	statusError     error
	ignoreMatches   map[string][]string
	ignoreError     error
	lastIgnoreInput []string
}

func (f *gitIgnoreFileSystem) ReadGitStatus(string) ([]filesystem.GitStatusEntry, error) {
	if f.statusError != nil {
		return nil, f.statusError
	}
	return append([]filesystem.GitStatusEntry(nil), f.statuses...), nil
}

func (f *gitIgnoreFileSystem) ReadGitIgnore(path string, candidates []string) ([]string, error) {
	f.lastIgnoreInput = append([]string(nil), candidates...)
	if f.ignoreError != nil {
		return nil, f.ignoreError
	}
	matches := f.ignoreMatches[filepath.Clean(path)]
	if len(matches) == 0 {
		return nil, nil
	}
	var ignored []string
	for _, candidate := range candidates {
		for _, match := range matches {
			if candidate == match {
				ignored = append(ignored, candidate)
			}
		}
	}
	return ignored, nil
}
