package app

import (
	"errors"
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
	// Both spellings stay asserted so either regression class is caught: the
	// name-only status style and the single-span icon+name style (issue #44
	// intake memo).
	if strings.Contains(view, gitStatusStyle(browser.GitStatusModified).Render(" clean")) {
		t.Fatal("clean row name unexpectedly uses a Git status style")
	}
	if strings.Contains(view, gitStatusStyle(browser.GitStatusModified).Render(iconForTestName("clean")+" clean")) {
		t.Fatal("clean row icon+name unexpectedly share one Git status style")
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

func TestGitStatusLettersFollowIssueDecisions(t *testing.T) {
	tests := []struct {
		name   string
		status browser.GitStatus
		want   string
	}{
		{name: "modified", status: browser.GitStatusModified, want: "M"},
		{name: "added", status: browser.GitStatusAdded, want: "A"},
		{name: "untracked is the new-file question mark", status: browser.GitStatusUntracked, want: "?"},
		{name: "unmerged is the conflict U", status: browser.GitStatusUnmerged, want: "U"},
		{name: "deleted", status: browser.GitStatusDeleted, want: "D"},
		{name: "none", status: browser.GitStatusNone, want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gitStatusLetter(test.status); got != test.want {
				t.Fatalf("gitStatusLetter(%v) = %q, want %q", test.status, got, test.want)
			}
		})
	}
}

func TestRenderTreeRowPaintsTheLetterColumnForRepositories(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.go", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	m := Model{tree: model.tree, selected: 9}
	tests := []struct {
		name   string
		status browser.GitStatus
		letter string
		color  string
	}{
		{name: "modified M", status: browser.GitStatusModified, letter: "M", color: "38;5;220"},
		{name: "added A", status: browser.GitStatusAdded, letter: "A", color: "38;5;42"},
		{name: "untracked question mark", status: browser.GitStatusUntracked, letter: "?", color: "38;5;42"},
		{name: "unmerged U", status: browser.GitStatusUnmerged, letter: "U", color: "38;5;203"},
		{name: "deleted D", status: browser.GitStatusDeleted, letter: "D", color: "38;5;203"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := m.renderTreeRow(2, "", "icon", "file.go", test.status, false, 20)
			plain := ansi.Strip(row)
			if !strings.HasSuffix(plain, test.letter) {
				t.Fatalf("row = %q, want letter %q as the final cell", plain, test.letter)
			}
			if !strings.Contains(row, "\x1b["+test.color+"m"+test.letter) {
				t.Fatalf("row = %q, want %s painted on %q", plain, test.color, test.letter)
			}
		})
	}

	// The reserved column stays blank on statusless rows so every line keeps
	// the same truncate width.
	row := m.renderTreeRow(2, "", "icon", "clean.go", browser.GitStatusNone, false, 20)
	plain := ansi.Strip(row)
	if strings.HasSuffix(plain, "M") || strings.HasSuffix(plain, "?") {
		t.Fatalf("statusless row = %q, want blank letter cell", plain)
	}
}

func TestRenderTreeRowGreysOutIgnoredRows(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "ignored.log", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	m := Model{tree: model.tree, selected: 9}
	row := m.renderTreeRow(2, "", "icon", "ignored.log", browser.GitStatusNone, true, 20)
	if got := strings.Count(row, "38;5;245"); got < 2 {
		t.Fatalf("ignored row = %q, want grey icon and name (245) in both spans", row)
	}
	if strings.Contains(row, "38;5;220") {
		t.Fatalf("ignored row = %q, must not carry a status color", row)
	}

	// The selected-row background still applies on top of the grey.
	selected := Model{tree: model.tree, selected: 0}
	row = selected.renderTreeRow(0, "  ", "icon", "ignored.log", browser.GitStatusNone, true, 20)
	if !strings.Contains(row, "48;5;238") {
		t.Fatalf("selected ignored row = %q, want selection background", row)
	}

	row = m.renderTreeRow(2, "", "icon", "normal.go", browser.GitStatusNone, false, 20)
	if strings.Contains(row, "38;5;245") {
		t.Fatalf("clean row = %q, must not be grey", row)
	}
}

func TestViewEnablesFocusReporting(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	if view := model.View(); !view.ReportFocus {
		t.Fatal("View() must enable focus reporting so FocusMsg reaches Update")
	}
	var nilModel *Model
	if view := nilModel.View(); !view.ReportFocus {
		t.Fatal("nil View() must enable focus reporting too")
	}
}

func TestFocusReturnRefreshesTreeWithoutToast(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	statusCalls := fake.statusCalls

	if _, cmd := model.Update(tea.FocusMsg{}); cmd == nil {
		t.Fatal("FocusMsg returned nil command")
	} else {
		switch message := cmd().(type) {
		case browser.LoadResult:
			model.Update(message)
		case tea.BatchMsg:
			for _, reloadCmd := range message {
				result, ok := reloadCmd().(browser.LoadResult)
				if !ok {
					t.Fatalf("focus reload message = %T, want browser.LoadResult", reloadCmd())
				}
				model.Update(result)
			}
		default:
			t.Fatalf("FocusMsg command message = %T, want LoadResult or tea.BatchMsg", message)
		}
	}
	if fake.statusCalls != statusCalls+1 {
		t.Fatalf("Git status calls after focus = %d, want %d", fake.statusCalls, statusCalls+1)
	}
	if model.toast != "" {
		t.Fatalf("focus refresh showed toast %q, want quiet refresh", model.toast)
	}

	fresh := NewModel(root, "", fake)
	if _, cmd := fresh.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("FocusMsg before any load returned a command")
	}
}

func TestTreeContentWidthReservesLetterColumnPerRepository(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})

	before := NewModel(root, "", fake)
	before.Update(teaWindowSize(40, 10))
	if got := before.treeContentWidth(); got != 40-1-1 {
		t.Fatalf("pre-load content width = %d, want %d", got, 40-1-1)
	}

	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(40, 10))
	if got := model.treeContentWidth(); got != 40-1-1-2 {
		t.Fatalf("git content width = %d, want %d (gap + letter reserved)", got, 40-1-1-2)
	}

	errFake := newStatusFileSystem()
	errFake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	errFake.statusErr = errors.New("not a git worktree")
	errModel := NewModel(root, "", errFake)
	completeInitialLoad(t, errModel)
	errModel.Update(teaWindowSize(40, 10))
	if got := errModel.treeContentWidth(); got != 40-1-1 {
		t.Fatalf("non-Git content width = %d, want %d (no reservation)", got, 40-1-1)
	}
}

func TestViewAlignsLettersInOneColumnBeforeTheScrollbar(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "clean.go", Mode: 0},
		{Name: "dir", Mode: fs.ModeDir},
	})
	fake.set(filepath.Join(root, "dir"), []filesystem.Entry{{Name: "inner.log", Mode: 0}})
	fake.statuses = []filesystem.GitStatusEntry{
		{Path: "clean.go", Status: filesystem.GitStatusUntracked},
		{Path: "dir", Status: filesystem.GitStatusModified},
	}
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(teaWindowSize(40, 8))

	var column int
	scrolled := 0
	for _, line := range strings.Split(ansi.Strip(model.View().Content), "\n") {
		if !strings.HasSuffix(line, scrollbarTrackGlyph) && !strings.HasSuffix(line, scrollbarThumbGlyph) {
			continue
		}
		scrolled++
		pos := lipgloss.Width(line) - 1
		if column == 0 {
			column = pos
		} else if pos != column {
			t.Fatalf("letter column drifted: line ends at %d, want %d (%q)", pos, column, line)
		}
		runes := []rune(line)
		letter := runes[len(runes)-2]
		if letter != ' ' && letter != 'M' && letter != '?' && letter != 'A' && letter != 'U' && letter != 'D' {
			t.Fatalf("letter cell = %q, want M/A/?/U/D or blank", letter)
		}
	}
	if scrolled < 2 {
		t.Fatalf("scrollable lines = %d, want at least 2", scrolled)
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
	directories   map[string][]filesystem.Entry
	statuses      []filesystem.GitStatusEntry
	statusCalls   int
	statusErr     error
	ignoreMatches []string
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
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return append([]filesystem.GitStatusEntry(nil), f.statuses...), nil
}

func (f *statusFileSystem) ReadGitIgnore(string, []string) ([]string, error) {
	return append([]string(nil), f.ignoreMatches...), nil
}
