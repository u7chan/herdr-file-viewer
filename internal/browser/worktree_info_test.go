package browser

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestTreeLoadsWorktreeInfoWithInitialRootRead(t *testing.T) {
	root := t.TempDir()
	fake := &worktreeFileSystem{gitStatusFileSystem: newGitStatusFileSystem()}
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", RepoName: "agent-harness", IsLinked: true}

	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	result := tree.ReadInitial(request)
	if fake.worktreeCalls != 1 {
		t.Fatalf("ReadInitial() worktree calls = %d, want 1", fake.worktreeCalls)
	}
	if _, ok := tree.WorktreeInfo(); ok {
		t.Fatal("WorktreeInfo() visible before ApplyLoad")
	}
	if !tree.ApplyLoad(result) {
		t.Fatal("ApplyLoad() rejected initial result")
	}

	info, ok := tree.WorktreeInfo()
	if !ok {
		t.Fatal("WorktreeInfo() not visible after ApplyLoad")
	}
	if info.Branch != "feature" || !info.IsLinked || info.RepoName != "agent-harness" {
		t.Fatalf("WorktreeInfo() = %#v, want linked feature/agent-harness", info)
	}
}

func TestTreeReloadRefreshesWorktreeInfo(t *testing.T) {
	root := t.TempDir()
	fake := &worktreeFileSystem{gitStatusFileSystem: newGitStatusFileSystem()}
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "main"}

	tree := mustGitStatusTree(t, root, fake)
	rootRequest, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.ReadInitial(rootRequest)) {
		t.Fatal("ApplyLoad() rejected initial result")
	}
	if fake.worktreeCalls != 1 {
		t.Fatalf("initial worktree calls = %d, want 1", fake.worktreeCalls)
	}

	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", RepoName: "agent-harness", IsLinked: true}
	requests := tree.Reload()
	if len(requests) != 1 {
		t.Fatalf("Reload() requests = %d, want 1", len(requests))
	}
	for _, reloadRequest := range requests {
		if !tree.ApplyLoad(tree.ReadReload(reloadRequest)) {
			t.Fatal("ApplyLoad() rejected reload result")
		}
	}

	if fake.worktreeCalls != 2 {
		t.Fatalf("worktree calls after reload = %d, want 2", fake.worktreeCalls)
	}
	info, ok := tree.WorktreeInfo()
	if !ok || info.Branch != "feature" || !info.IsLinked {
		t.Fatalf("WorktreeInfo() after reload = %#v (ok %v), want refreshed linked feature", info, ok)
	}
}

func TestTreeWorktreeInfoErrorHidesTheInfoLine(t *testing.T) {
	root := t.TempDir()
	fake := &worktreeFileSystem{gitStatusFileSystem: newGitStatusFileSystem()}
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktreeErr = errors.New("not a git worktree")

	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.ReadInitial(request)) {
		t.Fatal("ApplyLoad() rejected initial result")
	}
	if _, ok := tree.WorktreeInfo(); ok {
		t.Fatal("WorktreeInfo() visible with a failed snapshot")
	}
}

func TestTreeWithoutWorktreeCapabilityNeverShowsInfo(t *testing.T) {
	root := t.TempDir()
	fake := newGitStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})

	tree := mustGitStatusTree(t, root, fake)
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	result := tree.ReadInitial(request)
	if result.HasWorktreeInfo {
		t.Fatal("ReadInitial() marked worktree info present without the capability")
	}
	if !tree.ApplyLoad(result) {
		t.Fatal("ApplyLoad() rejected initial result")
	}
	if _, ok := tree.WorktreeInfo(); ok {
		t.Fatal("WorktreeInfo() visible without the capability")
	}
}

func TestLazyDirectoryReadsKeepTheRootWorktreeSnapshot(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := &worktreeFileSystem{gitStatusFileSystem: newGitStatusFileSystem()}
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "child", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}

	tree := mustGitStatusTree(t, root, fake)
	rootRequest, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.ReadInitial(rootRequest)) {
		t.Fatal("ApplyLoad() rejected initial result")
	}
	calls := fake.worktreeCalls

	directory := tree.Root().Children()[0]
	directoryRequest, ok := tree.Expand(directory)
	if !ok {
		t.Fatal("Expand(directory) started no load")
	}
	result := tree.Read(directoryRequest)
	if result.HasWorktreeInfo {
		t.Fatal("lazy read carried a worktree snapshot")
	}
	if !tree.ApplyLoad(result) {
		t.Fatal("ApplyLoad() rejected lazy result")
	}
	if fake.worktreeCalls != calls {
		t.Fatalf("lazy read worktree calls = %d, want unchanged %d", fake.worktreeCalls, calls)
	}
	info, ok := tree.WorktreeInfo()
	if !ok || info.Branch != "feature" {
		t.Fatalf("WorktreeInfo() after lazy read = %#v, want initial snapshot retained", info)
	}
}

type worktreeFileSystem struct {
	*gitStatusFileSystem
	worktree      filesystem.WorktreeInfo
	worktreeErr   error
	worktreeCalls int
}

func newGitStatusFileSystem() *gitStatusFileSystem {
	return &gitStatusFileSystem{fakeFileSystem: newFakeFileSystem()}
}

func (f *worktreeFileSystem) ReadWorktreeInfo(string) (filesystem.WorktreeInfo, error) {
	f.worktreeCalls++
	if f.worktreeErr != nil {
		return filesystem.WorktreeInfo{}, f.worktreeErr
	}
	return f.worktree, nil
}
