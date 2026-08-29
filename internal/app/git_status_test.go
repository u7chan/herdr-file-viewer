package app

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestInitialLoadReadsGitStatusThroughTheCommandOnce(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)

	if fake.statusCalls != 0 {
		t.Fatalf("NewModel() Git status calls = %d, want 0", fake.statusCalls)
	}
	load := model.Init()
	if load == nil {
		t.Fatal("Init() returned nil command")
	}
	if fake.statusCalls != 0 {
		t.Fatalf("Init() Git status calls = %d, want command-deferred read", fake.statusCalls)
	}
	result, ok := load().(browser.LoadResult)
	if !ok {
		t.Fatalf("Init() message = %T, want browser.LoadResult", load())
	}
	if fake.statusCalls != 1 {
		t.Fatalf("initial command Git status calls = %d, want 1", fake.statusCalls)
	}
	model.Update(result)
	model.Init()
	if fake.statusCalls != 1 {
		t.Fatalf("repeated Init() Git status calls = %d, want 1", fake.statusCalls)
	}
}

func TestReloadKeyRefreshesGitStatusSnapshot(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "file", Status: filesystem.GitStatusModified},
	}
	model := NewModel(root, "", fake)
	model.Update(model.Init()().(browser.LoadResult))
	if fake.statusCalls != 1 {
		t.Fatalf("initial Git status calls = %d, want 1", fake.statusCalls)
	}

	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "file", Status: filesystem.GitStatusUntracked},
	}
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	result, ok := cmd().(browser.LoadResult)
	if !ok {
		t.Fatalf("reload message = %T, want browser.LoadResult", cmd())
	}
	model.Update(result)

	if fake.statusCalls != 2 {
		t.Fatalf("Git status calls after reload = %d, want 2", fake.statusCalls)
	}
	if got := model.tree.GitStatusForPath(filepath.Join(root, "file")); got != browser.GitStatusUntracked {
		t.Fatalf("status after reload = %v, want untracked", got)
	}
}

func TestGitStatusColorsRowsAndAggregatesDirectories(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "directory", Mode: fs.ModeDir},
		{Name: "added", Mode: 0},
		{Name: "clean", Mode: 0},
		{Name: "modified", Mode: 0},
		{Name: "unmerged", Mode: 0},
	})
	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "directory/nested", Status: filesystem.GitStatusUntracked},
		{Path: "added", Status: filesystem.GitStatusAdded},
		{Path: "modified", Status: filesystem.GitStatusModified},
		{Path: "unmerged", Status: filesystem.GitStatusUnmerged},
		{Path: "deleted", Status: filesystem.GitStatusDeleted},
	}
	model := NewModel(root, "", fake)
	model.Update(model.Init()().(browser.LoadResult))
	model.Update(teaWindowSize(80, 10))

	view := model.View().Content
	for _, test := range []struct {
		name   string
		status browser.GitStatus
	}{
		{name: "directory", status: browser.GitStatusUntracked},
		{name: "added", status: browser.GitStatusAdded},
		{name: "modified", status: browser.GitStatusModified},
		{name: "unmerged", status: browser.GitStatusUnmerged},
	} {
		if !strings.Contains(view, gitStatusStyle(test.status).Render(" "+test.name)) {
			t.Errorf("view does not color the %s name with its Git status: %q", test.name, view)
		}
		if strings.Contains(view, gitStatusStyle(test.status).Render(iconForTestName(test.name))) {
			t.Errorf("Git status color leaked into the %s icon: %q", test.name, view)
		}
	}
	if strings.Contains(view, gitStatusStyle(browser.GitStatusModified).Render(" clean")) {
		t.Fatal("clean row unexpectedly uses a Git status style")
	}

	for _, width := range []int{1, 8, 24, 80} {
		model.Update(teaWindowSize(width, 10))
		for lineNumber, line := range strings.Split(ansi.Strip(model.View().Content), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d line %d width = %d, want <= %d: %q", width, lineNumber, got, width, line)
			}
		}
	}
}

func TestTreeRowsKeepIconPaletteColorSeparateFromGitStatusColor(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "main.go", Mode: 0}})
	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "main.go", Status: filesystem.GitStatusModified},
	}
	model := NewModel(root, "", fake)
	model.Update(model.Init()().(browser.LoadResult))
	model.Update(teaWindowSize(80, 10))
	view := model.View().Content

	goIcon := fileIconFor("main.go", iconsFor(defaultTreeIconSet).file)
	if !strings.Contains(view, iconStyle(goIcon).Render(goIcon)) {
		t.Errorf("view does not render the Go icon with its palette color: %q", view)
	}
	if strings.Contains(view, gitStatusStyle(browser.GitStatusModified).Render(goIcon)) {
		t.Errorf("Git status color leaked into the Go icon: %q", view)
	}
	if !strings.Contains(view, gitStatusStyle(browser.GitStatusModified).Render(" main.go")) {
		t.Errorf("view does not color the file name with its Git status: %q", view)
	}
}

func TestIndentKeepsRootDirectChildrenAlignedAndCompressesOnlyFirstLevel(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	nestedPath := filepath.Join(directoryPath, "nested")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(directoryPath, []filesystem.Entry{{Name: "nested", Mode: fs.ModeDir}})
	fake.set(nestedPath, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})().(browser.LoadResult))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})().(browser.LoadResult))
	model.Update(teaWindowSize(80, 10))

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	start := model.treeStartY()
	if got, want := strings.Index(lines[start], rootTreeIcon), 1; got != want {
		t.Fatalf("root icon column = %d, want %d: %q", got, want, lines[start])
	}
	if got, want := strings.Index(lines[start+1], expandedTreeIcon), 1; got != want {
		t.Fatalf("depth 1 chevron column = %d, want %d: %q", got, want, lines[start+1])
	}
	if got, want := strings.Index(lines[start+2], expandedTreeIcon), 3; got != want {
		t.Fatalf("depth 2 chevron column = %d, want %d: %q", got, want, lines[start+2])
	}
	if got, want := strings.Index(lines[start+3], fileIconFor("file", iconsFor(defaultTreeIconSet).file)), 7; got != want {
		t.Fatalf("depth 3 icon column = %d, want %d: %q", got, want, lines[start+3])
	}
}

// Keep the test's message construction independent of model.go's test-only
// convenience wrappers.
func teaWindowSize(width, height int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: width, Height: height}
}

func iconForTestName(name string) string {
	if name == "directory" {
		return iconsFor(defaultTreeIconSet).directory
	}
	return fileIconFor(name, iconsFor(defaultTreeIconSet).file)
}

type statusFileSystem struct {
	directories map[string][]filesystem.Entry
	statuses    []filesystem.GitStatusEntry
	statusCalls int
}

func newStatusFileSystem() *statusFileSystem {
	return &statusFileSystem{directories: make(map[string][]filesystem.Entry)}
}

func (f *statusFileSystem) set(path string, entries []filesystem.Entry) {
	f.directories[cleanAbsolute(path)] = append([]filesystem.Entry(nil), entries...)
}

func (f *statusFileSystem) ReadDir(path string) ([]filesystem.Entry, error) {
	entries, ok := f.directories[cleanAbsolute(path)]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]filesystem.Entry(nil), entries...), nil
}

func (f *statusFileSystem) ReadGitStatus(string) ([]filesystem.GitStatusEntry, error) {
	f.statusCalls++
	return append([]filesystem.GitStatusEntry(nil), f.statuses...), nil
}
