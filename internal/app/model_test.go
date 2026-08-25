package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
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

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if model.selected != 0 {
		t.Fatalf("up at top selected = %d, want 0", model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.selected != len(model.visibleRows)-1 {
		t.Fatalf("down at bottom selected = %d, want %d", model.selected, len(model.visibleRows)-1)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.selected != len(model.visibleRows)-1 {
		t.Fatalf("down past bottom selected = %d, want %d", model.selected, len(model.visibleRows)-1)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyUp})
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
	model.Update(tea.WindowSizeMsg{Width: 24, Height: 4})
	for index := 0; index < 4; index++ {
		model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if model.selected != 4 || model.offset != 3 {
		t.Fatalf("viewport state = selected %d, offset %d; want 4, 3", model.selected, model.offset)
	}
	if lines := strings.Split(ansi.Strip(model.View().Content), "\n"); len(lines) != 4 {
		t.Fatalf("viewport view lines = %d, want window height 4: %q", len(lines), model.View().Content)
	}

	model.Update(tea.WindowSizeMsg{Width: 1, Height: 1})
	_ = model.View()
	model.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	view := model.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeNone {
		t.Fatalf("zero-size view flags = AltScreen %v, MouseMode %v", view.AltScreen, view.MouseMode)
	}
	model.Update(tea.WindowSizeMsg{Width: -1, Height: -2})
	_ = model.View()
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
