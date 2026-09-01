package app

import (
	"errors"
	"image/color"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

// gitInfoRowY returns the screen row of the dedicated Git info row: directly
// above the footer, below the bottom divider.
func gitInfoRowY(model *Model) int {
	_, treeHeight, _ := layoutHeights(model.height)
	return model.treeStartY() + treeHeight + footerDividerHeight(model.height)
}

func TestGitInfoRowRendersBranchAndLinkedWorktreeAboveFooter(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", RepoName: "agent-harness", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	startY := model.treeStartY()
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	infoRow := lines[gitInfoRowY(model)]
	if !strings.Contains(infoRow, branchTreeIcon) || !strings.Contains(infoRow, "feature") {
		t.Fatalf("git info row = %q, want branch icon and name", infoRow)
	}
	if !strings.Contains(infoRow, "   ") || !strings.Contains(infoRow, worktreeTreeIcon) || !strings.Contains(infoRow, "agent-harness") {
		t.Fatalf("git info row = %q, want worktree icon and repo name", infoRow)
	}
	// The row sits directly above the footer, below the bottom divider, and
	// outside the tree region.
	if !strings.Contains(lines[gitInfoRowY(model)-1], dividerGlyph) {
		t.Fatalf("row above the git info row = %q, want the bottom divider", lines[gitInfoRowY(model)-1])
	}
	if !strings.Contains(lines[gitInfoRowY(model)+1], helpCopyKey) {
		t.Fatalf("row below the git info row = %q, want the footer help line", lines[gitInfoRowY(model)+1])
	}
	if strings.Contains(lines[startY], branchTreeIcon) {
		t.Fatalf("root row = %q, must not carry the git info row", lines[startY])
	}
}

func TestGitInfoTextForegroundRoles(t *testing.T) {
	tests := []struct {
		name  string
		style lipgloss.Style
		want  color.Color
	}{
		{name: "branch dark", style: gitInfoBranchStyle(false), want: lipgloss.Color(gitInfoBranchForeground)},
		{name: "branch light", style: gitInfoBranchStyle(true), want: lipgloss.Color(gitInfoBranchForegroundLight)},
		{name: "worktree dark", style: gitInfoWorktreeStyle(false), want: lipgloss.Color(gitInfoWorktreeForeground)},
		{name: "worktree light", style: gitInfoWorktreeStyle(true), want: lipgloss.Color(gitInfoWorktreeForegroundLight)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.style.GetForeground(); got != test.want {
				t.Fatalf("%s foreground = %v, want %v", test.name, got, test.want)
			}
		})
	}
	// The worktree name must never read stronger than the branch name.
	// On a dark palette a lower ANSI gray index is weaker (darker); on a
	// light palette a higher index is weaker (lighter).
	branchDark := ansiIndex(t, gitInfoBranchForeground)
	worktreeDark := ansiIndex(t, gitInfoWorktreeForeground)
	if worktreeDark >= branchDark {
		t.Fatalf("dark worktree gray %d must be weaker than branch gray %d", worktreeDark, branchDark)
	}
	branchLight := ansiIndex(t, gitInfoBranchForegroundLight)
	worktreeLight := ansiIndex(t, gitInfoWorktreeForegroundLight)
	if worktreeLight <= branchLight {
		t.Fatalf("light worktree gray %d must be weaker than branch gray %d", worktreeLight, branchLight)
	}
}

func TestGitInfoLineAppliesMutedGrayRolesAndKeepsIconPalette(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", RepoName: "agent-harness", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	line := model.renderGitInfoLine(80)
	for _, segment := range []string{
		iconStyle(branchTreeIcon).Render(branchTreeIcon),
		gitInfoBranchStyle(false).Render("feature"),
		iconStyle(worktreeTreeIcon).Render(worktreeTreeIcon),
		gitInfoWorktreeStyle(false).Render("agent-harness"),
	} {
		if !strings.Contains(line, segment) {
			t.Fatalf("dark git info line = %q, want segment %q", line, segment)
		}
	}

	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	line = model.renderGitInfoLine(80)
	for _, segment := range []string{
		gitInfoBranchStyle(true).Render("feature"),
		gitInfoWorktreeStyle(true).Render("agent-harness"),
	} {
		if !strings.Contains(line, segment) {
			t.Fatalf("light git info line = %q, want segment %q", line, segment)
		}
	}
}

// ansiIndex parses an ANSI 256 color code for relative-strength
// comparisons between the branch and worktree grays.
func ansiIndex(t *testing.T, code string) int {
	t.Helper()
	index, err := strconv.Atoi(code)
	if err != nil {
		t.Fatalf("ANSI color %q is not numeric: %v", code, err)
	}
	return index
}

func TestGitInfoRowShowsShortSHAWhenDetached(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{ShortSHA: "4c4b3be", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	infoRow := strings.Split(ansi.Strip(model.View().Content), "\n")[gitInfoRowY(model)]
	if !strings.Contains(infoRow, "4c4b3be") {
		t.Fatalf("detached git info row = %q, want short SHA", infoRow)
	}
	if strings.Contains(infoRow, "feature") {
		t.Fatalf("detached git info row = %q, must not carry a branch name", infoRow)
	}
}

func TestGitInfoRowOmitsWorktreeForMainCheckoutAndKeepsBranch(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "main", RepoName: "agent-harness"}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))

	infoRow := strings.Split(ansi.Strip(model.View().Content), "\n")[gitInfoRowY(model)]
	if !strings.Contains(infoRow, branchTreeIcon) || !strings.Contains(infoRow, "main") {
		t.Fatalf("main-checkout git info row = %q, want branch only", infoRow)
	}
	if strings.Contains(infoRow, worktreeTreeIcon) || strings.Contains(infoRow, "agent-harness") {
		t.Fatalf("main-checkout git info row = %q, want no worktree column", infoRow)
	}
}

func TestGitInfoRowBlankOutsideRepositoryWithoutCapabilityAndWhileLoading(t *testing.T) {
	root := t.TempDir()

	errFake := newWorktreeFileSystem()
	errFake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	errFake.worktreeErr = errors.New("not a git worktree")
	errModel := NewModel(root, "", errFake)
	completeInitialLoad(t, errModel)
	errModel.Update(teaWindowSize(80, 8))

	plainFake := newFakeFileSystem()
	plainFake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	plain := NewModel(root, "", plainFake)
	completeInitialLoad(t, plain)
	plain.Update(teaWindowSize(80, 8))

	loading := NewModel(root, "", newWorktreeFileSystem())
	loading.Update(teaWindowSize(80, 8))

	for name, model := range map[string]*Model{
		"failed snapshot": errModel,
		"no capability":   plain,
		"still loading":   loading,
	} {
		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		infoRow := lines[gitInfoRowY(model)]
		if strings.Contains(infoRow, branchTreeIcon) || strings.Contains(infoRow, worktreeTreeIcon) {
			t.Fatalf("%s git info row = %q, want no icons", name, infoRow)
		}
		if got := strings.TrimSpace(infoRow); got != "" {
			t.Fatalf("%s git info row = %q, want a blank reserved row", name, infoRow)
		}
	}
}

func TestReloadKeyRefreshesGitInfoRow(t *testing.T) {
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
	rowBefore := gitInfoRowY(model)
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
	if rowBefore != gitInfoRowY(model) {
		t.Fatalf("git info row moved from %d to %d across reload", rowBefore, gitInfoRowY(model))
	}

	infoRow := strings.Split(ansi.Strip(model.View().Content), "\n")[gitInfoRowY(model)]
	if !strings.Contains(infoRow, "feature") || !strings.Contains(infoRow, "agent-harness") {
		t.Fatalf("git info row after reload = %q, want refreshed branch and repo", infoRow)
	}
}

func TestGitInfoRowStaysFixedWhileDescendantsScroll(t *testing.T) {
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
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	infoBefore := lines[gitInfoRowY(model)]
	footerBefore := lines[len(lines)-1]
	rootBefore := lines[startY]

	model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelDown})
	if model.offset == 0 {
		t.Fatal("wheel over the tree did not scroll descendants")
	}
	lines = strings.Split(ansi.Strip(model.View().Content), "\n")
	if lines[gitInfoRowY(model)] != infoBefore {
		t.Fatalf("git info row after scrolling = %q, want unchanged %q", lines[gitInfoRowY(model)], infoBefore)
	}
	if lines[len(lines)-1] != footerBefore {
		t.Fatalf("footer after scrolling = %q, want unchanged %q", lines[len(lines)-1], footerBefore)
	}
	if lines[startY] != rootBefore {
		t.Fatalf("root row after scrolling = %q, want unchanged %q", lines[startY], rootBefore)
	}
}

func TestMouseHitTestSkipsTheGitInfoRow(t *testing.T) {
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
	if index, ok := model.rowIndexAtY(startY + stickyRootHeight); !ok || index != 1 {
		t.Fatalf("first scroll row hit-test = (%d, %v), want (1, true)", index, ok)
	}
	if _, ok := model.rowIndexAtY(gitInfoRowY(model)); ok {
		t.Fatal("git info row hit-test = selectable, want no row")
	}
	if _, ok := model.rowIndexAtY(gitInfoRowY(model) + 1); ok {
		t.Fatal("footer hit-test = selectable, want no row")
	}

	if _, cmd := model.Update(tea.MouseClickMsg{X: 0, Y: gitInfoRowY(model), Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("git info row click returned command %v, want nil", cmd)
	}
	if model.selected != 0 {
		t.Fatalf("git info row click selected = %d, want selection unchanged", model.selected)
	}
}

func TestGitInfoRowDoesNotConsumeScrollableRows(t *testing.T) {
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
	// The git info row is not an element of visibleRows, so every child
	// after the root stays scrollable and the row mapping matches the
	// non-Git layout exactly.
	if got := model.scrollableRowCount(); got != 2 {
		t.Fatalf("scrollableRowCount() = %d, want 2", got)
	}

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	firstScrollRow := lines[startY+stickyRootHeight]
	if !strings.Contains(firstScrollRow, "a-file") || strings.Contains(firstScrollRow, "b-file") {
		t.Fatalf("first scroll row = %q, want a-file without b-file", firstScrollRow)
	}

	if index, ok := model.rowIndexAtY(startY + stickyRootHeight); !ok || index != 1 {
		t.Fatalf("first scroll row hit-test = (%d, %v), want (1, true)", index, ok)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.selected != 1 {
		t.Fatalf("down selected = %d, want 1 (a-file)", model.selected)
	}
	lines = strings.Split(ansi.Strip(model.View().Content), "\n")
	if !strings.Contains(lines[startY+stickyRootHeight], "a-file") {
		t.Fatalf("selected row = %q, want a-file highlighted on the first scroll row", lines[startY+stickyRootHeight])
	}
}

func TestReloadWithGitStatusFailureBlanksTheGitInfoRow(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 8))
	rowBefore := gitInfoRowY(model)
	rootBefore := strings.Split(ansi.Strip(model.View().Content), "\n")[model.treeStartY()]
	rowsBefore := len(model.visibleRows)

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

	if rowBefore != gitInfoRowY(model) {
		t.Fatalf("git info row moved from %d to %d across failed reload", rowBefore, gitInfoRowY(model))
	}
	if len(model.visibleRows) != rowsBefore {
		t.Fatalf("visible rows after failed reload = %d, want %d", len(model.visibleRows), rowsBefore)
	}
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	infoRow := lines[gitInfoRowY(model)]
	if strings.Contains(infoRow, branchTreeIcon) || strings.Contains(infoRow, worktreeTreeIcon) {
		t.Fatalf("git info row after failed reload = %q, want no icons", infoRow)
	}
	if got := strings.TrimSpace(infoRow); got != "" {
		t.Fatalf("git info row after failed reload = %q, want a blank reserved row", infoRow)
	}
	if lines[model.treeStartY()] != rootBefore {
		t.Fatalf("root row after failed reload = %q, want unchanged %q", lines[model.treeStartY()], rootBefore)
	}
}

func TestGitInfoRowPreservesTreeCoordinatesAcrossRepositoryTransitions(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "a-file", Mode: 0},
		{Name: "b-file", Mode: 0},
		{Name: "c-file", Mode: 0},
	})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(80, 10))

	geometry := func() (lineCount, rowCount, viewport int, hits []bool) {
		view := model.View()
		lineCount = len(strings.Split(ansi.Strip(view.Content), "\n"))
		rowCount = len(model.visibleRows)
		viewport = model.scrollableViewportHeight()
		_, treeHeight, _ := layoutHeights(model.height)
		startY := model.treeStartY()
		hits = make([]bool, treeHeight)
		for offset := 0; offset < treeHeight; offset++ {
			_, hits[offset] = model.rowIndexAtY(startY + offset)
		}
		return lineCount, rowCount, viewport, hits
	}
	linesBefore, rowsBefore, viewportBefore, hitsBefore := geometry()
	if rowsBefore != 4 || viewportBefore != 4 {
		t.Fatalf("initial geometry = rows %d, viewport %d; want 4, 4", rowsBefore, viewportBefore)
	}

	// Git -> non-Git: the reserved row stays, the content blanks, and every
	// tree coordinate is unchanged.
	fake.statusErr = errors.New("not a git repository anymore")
	fake.worktreeErr = errors.New("not a git repository anymore")
	applyReload(t, model, model.UpdateKey(tea.KeyPressMsg{Code: 'r'}))
	linesAfter, rowsAfter, viewportAfter, hitsAfter := geometry()
	if linesAfter != linesBefore || rowsAfter != rowsBefore || viewportAfter != viewportBefore {
		t.Fatalf("non-Git geometry changed: lines %d/%d, rows %d/%d, viewport %d/%d", linesAfter, linesBefore, rowsAfter, rowsBefore, viewportAfter, viewportBefore)
	}
	for index := range hitsBefore {
		if hitsAfter[index] != hitsBefore[index] {
			t.Fatalf("non-Git hit-test row %d = %v, want %v", index, hitsAfter[index], hitsBefore[index])
		}
	}
	if got := strings.TrimSpace(strings.Split(ansi.Strip(model.View().Content), "\n")[gitInfoRowY(model)]); got != "" {
		t.Fatalf("non-Git git info row = %q, want blank", got)
	}

	// Non-Git -> Git: the row fills in again and the geometry still matches.
	fake.statusErr = nil
	fake.worktreeErr = nil
	fake.worktree = filesystem.WorktreeInfo{Branch: "other", IsLinked: true}
	applyReload(t, model, model.UpdateKey(tea.KeyPressMsg{Code: 'r'}))
	linesAfter, rowsAfter, viewportAfter, hitsAfter = geometry()
	if linesAfter != linesBefore || rowsAfter != rowsBefore || viewportAfter != viewportBefore {
		t.Fatalf("re-git geometry changed: lines %d/%d, rows %d/%d, viewport %d/%d", linesAfter, linesBefore, rowsAfter, rowsBefore, viewportAfter, viewportBefore)
	}
	for index := range hitsBefore {
		if hitsAfter[index] != hitsBefore[index] {
			t.Fatalf("re-git hit-test row %d = %v, want %v", index, hitsAfter[index], hitsBefore[index])
		}
	}
	infoRow := strings.Split(ansi.Strip(model.View().Content), "\n")[gitInfoRowY(model)]
	if !strings.Contains(infoRow, "other") {
		t.Fatalf("re-git info row = %q, want refreshed branch", infoRow)
	}
}

func TestGitInfoRowBoundaryAtTinyHeights(t *testing.T) {
	root := t.TempDir()
	fake := newWorktreeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.worktree = filesystem.WorktreeInfo{Branch: "feature", IsLinked: true}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	for height, wantGit := range map[int]int{3: 0, 4: 0, 5: 0, 6: 1, 7: 1} {
		if got := gitInfoHeight(height); got != wantGit {
			t.Errorf("gitInfoHeight(%d) = %d, want %d", height, got, wantGit)
		}
	}
	if header, tree, footer := layoutHeights(5); header != 1 || tree != 1 || footer != 1 {
		t.Fatalf("layoutHeights(5) = (%d, %d, %d), want (1, 1, 1) without the git info row", header, tree, footer)
	}
	if header, tree, footer := layoutHeights(6); header != 1 || tree != 1 || footer != 1 {
		t.Fatalf("layoutHeights(6) = (%d, %d, %d), want (1, 1, 1) with the git info row", header, tree, footer)
	}

	for _, height := range []int{3, 4, 5, 6, 7} {
		model.Update(teaWindowSize(80, height))
		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		if len(lines) != height {
			t.Fatalf("height %d view lines = %d, want %d", height, len(lines), height)
		}
		rootLine := lines[1+headerDividerHeight(height)]
		if !strings.Contains(rootLine, root) {
			t.Fatalf("height %d root row = %q, want root path %q", height, rootLine, root)
		}
		if height >= 6 {
			infoRow := lines[gitInfoRowY(model)]
			if !strings.Contains(infoRow, branchTreeIcon) || !strings.Contains(infoRow, "feature") {
				t.Fatalf("height %d git info row = %q, want branch content", height, infoRow)
			}
		} else if strings.Contains(model.View().Content, branchTreeIcon) {
			t.Fatalf("height %d view contains the branch icon, want the git info row omitted", height)
		}
	}
}

func TestGitInfoRowTruncatesWithinWidth(t *testing.T) {
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
		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		for lineNumber, line := range lines {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d line %d cell width = %d, want <= %d: %q", width, lineNumber, got, width, line)
			}
		}
		if width == 80 {
			infoRow := lines[gitInfoRowY(model)]
			if !strings.Contains(infoRow, "very-long-feature-branch-name") || !strings.Contains(infoRow, "a-very-long-repository-name") {
				t.Fatalf("full-width git info row = %q, want untruncated branch and repo", infoRow)
			}
		}
	}
}

// applyReload dispatches a reload command's message: bubbletea v2 unwraps a
// single-command Batch into the command itself, so the result may arrive as
// a bare browser.LoadResult or as a tea.BatchMsg.
func applyReload(t *testing.T, model *Model, cmd tea.Cmd) {
	t.Helper()
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
