package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestGitInfoLineRendersBranchAndLinkedWorktreeBelowRoot(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", RepoName: "agent-harness", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	startY := model.treeStartY()
	if model.stickyRowCount() != stickyRootHeight+1 {
		t.Fatalf("stickyRowCount() = %d, want %d", model.stickyRowCount(), stickyRootHeight+1)
	}
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	infoLine := lines[startY+1]
	if !strings.Contains(infoLine, branchTreeIcon) || !strings.Contains(infoLine, "feature") {
		t.Fatalf("info line = %q, want branch icon and name", infoLine)
	}
	if !strings.Contains(infoLine, "   ") || !strings.Contains(infoLine, worktreeTreeIcon) || !strings.Contains(infoLine, "agent-harness") {
		t.Fatalf("info line = %q, want worktree icon and repo name", infoLine)
	}
	if strings.Contains(lines[startY], branchTreeIcon) {
		t.Fatalf("root row = %q, must not carry the info line", lines[startY])
	}
}

func TestGitInfoLineShowsShortSHAWhenDetached(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{ShortSHA: "4c4b3be", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	infoLine := lines[model.treeStartY()+1]
	if !strings.Contains(infoLine, "4c4b3be") {
		t.Fatalf("detached info line = %q, want short SHA", infoLine)
	}
	if strings.Contains(infoLine, "feature") {
		t.Fatalf("detached info line = %q, must not carry a branch name", infoLine)
	}
}

func TestGitInfoLineOmitsWorktreeForMainCheckoutAndKeepsBranch(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "main", RepoName: "agent-harness"}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	infoLine := lines[model.treeStartY()+1]
	if !strings.Contains(infoLine, branchTreeIcon) || !strings.Contains(infoLine, "main") {
		t.Fatalf("main-checkout info line = %q, want branch only", infoLine)
	}
	if strings.Contains(infoLine, worktreeTreeIcon) || strings.Contains(infoLine, "agent-harness") {
		t.Fatalf("main-checkout info line = %q, want no worktree column", infoLine)
	}
}

func TestGitInfoLineHiddenOutsideRepositoryAndWithoutCapability(t *testing.T) {
	root := t.TempDir()

	errFake := newWorktreeFileSystem()
	errFake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	errFake.worktreeErr = errors.New("not a git worktree")
	errModel := NewModel(root, "", errFake)
	completeInitialLoad(t, errModel)
	errModel.Update(teaWindowSize(80, 8))
	if got := errModel.stickyRowCount(); got != stickyRootHeight {
		t.Fatalf("non-Git stickyRowCount() = %d, want %d", got, stickyRootHeight)
	}
	lines := strings.Split(ansi.Strip(errModel.View().Content), "\n")
	if strings.Contains(lines[errModel.treeStartY()+1], branchTreeIcon) || strings.Contains(lines[errModel.treeStartY()+1], worktreeTreeIcon) {
		t.Fatalf("non-Git row below root = %q, want a scroll row without icons", lines[errModel.treeStartY()+1])
	}

	plainFake := newFakeFileSystem()
	plainFake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	plain := NewModel(root, "", plainFake)
	completeInitialLoad(t, plain)
	plain.Update(teaWindowSize(80, 8))
	if got := plain.stickyRowCount(); got != stickyRootHeight {
		t.Fatalf("capability-less stickyRowCount() = %d, want %d", got, stickyRootHeight)
	}
}

func TestReloadKeyRefreshesWorktreeInfoLine(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "main"}
	model := NewModel(root, "", fake)
	model.Update(loadResultFromInit(t, model.Init()))
	model.Update(teaWindowSize(80, 8))
	if fake.worktreeCalls != 1 {
		t.Fatalf("initial worktree calls = %d, want 1", fake.worktreeCalls)
	}

	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", RepoName: "agent-harness", IsLinked: true}
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	switch message := cmd().(type) {
	case browser.LoadResult:
		model.Update(message)
	case tea.BatchMsg:
		for _, reloadCmd := range message {
			result, ok := reloadCmd().(browser.LoadResult)
			if !ok {
				t.Fatalf("reload message = %T, want browser.LoadResult", reloadCmd())
			}
			model.Update(result)
		}
	default:
		t.Fatalf("reload message = %T, want browser.LoadResult or tea.BatchMsg", message)
	}
	if fake.worktreeCalls != 2 {
		t.Fatalf("worktree calls after reload = %d, want 2", fake.worktreeCalls)
	}

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	infoLine := lines[model.treeStartY()+1]
	if !strings.Contains(infoLine, "feature") || !strings.Contains(infoLine, "agent-harness") {
		t.Fatalf("info line after reload = %q, want refreshed branch and repo", infoLine)
	}
}

func TestGitInfoLineStaysStickyWhileDescendantsScroll(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	entries := make([]filesystem.Entry, 0, 12)
	for index := 0; index < 12; index++ {
		entries = append(entries, filesystem.Entry{Name: "file-" + string(rune('a'+index)), Mode: 0})
	}
	fake.set(root, entries)
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 7))

	startY := model.treeStartY()
	infoBefore := strings.Split(ansi.Strip(model.View().Content), "\n")[startY+1]
	model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelDown})
	if model.offset == 0 {
		t.Fatal("wheel over the tree did not scroll descendants")
	}
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	if lines[startY+1] != infoBefore {
		t.Fatalf("info line after scrolling = %q, want unchanged %q", lines[startY+1], infoBefore)
	}
	if !strings.Contains(lines[startY], model.tree.Root().Path()) {
		t.Fatalf("root row after scrolling = %q, want sticky root path", lines[startY])
	}
}

func TestMouseHitTestSkipsTheStickyInfoLine(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "a-file", Mode: 0},
		{Name: "b-file", Mode: 0},
	})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	startY := model.treeStartY()
	if index, ok := model.rowIndexAtY(startY); !ok || index != 0 {
		t.Fatalf("root hit-test = (%d, %v), want (0, true)", index, ok)
	}
	if _, ok := model.rowIndexAtY(startY + stickyRootHeight); ok {
		t.Fatal("info line hit-test = selectable, want no row")
	}
	if index, ok := model.rowIndexAtY(startY + stickyRootHeight + 1); !ok || index != 1 {
		t.Fatalf("first scroll row hit-test = (%d, %v), want (1, true)", index, ok)
	}

	if _, cmd := model.Update(tea.MouseClickMsg{X: 0, Y: startY + stickyRootHeight, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("info line click returned command %v, want nil", cmd)
	}
	if model.selected != 0 {
		t.Fatalf("info line click selected = %d, want selection unchanged", model.selected)
	}
}

func TestStickyInfoLineKeepsFirstChildVisibleAndSelectable(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "a-file", Mode: 0},
		{Name: "b-file", Mode: 0},
	})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	startY := model.treeStartY()
	sticky := model.stickyRowCount()
	if sticky != stickyRootHeight+1 {
		t.Fatalf("stickyRowCount() = %d, want %d", sticky, stickyRootHeight+1)
	}
	// The info line is not an element of visibleRows, so every child after
	// the root stays scrollable: two files below root, zero skipped.
	if got := model.scrollableRowCount(); got != 2 {
		t.Fatalf("scrollableRowCount() = %d, want 2", got)
	}

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	firstScrollRow := lines[startY+sticky]
	if !strings.Contains(firstScrollRow, "a-file") || strings.Contains(firstScrollRow, "b-file") {
		t.Fatalf("first scroll row = %q, want a-file without b-file", firstScrollRow)
	}

	if index, ok := model.rowIndexAtY(startY + sticky); !ok || index != 1 {
		t.Fatalf("first scroll row hit-test = (%d, %v), want (1, true)", index, ok)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.selected != 1 {
		t.Fatalf("down selected = %d, want 1 (a-file)", model.selected)
	}
	lines = strings.Split(ansi.Strip(model.View().Content), "\n")
	if !strings.Contains(lines[startY+sticky], "a-file") {
		t.Fatalf("selected row = %q, want a-file highlighted on the first scroll row", lines[startY+sticky])
	}
}

func TestReloadWithGitStatusFailureHidesTheInfoLine(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))
	if got := model.stickyRowCount(); got != stickyRootHeight+1 {
		t.Fatalf("stickyRowCount() before reload = %d, want %d", got, stickyRootHeight+1)
	}

	fake.statusErr = errors.New("not a git repository anymore")
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	switch message := cmd().(type) {
	case browser.LoadResult:
		model.Update(message)
	case tea.BatchMsg:
		for _, reloadCmd := range message {
			result, ok := reloadCmd().(browser.LoadResult)
			if !ok {
				t.Fatalf("reload message = %T, want browser.LoadResult", reloadCmd())
			}
			model.Update(result)
		}
	default:
		t.Fatalf("reload message = %T, want browser.LoadResult or tea.BatchMsg", message)
	}

	if got := model.stickyRowCount(); got != stickyRootHeight {
		t.Fatalf("stickyRowCount() after failed reload = %d, want %d", got, stickyRootHeight)
	}
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	rowBelowRoot := lines[model.treeStartY()+1]
	if strings.Contains(rowBelowRoot, branchTreeIcon) || strings.Contains(rowBelowRoot, worktreeTreeIcon) {
		t.Fatalf("row below root after failed reload = %q, want no info line", rowBelowRoot)
	}
}

func TestGitInfoLineTruncatesWithinWidth(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	entries := make([]filesystem.Entry, 0, 6)
	for index := 0; index < 6; index++ {
		entries = append(entries, filesystem.Entry{Name: "file-" + string(rune('a'+index)), Mode: 0})
	}
	fake.set(root, entries)
	fake.worktree = filesystem.WorktreeInfo{Branch: "very-long-feature-branch-name", RepoName: "a-very-long-repository-name", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	for _, width := range []int{1, 8, 24, 80} {
		model.Update(teaWindowSize(width, 10))
		for lineNumber, line := range strings.Split(ansi.Strip(model.View().Content), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d line %d cell width = %d, want <= %d: %q", width, lineNumber, got, width, line)
			}
		}
	}
}

type worktreeFileSystem struct {
	*statusFileSystem
	worktree      filesystem.WorktreeInfo
	worktreeErr   error
	worktreeCalls int
}

func newWorktreeFileSystem() *worktreeFileSystem {
	return &worktreeFileSystem{statusFileSystem: newStatusFileSystem()}
}

func (f *worktreeFileSystem) ReadWorktreeInfo(string) (filesystem.WorktreeInfo, error) {
	f.worktreeCalls++
	if f.worktreeErr != nil {
		return filesystem.WorktreeInfo{}, f.worktreeErr
	}
	return f.worktree, nil
}
