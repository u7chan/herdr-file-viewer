package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

// shiftC is the shift+c key event; String() reports "C".
var shiftC = tea.KeyPressMsg{Code: 'C', Text: "C"}

// backspaceKey is the Backspace key event; String() reports "backspace".
var backspaceKey = tea.KeyPressMsg{Code: tea.KeyBackspace}

// helpKey is the h key event.
var helpKey = tea.KeyPressMsg{Code: 'h', Text: "h"}

// chdirRecorder records every cwd change a root move requests.
type chdirRecorder struct {
	paths []string
	err   error
}

func (r *chdirRecorder) chdir(path string) error {
	r.paths = append(r.paths, path)
	return r.err
}

func TestCDownMovesRootSyncsCwdAndSelectsTheStickyRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "child-dir")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "child-dir", Mode: fs.ModeDir}})
	fake.set(target, []filesystem.Entry{{Name: "file", Mode: 0}, {Name: "second", Mode: 0}})
	chdir := &chdirRecorder{}
	model := NewModelConfigured(root, "", ModelConfig{Chdir: chdir.chdir}, fake)
	completeInitialLoad(t, model)

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := model.UpdateKey(shiftC)
	if cmd == nil {
		t.Fatal("C on a directory returned nil command")
	}
	if got := chdir.paths; len(got) != 1 || got[0] != target {
		t.Fatalf("chdir calls = %v, want the selected directory %q", got, target)
	}
	if model.tree.Root().Path() != target {
		t.Fatalf("tree root = %q, want %q", model.tree.Root().Path(), target)
	}
	if model.selected != 0 || len(model.visibleRows) != 1 {
		t.Fatalf("post-move selection = %d with %d rows, want sticky root row 0 of 1", model.selected, len(model.visibleRows))
	}
	if !model.loading || model.status != loadingStatus {
		t.Fatalf("post-move load state = loading %v, status %q; want pending fresh root load", model.loading, model.status)
	}

	model.Update(loadResultFromCommand(t, cmd))
	if model.loading || model.tree.Root().Path() != target {
		t.Fatalf("loaded state = loading %v, root %q; want idle %q", model.loading, model.tree.Root().Path(), target)
	}
	children := model.tree.Root().Children()
	if len(children) != 2 || children[0].Name() != "file" || children[1].Name() != "second" {
		t.Fatalf("new root children = %v, want the target's own entries", children)
	}
	if model.selected != 0 {
		t.Fatalf("selection after fresh root load = %d, want sticky root 0", model.selected)
	}
}

func TestCDownLoadsTheNewRootAsynchronouslyWithoutReusingExpansionCache(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "target", Mode: fs.ModeDir}})
	fake.set(target, []filesystem.Entry{{Name: "nested", Mode: fs.ModeDir}})
	fake.set(filepath.Join(target, "nested"), []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModelConfigured(root, "", ModelConfig{}, fake)
	completeInitialLoad(t, model)

	// Expand the future root in the old tree so the old expansion cache
	// would be visible if it were reused.
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(loadResultFromCommand(t, model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})))
	if len(model.visibleRows) != 3 {
		t.Fatalf("pre-move rows = %d, want 3 expanded rows", len(model.visibleRows))
	}
	calls := len(fake.calls())

	cmd := model.UpdateKey(shiftC)
	if cmd == nil {
		t.Fatal("C returned nil command")
	}
	if got := fake.calls(); len(got) != calls {
		t.Fatalf("C performed filesystem reads = %v, want command-deferred read only", got)
	}
	if len(model.visibleRows) != 1 || model.selected != 0 {
		t.Fatalf("pre-load rows = %d, selected %d; want only the sticky root", len(model.visibleRows), model.selected)
	}

	model.Update(loadResultFromCommand(t, cmd))
	if len(model.visibleRows) != 2 || model.tree.Root().Children()[0].Name() != "nested" {
		t.Fatalf("new tree rows = %d with children %v, want root plus the target's unexpanded entries", len(model.visibleRows), model.tree.Root().Children())
	}
	if model.tree.Root().Children()[0].Expanded() {
		t.Fatal("new tree reused the old expansion cache")
	}
}

func TestCDownIsNoOpOnFileSymlinkAndStickyRoot(t *testing.T) {
	root := t.TempDir()
	chdir := &chdirRecorder{}
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "file", Mode: 0},
		{Name: "link", Mode: fs.ModeSymlink},
		{Name: "directory", Mode: fs.ModeDir},
	})
	fake.set(filepath.Join(root, "directory"), nil)
	model := NewModelConfigured(root, "", ModelConfig{Chdir: chdir.chdir}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	for iteration, key := range []struct {
		name string
		row  int
	}{
		{name: "sticky root", row: 0},
		{name: "file", row: 2},
		{name: "symlink", row: 3},
	} {
		model.selected = key.row
		if cmd := model.UpdateKey(shiftC); cmd != nil {
			t.Fatalf("C on %s returned command %v, want no-op", key.name, cmd)
		}
		if model.tree.Root().Path() != root {
			t.Fatalf("C on %s changed the root to %q", key.name, model.tree.Root().Path())
		}
		if len(chdir.paths) != 0 {
			t.Fatalf("C on %s changed the cwd to %v", key.name, chdir.paths)
		}
		if model.loading || model.status != readyStatus {
			t.Fatalf("C on %s moved the model into loading=%v status=%q", key.name, model.loading, model.status)
		}
		_ = iteration
	}
}

func TestBackspaceMovesToParentAndReSelectsThePreviousRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "child", Mode: fs.ModeDir}})
	fake.set(child, []filesystem.Entry{{Name: "file", Mode: 0}})
	chdir := &chdirRecorder{}
	model := NewModelConfigured(child, "", ModelConfig{Chdir: chdir.chdir}, fake)
	completeInitialLoad(t, model)

	cmd := model.UpdateKey(backspaceKey)
	if cmd == nil {
		t.Fatal("Backspace on the sticky root returned nil command")
	}
	if got := chdir.paths; len(got) != 1 || got[0] != root {
		t.Fatalf("chdir calls = %v, want the parent %q", got, root)
	}
	if model.tree.Root().Path() != root || model.selected != 0 {
		t.Fatalf("post-move root = %q, selection %d; want parent with sticky root selected", model.tree.Root().Path(), model.selected)
	}

	model.Update(loadResultFromCommand(t, cmd))
	if model.tree.Root().Path() != root || model.loading {
		t.Fatalf("loaded state = root %q, loading %v; want idle parent", model.tree.Root().Path(), model.loading)
	}
	if model.selected == 0 || model.selectedNode().Name() != "child" {
		t.Fatalf("selection after parent load = row %d (%q), want the previous root child", model.selected, model.selectedNode().Name())
	}
}

func TestBackspaceFallsBackToStickyRootWhenThePreviousRootDisappeared(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{})
	fake.set(child, []filesystem.Entry{{Name: "file", Mode: 0}})
	chdir := &chdirRecorder{}
	model := NewModelConfigured(child, "", ModelConfig{Chdir: chdir.chdir}, fake)
	completeInitialLoad(t, model)

	model.Update(loadResultFromCommand(t, model.UpdateKey(backspaceKey)))
	if model.tree.Root().Path() != root {
		t.Fatalf("root = %q, want the parent %q", model.tree.Root().Path(), root)
	}
	if model.selected != 0 {
		t.Fatalf("selection = %d, want sticky root fallback when the previous root is no longer listed", model.selected)
	}
}

func TestBackspaceOnFailedParentReadKeepsStickyRootAndRecovers(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	grandparent := filepath.Dir(root)
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "child", Mode: fs.ModeDir}})
	fake.setError(root, errors.New("read exploded\x1b\n"))
	fake.set(child, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModelConfigured(child, "", ModelConfig{}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	model.Update(loadResultFromCommand(t, model.UpdateKey(backspaceKey)))
	if model.loading || model.tree.Root().Path() != root || model.tree.Root().Loaded() {
		t.Fatalf("failed parent state = loading %v, root %q, loaded %v; want idle errored root", model.loading, model.tree.Root().Path(), model.tree.Root().Loaded())
	}
	if model.selected != 0 {
		t.Fatalf("failed parent selection = %d, want sticky root", model.selected)
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "Error:") {
		t.Fatalf("failed parent view = %q, want error status", ansi.Strip(model.View().Content))
	}

	// Backspace from the errored sticky root moves on to the next parent.
	cmd := model.UpdateKey(backspaceKey)
	if cmd == nil {
		t.Fatal("Backspace on the errored root returned nil command, want recovery move")
	}
	if model.tree.Root().Path() != grandparent {
		t.Fatalf("recovery root = %q, want %q", model.tree.Root().Path(), grandparent)
	}
}

func TestBackspaceIsNoOpOffTheStickyRootAndAtFilesystemRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "child", Mode: fs.ModeDir}})
	fake.set(child, []filesystem.Entry{{Name: "file", Mode: 0}})
	chdir := &chdirRecorder{}
	model := NewModelConfigured(child, "", ModelConfig{Chdir: chdir.chdir}, fake)
	completeInitialLoad(t, model)

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := model.UpdateKey(backspaceKey); cmd != nil {
		t.Fatalf("Backspace off the sticky root returned command %v, want no-op", cmd)
	}
	if model.tree.Root().Path() != child || len(chdir.paths) != 0 {
		t.Fatalf("off-root Backspace changed root to %q and cwd to %v", model.tree.Root().Path(), chdir.paths)
	}

	filesystemRoot := "/"
	rootFake := newFakeFileSystem()
	rootFake.set(filesystemRoot, nil)
	rootModel := NewModelConfigured(filesystemRoot, "", ModelConfig{Chdir: chdir.chdir}, rootFake)
	completeInitialLoad(t, rootModel)
	if cmd := rootModel.UpdateKey(backspaceKey); cmd != nil {
		t.Fatalf("Backspace at / returned command %v, want no-op", cmd)
	}
	if rootModel.tree.Root().Path() != "/" || len(chdir.paths) != 0 {
		t.Fatalf("Backspace at / changed root to %q and cwd to %v", rootModel.tree.Root().Path(), chdir.paths)
	}
}

func TestChdirFailureKeepsRootTreeExpansionAndSelectionAndWarns(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "child", Mode: fs.ModeDir}})
	fake.set(child, []filesystem.Entry{{Name: "nested", Mode: fs.ModeDir}})
	fake.set(filepath.Join(child, "nested"), nil)
	model := NewModelConfigured(root, "", ModelConfig{Chdir: func(string) error {
		return errors.New("permission denied\x1b\n")
	}}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	// Build expansion + selection state that must survive the failed move.
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(loadResultFromCommand(t, model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})))
	selectedBefore := model.selected
	expandedChild := model.tree.Root().Children()[0]
	calls := len(fake.calls())

	if cmd := model.UpdateKey(shiftC); cmd != nil {
		t.Fatalf("C with failing chdir returned command %v, want no-op", cmd)
	}
	if model.tree.Root().Path() != root {
		t.Fatalf("failed chdir changed the root to %q", model.tree.Root().Path())
	}
	if model.selected != selectedBefore || !expandedChild.Expanded() {
		t.Fatalf("failed chdir changed selection %d/%d or expansion %v", model.selected, selectedBefore, expandedChild.Expanded())
	}
	if model.loading || len(fake.calls()) != calls {
		t.Fatalf("failed chdir started work: loading %v, filesystem calls %v", model.loading, fake.calls())
	}
	footer := ansi.Strip(strings.Split(model.View().Content, "\n")[len(strings.Split(model.View().Content, "\n"))-1])
	if !strings.Contains(footer, "Warning: cd: permission denied�") {
		t.Fatalf("failed chdir footer = %q, want sanitized warning", footer)
	}
	if strings.ContainsAny(model.warning, "\x00\x1b\n\r\t") {
		t.Fatalf("warning contains terminal controls: %q", model.warning)
	}
}

func TestViewAndKeyHandlingPerformNoFilesystemOrCLIWork(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "target", Mode: fs.ModeDir}})
	fake.set(target, []filesystem.Entry{{Name: "file", Mode: 0}})
	chdir := &chdirRecorder{}
	helps := &stubHelpClient{paneID: "wY:h1"}
	model := NewModelConfigured(root, "", ModelConfig{
		Help: HelpConfig{Client: helps, TargetPane: "wY:p3K"},
		Chdir: chdir.chdir,
	}, fake)
	completeInitialLoad(t, model)
	calls := len(fake.calls())

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	move := model.UpdateKey(shiftC)
	if move == nil {
		t.Fatal("C returned nil command")
	}
	model.UpdateKey(helpKey)
	_ = model.View()
	model.Update(loadResultFromCommand(t, move))
	_ = model.View()

	if len(fake.calls()) != calls+1 {
		t.Fatalf("filesystem calls = %v, want only the command-deferred root read", fake.calls())
	}
	if len(chdir.paths) != 1 {
		t.Fatalf("chdir calls = %v, want the move's single sync", chdir.paths)
	}
	if len(helps.requests) != 0 {
		t.Fatalf("help launches = %d, want the h command not yet executed", len(helps.requests))
	}
}

func TestStaleLoadAndReloadResultsCannotTouchTheNewRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "target", Mode: fs.ModeDir}})
	fake.set(target, []filesystem.Entry{{Name: "nested", Mode: fs.ModeDir}})
	fake.set(filepath.Join(target, "nested"), []filesystem.Entry{{Name: "file", Mode: 0}})
	chdir := &chdirRecorder{}
	model := NewModelConfigured(root, "", ModelConfig{Chdir: chdir.chdir}, fake)
	completeInitialLoad(t, model)

	// Issue a child expansion and a reload in the old generation. The
	// expansion reads the same path that the new root will load, so a
	// path-keyed bookkeeping would collide if stale results were applied.
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	oldExpand := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})
	oldReload := model.reloadTree(false)
	if oldExpand == nil || oldReload == nil {
		t.Fatal("old-generation load and reload commands are nil")
	}

	// Move the root to the directory whose load is still in flight.
	cmd := model.UpdateKey(shiftC)
	if cmd == nil {
		t.Fatal("C returned nil command")
	}
	newLoad := loadResultFromCommand(t, cmd)

	// Deliver every old-generation result late.
	model.Update(loadResultFromCommand(t, oldExpand))
	switch message := oldReload().(type) {
	case tea.BatchMsg:
		for _, reloadCmd := range message {
			model.Update(loadResultFromCommand(t, reloadCmd))
		}
	default:
		model.Update(loadResultFromCommand(t, oldReload))
	}
	if !model.loading || model.status != loadingStatus || len(model.pending) != 1 {
		t.Fatalf("stale results changed new state: loading %v, status %q, pending %d; want pending root load", model.loading, model.status, len(model.pending))
	}
	if model.selected != 0 || len(model.visibleRows) != 1 {
		t.Fatalf("stale results changed selection: selected %d, rows %d; want sticky root only", model.selected, len(model.visibleRows))
	}
	if model.tree.Root().Path() != target || model.tree.Root().Loaded() {
		t.Fatalf("stale results changed the tree: root %q loaded %v", model.tree.Root().Path(), model.tree.Root().Loaded())
	}

	model.Update(newLoad)
	if model.loading || model.tree.Root().Path() != target {
		t.Fatalf("new root load did not settle: loading %v, root %q", model.loading, model.tree.Root().Path())
	}
	if model.toast != "" {
		t.Fatalf("stale reload results produced toast %q", model.toast)
	}
}

func TestRootMoveRefreshesGitStatusIgnoreAndWorktreeForTheNewRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "target", Mode: fs.ModeDir}})
	fake.set(target, []filesystem.Entry{{Name: "inner", Mode: 0}})
	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "target", Status: filesystem.GitStatusModified},
		{Path: "inner", Status: filesystem.GitStatusUntracked},
	}
	fake.ignoreMatches = []string{"inner"}
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature"}
	chdir := &chdirRecorder{}
	model := NewModelConfigured(root, "", ModelConfig{Chdir: chdir.chdir}, fake)
	completeInitialLoad(t, model)
	if fake.statusCalls != 1 || fake.worktreeCalls != 1 {
		t.Fatalf("initial snapshots = status %d, worktree %d; want one each", fake.statusCalls, fake.worktreeCalls)
	}
	if got := model.tree.GitStatusForPath(filepath.Join(root, "target")); got != browser.GitStatusModified {
		t.Fatalf("pre-move status = %v, want modified", got)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(loadResultFromCommand(t, model.UpdateKey(shiftC)))
	if fake.statusCalls != 2 || fake.worktreeCalls != 2 {
		t.Fatalf("post-move snapshots = status %d, worktree %d; want one fresh snapshot each", fake.statusCalls, fake.worktreeCalls)
	}
	if got := model.tree.GitStatusForPath(filepath.Join(target, "inner")); got != browser.GitStatusUntracked {
		t.Fatalf("post-move status = %v, want untracked relative to the new root", got)
	}
	if got := model.tree.GitStatusForPath(root); got != browser.GitStatusNone {
		t.Fatalf("post-move status of the old root's parent = %v, want none outside the new root", got)
	}
	if !model.tree.IsIgnored(filepath.Join(target, "inner")) {
		t.Fatal("post-move ignore check misses the new root's .gitignore snapshot")
	}
	if info, ok := model.tree.WorktreeInfo(); !ok || info.Branch != "feature" {
		t.Fatalf("post-move worktree info = %#v (ok %v), want feature branch snapshot", info, ok)
	}
}

func TestRootMoveKeepsPreviewAndExistingNavigationBehavior(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	filePath := filepath.Join(target, "file")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "target", Mode: fs.ModeDir}})
	fake.set(target, []filesystem.Entry{{Name: "file", Mode: 0}})
	chdir := &chdirRecorder{}
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelConfigured(root, "", ModelConfig{
		Preview: PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"},
		Chdir:   chdir.chdir,
	}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})

	// Open a preview of the future root's file entry before the move.
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(loadResultFromCommand(t, model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	preview := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	model.Update(preview())
	if model.previewPaneID != "wY:p9Z" || len(client.openFiles) != 1 || client.openFiles[0] != filePath {
		t.Fatalf("pre-move preview = pane %q files %v, want tracked pane for %q", model.previewPaneID, client.openFiles, filePath)
	}

	// The root move keeps the tracked preview pane untouched.
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyUp})
	model.Update(loadResultFromCommand(t, model.UpdateKey(shiftC)))
	if model.previewPaneID != "wY:p9Z" {
		t.Fatalf("root move lost the tracked preview pane %q", model.previewPaneID)
	}

	// Enter and space keep working on the new tree.
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("Enter on the new root's file returned nil command")
	}
	model.Update(preview())
	if got := clipboardContent(t, model.UpdateKey(tea.KeyPressMsg{Code: tea.KeySpace})); got != filePath {
		t.Fatalf("space on the new root copied %q, want %q", got, filePath)
	}
	// Right on the loaded sticky root stays a no-op like before the move;
	// Left cannot collapse the root row.
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); cmd != nil {
		t.Fatalf("Right on the new sticky root returned command %v, want no-op", cmd)
	}
	if !model.tree.Root().Expanded() {
		t.Fatal("Right on the new sticky root collapsed the root")
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if model.selected != 0 || !model.tree.Root().Expanded() {
		t.Fatalf("Left on the new sticky root selected %d with expanded %v, want sticky root kept open", model.selected, model.tree.Root().Expanded())
	}
}