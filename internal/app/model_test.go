package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestModelQuitsOnQAndCtrlC(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		model := NewModel("/workspace", "")
		_, cmd := model.Update(key)
		if cmd == nil {
			t.Fatalf("Update(%q) returned nil command", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("Update(%q) command = %T, want tea.QuitMsg", key.String(), cmd())
		}
	}
}

func TestPendingLoadProcessesQuitAndCtrlC(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, nil)

	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		model := NewModel(root, "", fake)
		load := model.Init()
		if load == nil || !model.loading {
			t.Fatalf("Init() load = %v, loading = %v; want pending load", load != nil, model.loading)
		}

		_, quit := model.Update(key)
		if quit == nil {
			t.Fatalf("Update(%q) during pending load returned nil command", key.String())
		}
		if _, ok := quit().(tea.QuitMsg); !ok {
			t.Fatalf("Update(%q) command = %T, want tea.QuitMsg", key.String(), quit())
		}
		if got := fake.calls(); len(got) != 0 {
			t.Fatalf("Update(%q) started filesystem work = %v, want command still pending", key.String(), got)
		}
	}
}

func TestPendingLoadProcessesResizeAndPreservesViewport(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "b-directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "a-directory", Mode: fs.ModeDir},
		{Name: "b-directory", Mode: fs.ModeDir},
	})
	fake.set(directoryPath, nil)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	selectedBefore := model.selected
	offsetBefore := model.offset
	if selectedBefore != 2 || offsetBefore != 1 {
		t.Fatalf("viewport before pending load = selected %d, offset %d; want 2, 1", selectedBefore, offsetBefore)
	}
	load := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if load == nil || !model.loading {
		t.Fatalf("right load = %v, loading = %v; want pending load", load != nil, model.loading)
	}

	if _, next := model.Update(tea.WindowSizeMsg{Width: 80, Height: 6}); next != nil {
		t.Fatalf("resize during pending load returned command %v, want nil", next)
	}
	if model.width != 80 || model.height != 6 {
		t.Fatalf("resize dimensions = %d x %d, want 80 x 6", model.width, model.height)
	}
	if model.selected != selectedBefore || model.offset != offsetBefore {
		t.Fatalf("viewport after pending resize = selected %d, offset %d; want %d, %d", model.selected, model.offset, selectedBefore, offsetBefore)
	}
	if got := fake.calls(); len(got) != 1 || got[0] != root {
		t.Fatalf("resize during pending load changed filesystem calls = %v, want only initial load", got)
	}
}

func TestInitStartsAsyncRootLoadAndAppliesResultInUpdate(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "file", Mode: 0},
		{Name: "directory", Mode: fs.ModeDir},
	})
	model := NewModel(root, "", fake)

	if got := fake.calls(); len(got) != 0 {
		t.Fatalf("NewModel() filesystem calls = %v, want none", got)
	}
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil command")
	}
	if !model.loading || !model.tree.Root().Loading() {
		t.Fatalf("initial loading state = model %v, node %v; want both true", model.loading, model.tree.Root().Loading())
	}
	if got := len(model.visibleRows); got != 1 {
		t.Fatalf("initial visible rows = %d, want root only", got)
	}
	if got := fake.calls(); len(got) != 0 {
		t.Fatalf("Init() filesystem calls = %v, want command-deferred read", got)
	}

	result, ok := cmd().(browser.LoadResult)
	if !ok {
		t.Fatalf("Init() command message = %T, want browser.LoadResult", cmd())
	}
	if got := fake.calls(); len(got) != 1 || got[0] != model.tree.Root().Path() {
		t.Fatalf("command filesystem calls = %v, want root read", got)
	}
	if _, next := model.Update(result); next != nil {
		t.Fatalf("Update(success) returned command %v, want nil", next)
	}
	if model.loading || model.status != readyStatus {
		t.Fatalf("success state = loading %v, status %q; want false, %q", model.loading, model.status, readyStatus)
	}
	if got := len(model.visibleRows); got != 3 {
		t.Fatalf("loaded visible rows = %d, want root and two entries", got)
	}
}

func TestLoadErrorIsRecoverableAndRetryIsAsync(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.setError(root, errors.New("permission denied\x1b\n"))
	model := NewModel(root, "", fake)

	first := model.Init()
	result := first().(browser.LoadResult)
	model.Update(result)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	if model.loading || model.tree.Root().Loaded() || model.tree.Root().Loading() {
		t.Fatalf("error state = loading %v, loaded %v, node loading %v; want idle and unloaded", model.loading, model.tree.Root().Loaded(), model.tree.Root().Loading())
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "Error:") {
		t.Fatalf("error view = %q, want error status", ansi.Strip(model.View().Content))
	}
	if strings.ContainsAny(model.status, "\x00\x1b\n\r\t") {
		t.Fatalf("status contains terminal controls: %q", model.status)
	}

	retry := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if retry == nil {
		t.Fatal("right after error returned nil retry command")
	}
	if !model.loading || !model.tree.Root().Loading() {
		t.Fatalf("retry state = loading %v, node loading %v; want both true", model.loading, model.tree.Root().Loading())
	}
	model.Update(retry().(browser.LoadResult))
	if got := len(fake.calls()); got != 2 {
		t.Fatalf("retry filesystem calls = %d, want 2", got)
	}
}

func TestKeyboardNavigationHasBoundariesAndDoesNotReadFilesystem(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "a-dir", Mode: fs.ModeDir},
		{Name: "b-dir", Mode: fs.ModeDir},
		{Name: "file", Mode: 0},
	})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	calls := len(fake.calls())

	_ = model.View()
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if model.selected != 0 {
		t.Fatalf("up at top selected = %d, want 0", model.selected)
	}
	for range 3 {
		_ = model.View()
		model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if model.selected != len(model.visibleRows)-1 {
		t.Fatalf("down at bottom selected = %d, want %d", model.selected, len(model.visibleRows)-1)
	}
	_ = model.View()
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.selected != len(model.visibleRows)-1 {
		t.Fatalf("down past bottom selected = %d, want %d", model.selected, len(model.visibleRows)-1)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyUp})
	_ = model.View()
	if model.selected != len(model.visibleRows)-2 {
		t.Fatalf("up selected = %d, want %d", model.selected, len(model.visibleRows)-2)
	}
	if len(fake.calls()) != calls {
		t.Fatalf("arrow navigation filesystem calls = %v, want unchanged count %d", fake.calls(), calls)
	}
	if model.tree.VisibleRowsDirty() {
		t.Fatal("arrow navigation dirtied visible rows")
	}
}

func TestRightExpandsDirectoryAndLeftCollapsesOrMovesToParent(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "child", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); cmd == nil {
		t.Fatal("right on directory returned nil command")
	} else {
		model.Update(cmd().(browser.LoadResult))
	}
	if len(model.visibleRows) != 3 {
		t.Fatalf("expanded rows = %d, want 3", len(model.visibleRows))
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if model.selected != 1 || model.selectedNode().Name() != "directory" {
		t.Fatalf("left from child selected row = %d (%q), want directory row 1", model.selected, model.selectedNode().Name())
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if len(model.visibleRows) != 2 || model.selected != 1 {
		t.Fatalf("collapse state = rows %d, selected %d, want rows 2 and directory selected", len(model.visibleRows), model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if model.selected != 0 || model.selectedNode() != model.tree.Root() {
		t.Fatalf("left from collapsed directory selected = %d, want root row 0", model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if len(model.visibleRows) != 2 || !model.tree.Root().Expanded() || model.selected != 0 {
		t.Fatalf("left on root state = rows %d, root expanded %v, selected %d; want rows 2, true, 0", len(model.visibleRows), model.tree.Root().Expanded(), model.selected)
	}
}

func TestEnterIsNoOpAndStaleResultsAreIgnored(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	cmd := model.Init()
	result := cmd().(browser.LoadResult)

	stale := result
	stale.Path = filepath.Join(root, "other")
	model.Update(stale)
	if !model.loading || len(model.visibleRows) != 1 {
		t.Fatalf("stale result changed state: loading %v, rows %d", model.loading, len(model.visibleRows))
	}
	model.Update(result)
	status := model.status
	selected := model.selected
	offset := model.offset
	if _, next := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); next != nil {
		t.Fatalf("enter returned command %v, want nil", next)
	}
	if model.status != status || model.selected != selected || model.offset != offset {
		t.Fatalf("enter changed state: status %q/%q selected %d/%d offset %d/%d", model.status, status, model.selected, selected, model.offset, offset)
	}
	model.Update(result)
	if len(model.visibleRows) != 2 || model.loading {
		t.Fatalf("duplicate result changed state: rows %d, loading %v", len(model.visibleRows), model.loading)
	}
}

func TestSpaceAndFullWidthSpaceCopyOnlyTheSelectedAbsolutePath(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	filePath := filepath.Join(root, "file")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "directory", Mode: fs.ModeDir},
		{Name: "file", Mode: 0},
	})
	fake.set(directoryPath, nil)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	keys := []tea.KeyPressMsg{
		{Code: tea.KeySpace},
		{Code: '　', Text: "　"},
	}
	for _, key := range keys {
		cmd := model.UpdateKey(key)
		if got := clipboardContent(t, cmd); got != root {
			t.Fatalf("copy key %q copied %q, want root %q", key.String(), got, root)
		}
		if model.status != readyStatus {
			t.Fatalf("copy key %q status = %q, want unchanged %q", key.String(), model.status, readyStatus)
		}
	}

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := clipboardContent(t, model.UpdateKey(tea.KeyPressMsg{Code: tea.KeySpace})); got != directoryPath {
		t.Fatalf("directory copy = %q, want %q", got, directoryPath)
	}
	if model.status != readyStatus {
		t.Fatalf("directory copy status = %q, want unchanged %q", model.status, readyStatus)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := clipboardContent(t, model.UpdateKey(tea.KeyPressMsg{Code: tea.KeySpace})); got != filePath {
		t.Fatalf("file copy = %q, want %q", got, filePath)
	}
	if model.status != readyStatus {
		t.Fatalf("file copy status = %q, want unchanged %q", model.status, readyStatus)
	}
}

func TestMouseClickMapsVisibleRowsUsingHeaderAndViewportOffset(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	entries := make([]filesystem.Entry, 0, 6)
	for index := 0; index < 6; index++ {
		entries = append(entries, filesystem.Entry{Name: string(rune('a' + index)), Mode: 0})
	}
	fake.set(root, entries)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 7})
	for index := 0; index < 3; index++ {
		model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if model.selected != 3 || model.offset != 1 {
		t.Fatalf("viewport before click = selected %d, offset %d; want 3, 1", model.selected, model.offset)
	}

	if _, cmd := model.Update(tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("visible-row click returned command %v, want nil", cmd)
	}
	if model.selected != 2 || model.selectedNode().Name() != "b" {
		t.Fatalf("offset row click = selected %d (%q), want row 2 (b)", model.selected, model.selectedNode().Name())
	}

	selected, offset, status := model.selected, model.offset, model.status
	for _, y := range []int{-1, 0, 1, 5, 6, 7} {
		if _, cmd := model.Update(tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft}); cmd != nil {
			t.Fatalf("out-of-tree click at y=%d returned command %v, want nil", y, cmd)
		}
		if model.selected != selected || model.offset != offset || model.status != status {
			t.Fatalf("out-of-tree click at y=%d changed state: selected %d/%d offset %d/%d status %q/%q", y, model.selected, selected, model.offset, offset, model.status, status)
		}
	}
}

func TestMouseDirectoryClickExpandsCollapsesAndLoadsAsynchronously(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "child", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 6})

	cmd := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("directory click returned nil command, want async load")
	}
	directory := model.tree.Root().Children()[0]
	if model.selected != 1 || !directory.Expanded() || !directory.Loading() {
		t.Fatalf("directory click state = selected %d, expanded %v, loading %v; want 1, true, true", model.selected, directory.Expanded(), directory.Loading())
	}
	if got := fake.calls(); len(got) != 1 || got[0] != root {
		t.Fatalf("directory click performed filesystem calls %v before command, want root only", got)
	}

	result, ok := cmd().(browser.LoadResult)
	if !ok {
		t.Fatalf("directory click command message = %T, want browser.LoadResult", cmd())
	}
	model.Update(result)
	if len(model.visibleRows) != 3 || !directory.Loaded() || directory.Loading() {
		t.Fatalf("loaded directory state = rows %d, loaded %v, loading %v; want 3, true, false", len(model.visibleRows), directory.Loaded(), directory.Loading())
	}

	if cmd := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("expanded directory click returned command %v, want nil collapse", cmd)
	}
	if len(model.visibleRows) != 2 || directory.Expanded() || model.selected != 1 {
		t.Fatalf("directory collapse state = rows %d, expanded %v, selected %d; want 2, false, 1", len(model.visibleRows), directory.Expanded(), model.selected)
	}

	if cmd := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("loaded directory re-expand returned command %v, want nil", cmd)
	}
	if len(model.visibleRows) != 3 || !directory.Expanded() {
		t.Fatalf("directory re-expand state = rows %d, expanded %v; want 3, true", len(model.visibleRows), directory.Expanded())
	}
	if got := fake.calls(); len(got) != 2 {
		t.Fatalf("directory click filesystem calls = %v, want root and directory only", got)
	}
}

func TestMouseClickSelectsRootFileAndSymlinkWithoutCopyOrPreview(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "directory", Mode: fs.ModeDir},
		{Name: "file", Mode: 0},
		{Name: "link", Mode: fs.ModeSymlink},
	})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	calls := len(fake.calls())

	if cmd := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: 2, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("root click returned command %v, want nil", cmd)
	}
	if model.selected != 0 || !model.tree.Root().Expanded() || len(model.visibleRows) != 4 {
		t.Fatalf("root click state = selected %d, expanded %v, rows %d; want 0, true, 4", model.selected, model.tree.Root().Expanded(), len(model.visibleRows))
	}

	if cmd := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: 4, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("file click returned command %v, want nil", cmd)
	}
	if model.selected != 2 || model.selectedNode().Name() != "file" || model.status != readyStatus {
		t.Fatalf("file click state = selected %d (%q), status %q; want file row and %q", model.selected, model.selectedNode().Name(), model.status, readyStatus)
	}

	if cmd := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: 5, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("symlink click returned command %v, want nil", cmd)
	}
	if model.selected != 3 || model.selectedNode().Name() != "link" || model.status != readyStatus {
		t.Fatalf("symlink click state = selected %d (%q), status %q; want link row and %q", model.selected, model.selectedNode().Name(), model.status, readyStatus)
	}
	if len(fake.calls()) != calls {
		t.Fatalf("selection-only clicks changed filesystem calls = %v, want %d calls", fake.calls(), calls)
	}
}

func TestMouseIgnoresNonLeftEventsAndViewDoesNotRequestAllMotion(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	selected, offset, status, calls := model.selected, model.offset, model.status, len(fake.calls())

	for _, msg := range []tea.Msg{
		tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseRight},
		tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseMiddle},
		tea.MouseWheelMsg{X: 0, Y: 2, Button: tea.MouseWheelDown},
		tea.MouseReleaseMsg{X: 0, Y: 3, Button: tea.MouseLeft},
		tea.MouseMotionMsg{X: 0, Y: 3, Button: tea.MouseLeft},
	} {
		if _, cmd := model.Update(msg); cmd != nil {
			t.Fatalf("ignored mouse event %T returned command %v, want nil", msg, cmd)
		}
	}
	if model.selected != selected || model.offset != offset || model.status != status || len(fake.calls()) != calls {
		t.Fatalf("ignored mouse events changed state: selected %d/%d offset %d/%d status %q/%q calls %d/%d", model.selected, selected, model.offset, offset, model.status, status, len(fake.calls()), calls)
	}

	view := model.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("view mouse mode = %v, want CellMotion", view.MouseMode)
	}
	if view.MouseMode == tea.MouseModeAllMotion {
		t.Fatal("view unexpectedly requested All Motion")
	}
	if view.OnMouse != nil {
		t.Fatal("view.OnMouse is set, want Update-based mouse handling")
	}
}

func TestFooterAndDividerReserveTheBottomOfTheViewport(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	if len(lines) != 6 {
		t.Fatalf("view lines = %d, want 6: %q", len(lines), model.View().Content)
	}
	header := lines[0]
	if strings.TrimSpace(header) != "Herdr File Viewer" {
		t.Fatalf("header = %q, want centered title", header)
	}
	leftPadding := len(header) - len(strings.TrimLeft(header, " "))
	rightPadding := len(header) - len(strings.TrimRight(header, " "))
	if leftPadding < rightPadding-1 || leftPadding > rightPadding+1 {
		t.Fatalf("header padding = left %d, right %d; want centered title", leftPadding, rightPadding)
	}
	if !strings.Contains(lines[1], dividerGlyph) || lipgloss.Width(lines[1]) != 80 {
		t.Fatalf("header divider = %q, want a full-width divider", lines[1])
	}
	if !strings.Contains(lines[4], dividerGlyph) || lipgloss.Width(lines[4]) != 80 {
		t.Fatalf("footer divider = %q, want a full-width divider", lines[4])
	}
	if got := strings.TrimRight(lines[len(lines)-1], " "); got != " space copy    r reload    q quit" {
		t.Fatalf("footer = %q, want shortcut hints at the bottom", lines[len(lines)-1])
	}
	if !strings.HasPrefix(lines[2], " "+rootTreeIcon+" ") {
		t.Fatalf("first tree row = %q, want one-cell left padding", lines[2])
	}
	if !strings.Contains(lines[2], root) {
		t.Fatalf("first tree row = %q, want root path %q", lines[2], root)
	}

	for _, test := range []struct {
		height     int
		wantHeader int
		wantTree   int
		wantFooter int
		wantTop    int
		wantBottom int
	}{
		{height: 0, wantHeader: 0, wantTree: 0, wantFooter: 0, wantTop: 0, wantBottom: 0},
		{height: 1, wantHeader: 1, wantTree: 0, wantFooter: 0, wantTop: 0, wantBottom: 0},
		{height: 2, wantHeader: 1, wantTree: 0, wantFooter: 1, wantTop: 0, wantBottom: 0},
		{height: 3, wantHeader: 1, wantTree: 1, wantFooter: 1, wantTop: 0, wantBottom: 0},
		{height: 4, wantHeader: 1, wantTree: 1, wantFooter: 1, wantTop: 1, wantBottom: 0},
		{height: 5, wantHeader: 1, wantTree: 1, wantFooter: 1, wantTop: 1, wantBottom: 1},
		{height: 6, wantHeader: 1, wantTree: 2, wantFooter: 1, wantTop: 1, wantBottom: 1},
	} {
		header, tree, footer := layoutHeights(test.height)
		if header != test.wantHeader || tree != test.wantTree || footer != test.wantFooter || headerDividerHeight(test.height) != test.wantTop || footerDividerHeight(test.height) != test.wantBottom {
			t.Errorf("layoutHeights(%d) = (%d, %d, %d), dividers %d/%d; want (%d, %d, %d), dividers %d/%d", test.height, header, tree, footer, headerDividerHeight(test.height), footerDividerHeight(test.height), test.wantHeader, test.wantTree, test.wantFooter, test.wantTop, test.wantBottom)
		}
	}

	for _, height := range []int{3, 4} {
		model.Update(tea.WindowSizeMsg{Width: 80, Height: height})
		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		_, treeHeight, _ := layoutHeights(height)
		rootLine := 1 + headerDividerHeight(height)
		if treeHeight < stickyRootHeight || !strings.Contains(lines[rootLine], root) {
			t.Fatalf("height %d root row = %q, want root path %q", height, lines[rootLine], root)
		}
	}
}

func TestFooterShowsOperationalStatusUntilReadyAndShortcutsWhenIdle(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	if got := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " "); got != " space copy    r reload    q quit" {
		t.Fatalf("initial footer = %q, want shortcut hints", got)
	}

	load := model.Init()
	if got := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " "); got != " "+loadingStatus {
		t.Fatalf("loading footer = %q, want %q", got, loadingStatus)
	}

	model.Update(load().(browser.LoadResult))
	if got := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " "); got != " space copy    r reload    q quit" {
		t.Fatalf("ready footer = %q, want shortcut hints", got)
	}
}

func TestReloadKeyReScansLoadedDirectoriesAndRestoresSelection(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{
		{Name: "one", Mode: 0},
		{Name: "two", Mode: 0},
	})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); cmd == nil {
		t.Fatal("expand returned nil command")
	} else {
		model.Update(cmd().(browser.LoadResult))
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if node := model.selectedNode(); node == nil || node.Name() != "two" {
		t.Fatalf("selection = %v, want two", model.selectedNode())
	}
	calls := len(fake.calls())

	fake.set(directoryPath, []filesystem.Entry{
		{Name: "one", Mode: 0},
		{Name: "two", Mode: 0},
		{Name: "three", Mode: 0},
	})
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("reload command message = %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("reload batch = %d commands, want 2", len(batch))
	}
	for _, reloadCmd := range batch {
		result, ok := reloadCmd().(browser.LoadResult)
		if !ok {
			t.Fatalf("reload command message = %T, want browser.LoadResult", reloadCmd())
		}
		model.Update(result)
	}

	if got := len(fake.calls()); got != calls+2 {
		t.Fatalf("filesystem calls = %d, want %d (root and directory re-read)", got, calls+2)
	}
	if node := model.selectedNode(); node == nil || node.Name() != "two" {
		t.Fatalf("selection after reload = %v, want two", model.selectedNode())
	}
	found := false
	for _, row := range model.visibleRows {
		if row.Node != nil && row.Node.Name() == "three" {
			found = true
		}
	}
	if !found {
		t.Fatal("newly added file is not visible after reload")
	}
	if !model.tree.Root().Expanded() {
		t.Fatal("reload collapsed the root")
	}
}

func TestReloadDropsSelectionAnchorWhenPathDisappears(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "two", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); cmd == nil {
		t.Fatal("expand returned nil command")
	} else {
		model.Update(cmd().(browser.LoadResult))
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if node := model.selectedNode(); node == nil || node.Name() != "two" {
		t.Fatalf("selection = %v, want two", model.selectedNode())
	}

	fake.set(directoryPath, nil)
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	batch := cmd().(tea.BatchMsg)
	for index := len(batch) - 1; index >= 0; index-- {
		model.Update(batch[index]().(browser.LoadResult))
	}

	if model.restorePath != "" {
		t.Fatalf("restorePath = %q, want anchor dropped", model.restorePath)
	}
	selected := model.selectedNode()
	if selected == nil || selected.Name() != "directory" {
		t.Fatalf("selection after vanished reload = %v, want directory", selected)
	}
	if model.loading {
		t.Fatal("reload left the model loading")
	}
}

func TestReloadCompletesWhenSelectedDirectoryDisappears(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); cmd == nil {
		t.Fatal("expand returned nil command")
	} else {
		model.Update(cmd().(browser.LoadResult))
	}

	fake.set(root, nil)
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	for _, reloadCmd := range cmd().(tea.BatchMsg) {
		model.Update(reloadCmd().(browser.LoadResult))
	}

	if model.loading {
		t.Fatal("reload with a removed directory left the model loading")
	}
	if got := model.status; got != model.readyStatus() {
		t.Fatalf("status after reload = %q, want %q", got, model.readyStatus())
	}
	if rows := model.visibleRows; len(rows) != 1 || rows[0].Node != model.tree.Root() {
		t.Fatalf("visible rows after reload = %v, want root only", rows)
	}
	if model.selected != 0 {
		t.Fatalf("selection after reload = %d, want root fallback", model.selected)
	}
}

func TestReloadShowsFooterToastOnceAndTimesOut(t *testing.T) {
	previousDuration := toastDisplayDuration
	toastDisplayDuration = time.Millisecond
	defer func() { toastDisplayDuration = previousDuration }()

	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); cmd == nil {
		t.Fatal("expand returned nil command")
	} else {
		model.Update(cmd().(browser.LoadResult))
	}

	fake.set(directoryPath, []filesystem.Entry{
		{Name: "file", Mode: 0},
		{Name: "added", Mode: 0},
	})
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	var toastCmd tea.Cmd
	for _, reloadCmd := range cmd().(tea.BatchMsg) {
		_, returned := model.Update(reloadCmd().(browser.LoadResult))
		if returned != nil {
			toastCmd = returned
		}
	}
	if toastCmd == nil {
		t.Fatal("reload completion returned no toast command")
	}
	if got := model.toast; got != "Reloaded" {
		t.Fatalf("toast = %q, want reload summary", got)
	}

	timeout, ok := toastCmd().(toastTimeoutMsg)
	if !ok {
		t.Fatalf("toast command message = %T, want toastTimeoutMsg", toastCmd())
	}
	model.Update(timeout)
	if model.toast != "" {
		t.Fatalf("toast = %q, want cleared after timeout", model.toast)
	}
}

func TestStaleToastTimerDoesNotClearNewerToast(t *testing.T) {
	previousDuration := toastDisplayDuration
	toastDisplayDuration = time.Millisecond
	defer func() { toastDisplayDuration = previousDuration }()

	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	reloadOnce := func() tea.Cmd {
		cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
		if cmd == nil {
			t.Fatal("reload returned nil command")
		}
		_, returned := model.Update(cmd().(browser.LoadResult))
		if returned == nil {
			t.Fatal("reload completion returned no toast command")
		}
		return returned
	}

	first := reloadOnce()
	if got := model.toast; got != "Reloaded" {
		t.Fatalf("toast = %q, want first reload summary", got)
	}
	second := reloadOnce()
	if got := model.toast; got != "Reloaded" {
		t.Fatalf("toast = %q, want second reload summary", got)
	}

	model.Update(first().(toastTimeoutMsg))
	if model.toast == "" {
		t.Fatal("stale timer cleared the newer toast")
	}
	model.Update(second().(toastTimeoutMsg))
	if model.toast != "" {
		t.Fatalf("toast = %q, want cleared by the current timer", model.toast)
	}
}

func TestReloadDoesNotToastOnError(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})

	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	fake.setError(root, errors.New("reload failed"))
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'}); cmd == nil {
		t.Fatal("reload returned nil command")
	} else if _, toast := model.Update(cmd().(browser.LoadResult)); toast != nil {
		t.Fatalf("failed reload returned command %v, want nil", toast)
	}
	if model.toast != "" {
		t.Fatalf("toast = %q after failed reload, want none", model.toast)
	}
}

func TestReloadMixedFailureKeepsErrorAndSuppressesToast(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "file", Mode: 0}})

	for _, errorFirst := range []bool{false, true} {
		delete(fake.errors, cleanAbsolute(directoryPath))
		model := NewModel(root, "", fake)
		completeInitialLoad(t, model)
		model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
		if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); cmd == nil {
			t.Fatal("expand returned nil command")
		} else {
			model.Update(cmd().(browser.LoadResult))
		}

		fake.setError(directoryPath, errors.New("read failed"))
		cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
		if cmd == nil {
			t.Fatal("reload returned nil command")
		}
		batch := cmd().(tea.BatchMsg)
		if len(batch) != 2 {
			t.Fatalf("reload batch = %d commands, want 2", len(batch))
		}
		results := make([]browser.LoadResult, 0, len(batch))
		for _, reloadCmd := range batch {
			results = append(results, reloadCmd().(browser.LoadResult))
		}

		var toastCmd tea.Cmd
		if errorFirst {
			for index := len(results) - 1; index >= 0; index-- {
				_, returned := model.Update(results[index])
				if returned != nil {
					toastCmd = returned
				}
			}
		} else {
			for _, result := range results {
				_, returned := model.Update(result)
				if returned != nil {
					toastCmd = returned
				}
			}
		}

		if toastCmd != nil {
			t.Fatalf("mixed-failure reload (errorFirst=%v) returned command %v, want nil", errorFirst, toastCmd)
		}
		if model.toast != "" {
			t.Fatalf("toast = %q after mixed failure, want none", model.toast)
		}
		if !strings.Contains(model.status, "Error:") {
			t.Fatalf("status = %q after mixed failure, want Error retained", model.status)
		}
	}
}

func TestReloadToastRendersInFooter(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'}); cmd == nil {
		t.Fatal("reload returned nil command")
	} else {
		model.Update(cmd().(browser.LoadResult))
	}
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	footer := lines[len(lines)-1]
	if !strings.HasPrefix(footer, " Reloaded") {
		t.Fatalf("footer = %q, want reload toast with the standard left padding", footer)
	}
	if got := strings.TrimRight(footer, " "); got != " Reloaded" {
		t.Fatalf("footer = %q, want only the reload toast", got)
	}
}

func TestMouseWheelAndScrollbarDragScrollWithoutReadingFilesystem(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	entries := make([]filesystem.Entry, 0, 20)
	for index := 0; index < 20; index++ {
		entries = append(entries, filesystem.Entry{Name: "file-" + string(rune('a'+index)), Mode: 0})
	}
	fake.set(root, entries)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 32, Height: 7})
	calls := len(fake.calls())
	startY := model.treeStartY()

	model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelDown})
	if model.offset != mouseWheelScrollLines || model.selected != 0 {
		t.Fatalf("wheel-down viewport = selected %d, offset %d; want 0, %d", model.selected, model.offset, mouseWheelScrollLines)
	}
	model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelUp})
	if model.offset != 0 || model.selected != 0 {
		t.Fatalf("wheel-up viewport = selected %d, offset %d; want 0, 0", model.selected, model.offset)
	}
	model.Update(tea.MouseWheelMsg{X: 0, Y: startY - 1, Button: tea.MouseWheelDown})
	if model.offset != 0 || model.selected != 0 {
		t.Fatalf("outside-tree wheel changed viewport = selected %d, offset %d", model.selected, model.offset)
	}

	for range 10 {
		model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelDown})
	}
	_, treeHeight, _ := layoutHeights(model.height)
	maxOffset := len(model.visibleRows) - treeHeight
	if model.offset != maxOffset || model.selected != 0 {
		t.Fatalf("wheel-to-bottom viewport = selected %d, offset %d; want 0, %d", model.selected, model.offset, maxOffset)
	}
	if len(fake.calls()) != calls {
		t.Fatalf("wheel scrolling changed filesystem calls = %v, want %d", fake.calls(), calls)
	}

	model.offset = 0
	model.selected = 0
	model.Update(tea.MouseClickMsg{X: model.width - 1, Y: startY + stickyRootHeight, Button: tea.MouseLeft})
	if !model.draggingScrollbar || model.offset != 0 {
		t.Fatalf("scrollbar press state = dragging %v, offset %d; want true, 0", model.draggingScrollbar, model.offset)
	}
	model.Update(tea.MouseMotionMsg{X: model.width - 1, Y: startY + 2, Button: tea.MouseLeft})
	if model.offset != maxOffset || model.selected != 0 {
		t.Fatalf("scrollbar drag viewport = selected %d, offset %d; want 0, %d", model.selected, model.offset, maxOffset)
	}
	model.Update(tea.MouseReleaseMsg{X: model.width - 1, Y: startY + 2, Button: tea.MouseLeft})
	if model.draggingScrollbar {
		t.Fatal("scrollbar release left dragging enabled")
	}
	if len(fake.calls()) != calls {
		t.Fatalf("scrollbar dragging changed filesystem calls = %v, want %d", fake.calls(), calls)
	}
}

func TestScrollbarIsVisibleAndTracksTheViewport(t *testing.T) {
	metrics := newScrollbarMetrics(3, 4, 0)
	if metrics.thumbSize != 2 || metrics.maxThumbStart() != 1 {
		t.Fatalf("small-overflow scrollbar metrics = %#v, want thumb size 2 and one track cell", metrics)
	}
	if got := metrics.offsetForThumbStart(metrics.maxThumbStart()); got != metrics.maxOffset() {
		t.Fatalf("bottom thumb offset = %d, want %d", got, metrics.maxOffset())
	}

	root := t.TempDir()
	fake := newFakeFileSystem()
	entries := make([]filesystem.Entry, 0, 20)
	for index := 0; index < 20; index++ {
		entries = append(entries, filesystem.Entry{Name: "file-" + string(rune('a'+index)), Mode: 0})
	}
	fake.set(root, entries)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 7})

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	startY := model.treeStartY()
	_, treeHeight, _ := layoutHeights(model.height)
	scrollHeight := model.scrollableViewportHeight()
	for row := 0; row < scrollHeight; row++ {
		line := lines[startY+stickyRootHeight+row]
		if !strings.HasSuffix(line, scrollbarTrackGlyph) && !strings.HasSuffix(line, scrollbarThumbGlyph) {
			t.Fatalf("scrollbar row %d = %q, want a scrollbar glyph", row, line)
		}
	}
	if !strings.HasSuffix(lines[startY+stickyRootHeight], scrollbarThumbGlyph) {
		t.Fatalf("top scrollbar = %q, want thumb", lines[startY+stickyRootHeight])
	}

	model.offset = len(model.visibleRows) - treeHeight
	lines = strings.Split(ansi.Strip(model.View().Content), "\n")
	if !strings.HasSuffix(lines[startY+stickyRootHeight+scrollHeight-1], scrollbarThumbGlyph) {
		t.Fatalf("bottom scrollbar = %q, want thumb on the last row", lines[startY+stickyRootHeight+scrollHeight-1])
	}
	if !strings.Contains(lines[startY], model.tree.Root().Path()) {
		t.Fatalf("sticky root row = %q, want root path %q", lines[startY], model.tree.Root().Path())
	}
}

func TestRootPathStaysStickyWhileDescendantsScroll(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	entries := make([]filesystem.Entry, 0, 12)
	for index := 0; index < 12; index++ {
		entries = append(entries, filesystem.Entry{Name: "file-" + string(rune('a'+index)), Mode: 0})
	}
	fake.set(root, entries)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 7})

	startY := model.treeStartY()
	before := strings.Split(ansi.Strip(model.View().Content), "\n")
	rootLine := before[startY]
	if index, ok := model.rowIndexAtY(startY); !ok || index != 0 {
		t.Fatalf("sticky root hit-test = (%d, %v), want (0, true)", index, ok)
	}
	if model.isScrollbarCell(model.width-1, startY) {
		t.Fatal("sticky root row is incorrectly treated as scrollbar")
	}

	model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelDown})
	after := strings.Split(ansi.Strip(model.View().Content), "\n")
	if model.offset == 0 {
		t.Fatal("wheel over the tree did not scroll descendants")
	}
	if after[startY] != rootLine {
		t.Fatalf("root row after scrolling = %q, want unchanged %q", after[startY], rootLine)
	}
}

func TestVimLikeKeysProvideLinePageAndBoundaryNavigation(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	entries := make([]filesystem.Entry, 0, 12)
	for index := 0; index < 12; index++ {
		entries = append(entries, filesystem.Entry{Name: "file-" + string(rune('a'+index)), Mode: 0})
	}
	fake.set(root, entries)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 32, Height: 7})

	model.UpdateKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if model.selected != 1 {
		t.Fatalf("j selected %d, want 1", model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if model.selected != 0 {
		t.Fatalf("k selected %d, want 0", model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if model.selected != 1 {
		t.Fatalf("ctrl-d selected %d, want 1", model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if model.selected != 2 {
		t.Fatalf("ctrl-f selected %d, want 2", model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyHome})
	if model.selected != 0 || model.offset != 0 {
		t.Fatalf("home viewport = selected %d, offset %d; want 0, 0", model.selected, model.offset)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	if model.selected != len(model.visibleRows)-1 || model.offset != len(model.visibleRows)-3 {
		t.Fatalf("end viewport = selected %d, offset %d; want %d, %d", model.selected, model.offset, len(model.visibleRows)-1, len(model.visibleRows)-3)
	}
}

func TestViewportFollowsSelectionAndNarrowOrZeroWindowsAreSafe(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	entries := make([]filesystem.Entry, 0, 6)
	for index := 0; index < 6; index++ {
		entries = append(entries, filesystem.Entry{Name: string(rune('a' + index)), Mode: 0})
	}
	fake.set(root, entries)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 24, Height: 6})
	for index := 0; index < 4; index++ {
		model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if model.selected != 4 || model.offset != 3 {
		t.Fatalf("viewport state = selected %d, offset %d; want 4, 3", model.selected, model.offset)
	}
	if lines := strings.Split(ansi.Strip(model.View().Content), "\n"); len(lines) != 6 {
		t.Fatalf("viewport view lines = %d, want window height 6: %q", len(lines), model.View().Content)
	}

	model.Update(tea.WindowSizeMsg{Width: 1, Height: 1})
	_ = model.View()
	model.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	view := model.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("zero-size view flags = AltScreen %v, MouseMode %v", view.AltScreen, view.MouseMode)
	}
	model.Update(tea.WindowSizeMsg{Width: -1, Height: -2})
	_ = model.View()
}

func TestRenderingUsesCellWidthForUnicodeAndNarrowWindows(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "日本語", Mode: 0},
		{Name: "emoji🙂", Mode: 0},
		{Name: "e\u0301-combining", Mode: 0},
		{Name: "control\x00\x1b\nname", Mode: 0},
	})
	model := NewModel(root, "status-日本語🙂", fake)
	completeInitialLoad(t, model)

	for width := 0; width <= 8; width++ {
		model.Update(tea.WindowSizeMsg{Width: width, Height: 8})
		content := ansi.Strip(model.View().Content)
		for lineNumber, line := range strings.Split(content, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d line %d cell width = %d, want <= %d: %q", width, lineNumber, got, width, line)
			}
			if strings.ContainsAny(line, "\x00\x1b\n\r\t\x7f") {
				t.Fatalf("width %d line %d contains terminal controls: %q", width, lineNumber, line)
			}
		}
	}
}

func TestTruncateToWidthHandlesPrefixesAndGraphemes(t *testing.T) {
	for _, value := range []string{"▸ 日本語🙂", "  日本語🙂", "e\u0301", "👨‍👩‍👧‍👦"} {
		for width := 0; width <= 2; width++ {
			got := truncateToWidth(value, width)
			if actual := lipgloss.Width(got); actual > width {
				t.Errorf("truncateToWidth(%q, %d) width = %d, want <= %d: %q", value, width, actual, width, got)
			}
		}
	}
}

func TestDirectoryLoadErrorIsVisibleAndRetryable(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.setError(directoryPath, errors.New("directory disappeared\x1b"))
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	load := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if load == nil {
		t.Fatal("right returned nil command, want directory load")
	}
	model.Update(load().(browser.LoadResult))
	directory := model.selectedNode()
	if model.loading || directory.Loading() || directory.Loaded() {
		t.Fatalf("directory error state = model loading %v, node loading %v, loaded %v; want idle and unloaded", model.loading, directory.Loading(), directory.Loaded())
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "Error:") {
		t.Fatalf("directory error view = %q, want error status", ansi.Strip(model.View().Content))
	}

	delete(fake.errors, cleanAbsolute(directoryPath))
	fake.set(directoryPath, []filesystem.Entry{{Name: "recovered", Mode: 0}})
	retry := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if retry == nil || !model.loading || !directory.Loading() {
		t.Fatalf("retry state = command %v, model loading %v, node loading %v; want pending retry", retry != nil, model.loading, directory.Loading())
	}
	model.Update(retry().(browser.LoadResult))
	if directory.LoadError() != nil || !directory.Loaded() || model.loading {
		t.Fatalf("recovered state = error %v, loaded %v, model loading %v; want successful idle load", directory.LoadError(), directory.Loaded(), model.loading)
	}
}

func TestTruncateToWidthAddsEllipsis(t *testing.T) {
	if got := truncateToWidth("exactly", 7); got != "exactly" {
		t.Fatalf("fitting value = %q, want unchanged", got)
	}
	for _, test := range []struct {
		value string
		width int
	}{
		{"a very long path", 5},
		{"日本語ファイル名.txt", 6},
		{"emoji🙂 name", 4},
	} {
		got := truncateToWidth(test.value, test.width)
		if actual := lipgloss.Width(got); actual > test.width {
			t.Errorf("truncateToWidth(%q, %d) width = %d, want <= %d: %q", test.value, test.width, actual, test.width, got)
		}
		if !strings.HasSuffix(got, ellipsis) {
			t.Errorf("truncateToWidth(%q, %d) = %q, want %q suffix", test.value, test.width, got, ellipsis)
		}
	}
	if got := truncateToWidth("xx", 1); got != ellipsis {
		t.Fatalf("width 1 = %q, want ellipsis only", got)
	}
}

func TestTruncateRootPathPreservesTheRootDirectoryName(t *testing.T) {
	path := "/home/user/workspace/herdr-file-viewer"

	if got := truncateRootPath(path, lipgloss.Width(path)); got != path {
		t.Fatalf("full-width root path = %q, want unchanged path %q", got, path)
	}
	if got := truncateRootPath(path, 19); got != "…/herdr-file-viewer" {
		t.Fatalf("narrow root path = %q, want %q", got, "…/herdr-file-viewer")
	}
	if got := truncateRootPath(path, 30); got != "…/workspace/herdr-file-viewer" {
		t.Fatalf("progressive root path = %q, want %q", got, "…/workspace/herdr-file-viewer")
	}
	for width := 0; width <= 19; width++ {
		if got := lipgloss.Width(truncateRootPath(path, width)); got > width {
			t.Errorf("root path width %d = %d, want <= %d", width, got, width)
		}
	}
}

func TestTruncateRootPathPreservesWideRootDirectoryName(t *testing.T) {
	path := "/home/日本語/長いディレクトリ名"

	if got := truncateRootPath(path, 20); got != "…/長いディレクトリ名" {
		t.Fatalf("wide root path = %q, want %q", got, "…/長いディレクトリ名")
	}
	// The cut boundary lands inside the last component; the straddling
	// grapheme is dropped so the tail still fits the budget.
	if got := truncateRootPath(path, 19); got != "…/いディレクトリ名" {
		t.Fatalf("straddling root path = %q, want %q", got, "…/いディレクトリ名")
	}
}

func TestTruncateRootPathStaysWithinWidthForWideCharacters(t *testing.T) {
	paths := []string{
		"/home/日本語/長いディレクトリ名",
		"/home/emoji🙂/dir",
		"/home/user/e\u0301-combining/名前",
		"/home/👨‍👩‍👧‍👦/家族",
	}
	for _, path := range paths {
		width := lipgloss.Width(path)
		for want := 0; want <= width; want++ {
			if got := lipgloss.Width(truncateRootPath(path, want)); got > want {
				t.Errorf("truncateRootPath(%q, %d) width = %d, want <= %d", path, want, got, want)
			}
		}
	}
}

func TestRootRowRendersWithoutChevron(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	content := ansi.Strip(model.View().Content)
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		t.Fatalf("view lines = %d, want at least 3", len(lines))
	}
	if strings.ContainsAny(lines[2], "▸▾") {
		t.Fatalf("root row = %q, want no chevron", lines[2])
	}
	if !strings.Contains(lines[2], "") {
		t.Fatalf("root row = %q, want the home icon", lines[2])
	}
	if !strings.Contains(lines[2], root) {
		t.Fatalf("root row = %q, want path %q", lines[2], root)
	}
}

func TestRootMouseClickSelectsWithoutCollapsing(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 6})

	if cmd := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: 2, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("root click returned command %v, want nil", cmd)
	}
	if len(model.visibleRows) != 2 || !model.tree.Root().Expanded() || model.selected != 0 {
		t.Fatalf("root click state = rows %d, root expanded %v, selected %d; want rows 2, true, 0", len(model.visibleRows), model.tree.Root().Expanded(), model.selected)
	}
}

func TestTerminalSafeSanitizationCoversDerivedStrings(t *testing.T) {

	value := sanitizeDisplay("ok\x00\x1b\n\r\t\x7f\u0085\xff")
	if strings.ContainsAny(value, "\x00\x1b\n\r\t\x7f") {
		t.Fatalf("sanitizeDisplay() = %q, contains terminal controls", value)
	}
	if !strings.Contains(value, "\uFFFD") {
		t.Fatalf("sanitizeDisplay() = %q, want replacement markers", value)
	}

	root := filepath.Join(t.TempDir(), "unsafe\x1b\nroot")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "unsafe\tname", Mode: 0}})
	model := NewModel(root, "warning\rtext", fake)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	plain := ansi.Strip(model.View().Content)
	if strings.ContainsAny(plain, "\x00\x1b\r\t") {
		t.Fatalf("filesystem-derived view = %q, contains terminal controls", plain)
	}
	if !strings.Contains(plain, "unsafe�") || !strings.Contains(plain, "warning�text") {
		t.Fatalf("filesystem-derived view = %q, want sanitized path/name/warning", plain)
	}
}

// UpdateKey keeps the key-driven tests focused on the resulting command.
func (m *Model) UpdateKey(key tea.KeyPressMsg) tea.Cmd {
	_, cmd := m.Update(key)
	return cmd
}

func (m *Model) UpdateMouse(mouse tea.MouseClickMsg) tea.Cmd {
	_, cmd := m.Update(mouse)
	return cmd
}

func clipboardContent(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("copy returned nil command")
	}
	message := cmd()
	if message == nil {
		t.Fatal("copy command returned nil message")
	}
	value := reflect.ValueOf(message)
	if value.Kind() != reflect.String {
		t.Fatalf("copy command message = %T, want string-backed clipboard message", message)
	}
	return value.String()
}

func completeInitialLoad(t *testing.T, model *Model) {
	t.Helper()
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil command")
	}
	result, ok := cmd().(browser.LoadResult)
	if !ok {
		t.Fatalf("Init() message = %T, want browser.LoadResult", cmd())
	}
	model.Update(result)
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
	f.directories[cleanAbsolute(path)] = append([]filesystem.Entry(nil), entries...)
}

func (f *fakeFileSystem) setError(path string, err error) {
	f.errors[cleanAbsolute(path)] = err
}

func (f *fakeFileSystem) ReadDir(path string) ([]filesystem.Entry, error) {
	path = cleanAbsolute(path)
	f.readCalls = append(f.readCalls, path)
	if err, ok := f.errors[path]; ok {
		return nil, err
	}
	entries, ok := f.directories[path]
	if !ok {
		return nil, errors.New("fake directory not configured")
	}
	return append([]filesystem.Entry(nil), entries...), nil
}

func (f *fakeFileSystem) calls() []string {
	return append([]string(nil), f.readCalls...)
}

func cleanAbsolute(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}
