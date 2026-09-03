package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestFindNameMatchRanges(t *testing.T) {
	tests := []struct {
		name  string
		value string
		query string
		want  []findMatchRange
	}{
		{
			name:  "case-insensitive partial matches",
			value: "BaNaNa",
			query: "aN",
			want:  []findMatchRange{{start: 1, end: 3}, {start: 3, end: 5}},
		},
		{
			name:  "overlapping matches",
			value: "aaaa",
			query: "aa",
			want:  []findMatchRange{{start: 0, end: 2}, {start: 1, end: 3}, {start: 2, end: 4}},
		},
		{
			name:  "CJK names",
			value: "日本語日本",
			query: "本",
			want:  []findMatchRange{{start: 3, end: 6}, {start: 12, end: 15}},
		},
		{
			name:  "simple Unicode fold",
			value: "Kettle",
			query: "k",
			want:  []findMatchRange{{start: 0, end: len("K")}},
		},
		{
			name:  "empty query",
			value: "filename",
			query: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := findNameMatchRanges(test.value, test.query); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("findNameMatchRanges(%q, %q) = %#v, want %#v", test.value, test.query, got, test.want)
			}
		})
	}
}

func TestFindVisibleRowTraversalExcludesRootAndWraps(t *testing.T) {
	root := filepath.Join(t.TempDir(), "needle-root")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "needle-one"},
		{Name: "needle-two"},
	})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	first := findVisibleIndex(t, model, "needle-one")
	second := findVisibleIndex(t, model, "needle-two")
	if got := firstMatchingVisibleRow(model.visibleRows, "needle"); got != first {
		t.Fatalf("firstMatchingVisibleRow() = %d, want child row %d", got, first)
	}
	if got := nextMatchingVisibleRow(model.visibleRows, "needle", first); got != second {
		t.Fatalf("nextMatchingVisibleRow() = %d, want %d", got, second)
	}
	if got := nextMatchingVisibleRow(model.visibleRows, "needle", second); got != first {
		t.Fatalf("wrapped nextMatchingVisibleRow() = %d, want %d", got, first)
	}
	if got := previousMatchingVisibleRow(model.visibleRows, "needle", first); got != second {
		t.Fatalf("wrapped previousMatchingVisibleRow() = %d, want %d", got, second)
	}

	singleRoot := t.TempDir()
	singleFake := newFakeFileSystem()
	singleFake.set(singleRoot, []filesystem.Entry{{Name: "needle-only"}})
	single := NewModel(singleRoot, "", singleFake)
	completeInitialLoad(t, single)
	only := findVisibleIndex(t, single, "needle-only")
	if got := nextMatchingVisibleRow(single.visibleRows, "needle", only); got != only {
		t.Fatalf("single-match next = %d, want %d", got, only)
	}
	if got := previousMatchingVisibleRow(single.visibleRows, "needle", only); got != only {
		t.Fatalf("single-match previous = %d, want %d", got, only)
	}
}

func TestFindIncrementallySelectsTheFirstVisibleMatch(t *testing.T) {
	model, fake := newFindModel(t, "alpha", "target-first", "target-second")
	model.selected = findVisibleIndex(t, model, "alpha")
	calls := len(fake.calls())

	model.UpdateKey(findTextKey("/"))
	if !model.findActive || model.findQuery != "" || model.findAnchorPath != model.selectedNode().Path() {
		t.Fatalf("find start state = active %v, query %q, anchor %q", model.findActive, model.findQuery, model.findAnchorPath)
	}

	model.UpdateKey(findTextKey("t"))
	first := firstMatchingVisibleRow(model.visibleRows, "t")
	if model.selected != first {
		t.Fatalf("first incremental match selected = %d, want %d", model.selected, first)
	}
	model.UpdateKey(findTextKey("arget"))
	first = firstMatchingVisibleRow(model.visibleRows, "target")
	if model.selected != first {
		t.Fatalf("updated query selected = %d, want %d", model.selected, first)
	}
	second := nextMatchingVisibleRow(model.visibleRows, "target", first)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.selected != second {
		t.Fatalf("down selected = %d, want %d", model.selected, second)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.selected != first {
		t.Fatalf("wrapped down selected = %d, want %d", model.selected, first)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if model.selected != second {
		t.Fatalf("wrapped up selected = %d, want %d", model.selected, second)
	}

	selected := model.selected
	model.UpdateKey(findTextKey("z"))
	if model.selected != selected {
		t.Fatalf("no-match query changed selected = %d, want %d", model.selected, selected)
	}
	if got := model.findPrompt(); got != "find: targetz (no match)" {
		t.Fatalf("findPrompt() = %q, want no-match prompt", got)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if model.findQuery != "target" || model.selected != first {
		t.Fatalf("backspace state = query %q, selected %d; want target and %d", model.findQuery, model.selected, first)
	}
	if got := len(fake.calls()); got != calls {
		t.Fatalf("find input filesystem calls = %d, want %d", got, calls)
	}

	model.Update(tea.WindowSizeMsg{Width: 20, Height: 7})
	if !model.findActive || model.findQuery != "target" {
		t.Fatalf("resize changed find state = active %v, query %q", model.findActive, model.findQuery)
	}
}

func TestFindKeepsAnIncrementalMatchVisible(t *testing.T) {
	names := make([]string, 0, 10)
	for index := 0; index < 9; index++ {
		names = append(names, "entry-"+string(rune('a'+index)))
	}
	names = append(names, "needle")
	model, _ := newFindModel(t, names...)
	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("needle"))

	if got := model.selectedNode().Name(); got != "needle" {
		t.Fatalf("incremental selection = %q, want needle", got)
	}
	if model.selected-stickyRootHeight < model.offset || model.selected-stickyRootHeight >= model.offset+model.scrollableViewportHeight() {
		t.Fatalf("incremental selection %d is outside offset %d viewport %d", model.selected, model.offset, model.scrollableViewportHeight())
	}
}

func TestFindConsumesCommandKeysAndBackspaceDeletesOneGrapheme(t *testing.T) {
	model, fake := newFindModel(t, "alpha", "beta")
	model.selected = findVisibleIndex(t, model, "alpha")
	calls := len(fake.calls())
	selected := model.selected
	model.UpdateKey(findTextKey("/"))

	for _, key := range []tea.KeyPressMsg{
		findTextKey("j"),
		findTextKey("k"),
		findTextKey("q"),
		findTextKey("r"),
		findTextKey(" "),
		{Text: "N", Mod: tea.ModShift},
		findTextKey("/"),
	} {
		if command := model.UpdateKey(key); command != nil {
			t.Fatalf("find key %q returned command %v, want nil", key.Text, command)
		}
	}
	if got := model.findQuery; got != "jkqr N/" {
		t.Fatalf("find query = %q, want command keys as text", got)
	}
	if model.selected != selected {
		t.Fatalf("find command keys changed selected = %d, want %d", model.selected, selected)
	}
	if got := len(fake.calls()); got != calls {
		t.Fatalf("find command keys filesystem calls = %d, want %d", got, calls)
	}

	model.cancelFind()
	model.UpdateKey(findTextKey("/"))
	if command := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyBackspace}); command != nil {
		t.Fatalf("empty-query backspace returned command %v, want nil", command)
	}
	if model.findQuery != "" {
		t.Fatalf("empty-query backspace changed query to %q", model.findQuery)
	}
	model.UpdateKey(findTextKey("e\u0301"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if model.findQuery != "" {
		t.Fatalf("grapheme backspace left query %q", model.findQuery)
	}

	if command := model.UpdateKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); command == nil {
		t.Fatal("ctrl+c in find mode returned nil command")
	} else if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command = %T, want tea.QuitMsg", command())
	}
}

func TestFindEnterEscapeAndMouseClickLifecycle(t *testing.T) {
	model, _ := newFindModel(t, "anchor", "match", "other")
	anchor := findVisibleIndex(t, model, "anchor")
	match := findVisibleIndex(t, model, "match")
	other := findVisibleIndex(t, model, "other")
	model.selected = anchor

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("match"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.findActive || model.findQuery != "" || model.lastQuery != "match" || model.findHighlightQuery != "match" || model.selected != match {
		t.Fatalf("enter state = active %v query %q last %q highlight %q selected %d", model.findActive, model.findQuery, model.lastQuery, model.findHighlightQuery, model.selected)
	}

	model.UpdateKey(findTextKey("/"))
	if model.findHighlightQuery != "match" {
		t.Fatalf("find start changed existing highlight to %q", model.findHighlightQuery)
	}
	model.UpdateKey(findTextKey("other"))
	if model.selected != other {
		t.Fatalf("new query selected = %d, want %d", model.selected, other)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if model.findActive || model.findQuery != "" || model.lastQuery != "match" || model.findHighlightQuery != "" || model.selected != match {
		t.Fatalf("escape state = active %v query %q last %q highlight %q selected %d", model.findActive, model.findQuery, model.lastQuery, model.findHighlightQuery, model.selected)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: 'n'})
	if model.findHighlightQuery != "" {
		t.Fatalf("n re-enabled a cancelled highlight %q", model.findHighlightQuery)
	}

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.lastQuery != "match" || model.findHighlightQuery != "" || model.selected != match {
		t.Fatalf("empty enter state = last %q highlight %q selected %d", model.lastQuery, model.findHighlightQuery, model.selected)
	}

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("other"))
	clickY := model.treeStartY() + stickyRootHeight
	clicked, ok := model.rowIndexAtY(clickY)
	if !ok {
		t.Fatalf("click coordinate y=%d does not map to a visible row", clickY)
	}
	if command := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: clickY, Button: tea.MouseLeft}); command != nil {
		t.Fatalf("file click returned command %v, want nil", command)
	}
	if model.findActive || model.findQuery != "" || model.lastQuery != "other" || model.findHighlightQuery != "other" || model.selected != clicked {
		t.Fatalf("mouse finish state = active %v query %q last %q highlight %q selected %d", model.findActive, model.findQuery, model.lastQuery, model.findHighlightQuery, model.selected)
	}

	selected := model.selected
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if model.lastQuery != "" || model.findHighlightQuery != "" || model.selected != selected {
		t.Fatalf("outside escape state = last %q highlight %q selected %d", model.lastQuery, model.findHighlightQuery, model.selected)
	}
}

func TestFindEmptyMouseClickPreservesTheCompletedSearch(t *testing.T) {
	model, _ := newFindModel(t, "match", "other")
	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("match"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	model.UpdateKey(findTextKey("/"))

	clickY := model.treeStartY() + stickyRootHeight
	if command := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: clickY, Button: tea.MouseLeft}); command != nil {
		t.Fatalf("file click returned command %v, want nil", command)
	}
	if model.findActive || model.lastQuery != "match" || model.findHighlightQuery != "match" {
		t.Fatalf("empty mouse finish state = active %v last %q highlight %q", model.findActive, model.lastQuery, model.findHighlightQuery)
	}
}

func TestFindMouseFileClickConfirmsWithoutOpeningPreview(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt"}})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("file"))
	click := tea.MouseClickMsg{X: 0, Y: model.treeStartY() + stickyRootHeight, Button: tea.MouseLeft}

	if command := model.UpdateMouse(click); command != nil {
		t.Fatalf("find-mode file click returned command %v, want nil", command)
	}
	if model.findActive || model.findQuery != "" || model.lastQuery != "file" || model.findHighlightQuery != "file" {
		t.Fatalf("find-mode click state = active %v query %q last %q highlight %q", model.findActive, model.findQuery, model.lastQuery, model.findHighlightQuery)
	}
	if node := model.selectedNode(); node == nil || node.Name() != "file.txt" {
		t.Fatalf("find-mode click selected node = %#v, want file.txt", node)
	}
	if len(client.openFiles) != 0 || len(client.closed) != 0 || len(client.getCalls) != 0 || len(client.listed) != 0 {
		t.Fatalf("find-mode click used preview client: opens %v closes %v gets %v lists %v", client.openFiles, client.closed, client.getCalls, client.listed)
	}

	command := model.UpdateMouse(click)
	if command == nil {
		t.Fatal("post-find file click returned nil command")
	}
	model.Update(command().(previewResultMsg))
	if got, want := client.openFiles, []string{filePath}; !reflect.DeepEqual(got, want) {
		t.Fatalf("post-find opened files = %v, want %v", got, want)
	}
}

func TestFindMouseDirectoryClickConfirmsBeforeExpanding(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "directory", Mode: fs.ModeDir},
		{Name: "match"},
	})
	fake.set(directoryPath, []filesystem.Entry{{Name: "child"}})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})

	directoryRow := findVisibleIndex(t, model, "directory")
	matchRow := findVisibleIndex(t, model, "match")
	directory := model.visibleRows[directoryRow].Node
	if directory == nil || !directory.IsDirectory() || directory.Expanded() {
		t.Fatalf("directory before find click = %#v, want collapsed directory", directory)
	}

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("match"))
	if model.selected != matchRow {
		t.Fatalf("find selection = %d, want match row %d", model.selected, matchRow)
	}
	clickY := model.treeStartY() + stickyRootHeight
	if clicked, ok := model.rowIndexAtY(clickY); !ok || clicked != directoryRow {
		t.Fatalf("click coordinate y=%d maps to row %d, %v; want directory row %d", clickY, clicked, ok, directoryRow)
	}
	click := tea.MouseClickMsg{X: 0, Y: clickY, Button: tea.MouseLeft}

	if command := model.UpdateMouse(click); command != nil {
		t.Fatalf("find-mode directory click returned command %v, want nil", command)
	}
	if model.findActive || model.selected != directoryRow {
		t.Fatalf("find-mode directory click state = active %v selected %d, want inactive and row %d", model.findActive, model.selected, directoryRow)
	}
	if directory.Expanded() || directory.Loading() {
		t.Fatalf("find-mode directory click changed directory state: expanded %v loading %v", directory.Expanded(), directory.Loading())
	}
	if len(client.openFiles) != 0 || len(client.closed) != 0 || len(client.getCalls) != 0 || len(client.listed) != 0 {
		t.Fatalf("find-mode directory click used preview client: opens %v closes %v gets %v lists %v", client.openFiles, client.closed, client.getCalls, client.listed)
	}

	command := model.UpdateMouse(click)
	if command == nil {
		t.Fatal("post-find directory click returned nil command, want async load")
	}
	if !directory.Expanded() || !directory.Loading() {
		t.Fatalf("post-find directory click state = expanded %v loading %v, want true true", directory.Expanded(), directory.Loading())
	}
	model.Update(loadResultFromCommand(t, command))
	if !directory.Loaded() || directory.Loading() {
		t.Fatalf("post-find directory load state = loaded %v loading %v, want true false", directory.Loaded(), directory.Loading())
	}
}

func TestFindPreviewEnterConsumesUnderlineAndKeepsRepeatNavigation(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "match-one"}, {Name: "match-two"}})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("match"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	first := findVisibleIndex(t, model, "match-one")
	if model.selected != first || model.lastQuery != "match" || model.findHighlightQuery != "match" {
		t.Fatalf("find confirm state = selected %d last %q highlight %q; want %d match match", model.selected, model.lastQuery, model.findHighlightQuery, first)
	}

	command := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("file enter returned nil command")
	}
	if model.findHighlightQuery != "" {
		t.Fatalf("preview enter did not consume underline synchronously: %q", model.findHighlightQuery)
	}
	model.Update(command().(previewResultMsg))
	if model.findHighlightQuery != "" {
		t.Fatalf("preview enter left underline %q, want cleared", model.findHighlightQuery)
	}
	if model.lastQuery != "match" {
		t.Fatalf("preview enter changed lastQuery to %q, want match kept", model.lastQuery)
	}
	if got, want := client.openFiles, []string{filepath.Join(root, "match-one")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preview enter opened %v, want %v", got, want)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: 'n'})
	if got := model.selectedNode().Name(); got != "match-two" {
		t.Fatalf("n after preview selected %q, want next match match-two", got)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: 'N'})
	if got := model.selectedNode().Name(); got != "match-one" {
		t.Fatalf("N after preview selected %q, want previous match match-one", got)
	}
	if model.findHighlightQuery != "" {
		t.Fatalf("repeat navigation re-enabled underline %q", model.findHighlightQuery)
	}
}

func TestFindPreviewClickConsumesUnderlineAndKeepsLastQuery(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt"}})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("file"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.findHighlightQuery != "file" || model.lastQuery != "file" {
		t.Fatalf("find confirm state = highlight %q last %q, want file file", model.findHighlightQuery, model.lastQuery)
	}

	click := tea.MouseClickMsg{X: 0, Y: model.treeStartY() + stickyRootHeight, Button: tea.MouseLeft}
	command := model.UpdateMouse(click)
	if command == nil {
		t.Fatal("file click returned nil command")
	}
	if model.findHighlightQuery != "" {
		t.Fatalf("preview click did not consume underline synchronously: %q", model.findHighlightQuery)
	}
	model.Update(command().(previewResultMsg))
	if model.findHighlightQuery != "" {
		t.Fatalf("preview click left underline %q, want cleared", model.findHighlightQuery)
	}
	if model.lastQuery != "file" {
		t.Fatalf("preview click changed lastQuery to %q, want file kept", model.lastQuery)
	}
	if got, want := client.openFiles, []string{filepath.Join(root, "file.txt")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preview click opened %v, want %v", got, want)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: 'n'})
	if got := model.selectedNode().Name(); got != "file.txt" {
		t.Fatalf("n after preview click selected %q, want the only match file.txt", got)
	}
}

func TestFindActivationWithoutPreviewConfigKeepsUnderline(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt"}})

	variants := []struct {
		name  string
		build func() *Model
	}{
		{name: "no config", build: func() *Model { return NewModel(root, "", fake) }},
		{name: "missing target pane", build: func() *Model {
			return NewModelWithPreview(root, "", PreviewConfig{Client: &stubPreviewClient{}, WorkspaceID: "wY"}, fake)
		}},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			model := variant.build()
			completeInitialLoad(t, model)
			model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
			model.UpdateKey(findTextKey("/"))
			model.UpdateKey(findTextKey("file"))
			model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})

			if command := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}); command != nil {
				t.Fatalf("file enter returned command %v, want nil no-op", command)
			}
			if model.findHighlightQuery != "file" || model.lastQuery != "file" {
				t.Fatalf("enter state = highlight %q last %q, want underline kept", model.findHighlightQuery, model.lastQuery)
			}

			click := tea.MouseClickMsg{X: 0, Y: model.treeStartY() + stickyRootHeight, Button: tea.MouseLeft}
			if command := model.UpdateMouse(click); command != nil {
				t.Fatalf("file click returned command %v, want nil no-op", command)
			}
			if model.findHighlightQuery != "file" {
				t.Fatalf("click cleared underline to %q, want kept", model.findHighlightQuery)
			}
		})
	}
}

func TestFindDirectoryActivationKeepsUnderline(t *testing.T) {
	root := t.TempDir()
	directoryPath := filepath.Join(root, "directory")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "directory", Mode: fs.ModeDir},
		{Name: "match"},
	})
	fake.set(directoryPath, []filesystem.Entry{{Name: "child"}})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("match"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	directory := findVisibleIndex(t, model, "directory")
	match := findVisibleIndex(t, model, "match")
	if model.selected != match {
		t.Fatalf("find confirm selected = %d, want match row %d", model.selected, match)
	}
	model.selected = directory

	if command := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}); command != nil {
		t.Fatalf("directory enter returned command %v, want nil", command)
	}
	if model.findHighlightQuery != "match" || model.lastQuery != "match" {
		t.Fatalf("directory enter state = highlight %q last %q, want underline kept", model.findHighlightQuery, model.lastQuery)
	}
	if len(client.openFiles) != 0 || len(client.closed) != 0 || len(client.getCalls) != 0 || len(client.listed) != 0 {
		t.Fatalf("directory enter used preview client: opens %v closes %v gets %v lists %v", client.openFiles, client.closed, client.getCalls, client.listed)
	}

	clickY := model.treeStartY() + stickyRootHeight + (directory - stickyRootHeight)
	click := tea.MouseClickMsg{X: 0, Y: clickY, Button: tea.MouseLeft}
	command := model.UpdateMouse(click)
	if command == nil {
		t.Fatal("directory click returned nil command, want async load")
	}
	message := command()
	result, ok := message.(browser.LoadResult)
	if !ok {
		t.Fatalf("directory click command message = %T, want browser.LoadResult", message)
	}
	model.Update(result)
	if model.findHighlightQuery != "match" || model.lastQuery != "match" {
		t.Fatalf("directory click state = highlight %q last %q, want underline kept", model.findHighlightQuery, model.lastQuery)
	}
	directoryNode := model.visibleRows[directory].Node
	if directoryNode == nil || !directoryNode.Expanded() || !directoryNode.Loaded() {
		t.Fatalf("directory click did not expand and load the directory: expanded %v loaded %v", directoryNode != nil && directoryNode.Expanded(), directoryNode != nil && directoryNode.Loaded())
	}
	if len(client.openFiles) != 0 {
		t.Fatalf("directory click opened %v, want none", client.openFiles)
	}
}

func TestFindPreviewActivationFailureStillConsumesUnderline(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt"}})
	client := &stubPreviewClient{openErr: errors.New("herdr CLI unavailable")}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("file"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.findHighlightQuery != "file" {
		t.Fatalf("find confirm state highlight = %q, want file", model.findHighlightQuery)
	}

	command := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("file enter returned nil command")
	}
	if model.findHighlightQuery != "" {
		t.Fatalf("failed preview attempt did not consume underline synchronously: %q", model.findHighlightQuery)
	}
	model.Update(command().(previewResultMsg))
	if model.findHighlightQuery != "" || model.lastQuery != "file" {
		t.Fatalf("failed preview state = highlight %q last %q, want cleared underline with kept lastQuery", model.findHighlightQuery, model.lastQuery)
	}
	if got, want := client.openFiles, []string{filepath.Join(root, "file.txt")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("failed preview opened %v, want attempted file %v", got, want)
	}
}

func TestFindEscapeFallsBackToTheLastVisibleRowWhenAnchorDisappears(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "anchor"},
		{Name: "needle-one"},
		{Name: "needle-two"},
	})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model.selected = findVisibleIndex(t, model, "anchor")

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("needle"))
	if got := model.selectedNode().Name(); got != "needle-one" {
		t.Fatalf("incremental selection = %q, want needle-one", got)
	}

	fake.set(root, []filesystem.Entry{
		{Name: "needle-one"},
		{Name: "needle-two"},
	})
	reload := model.reloadTree(false)
	if reload == nil {
		t.Fatal("reloadTree() returned nil command")
	}
	model.Update(loadResultFromCommand(t, reload))
	if !model.findActive || model.findAnchorPath == "" {
		t.Fatalf("reload changed find state = active %v anchor %q", model.findActive, model.findAnchorPath)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if got := model.selectedNode().Name(); got != "needle-two" {
		t.Fatalf("missing-anchor fallback selected %q, want final visible row needle-two", got)
	}
}

func TestFindSearchesOnlyVisibleRows(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "folder")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "folder", Mode: fs.ModeDir}})
	fake.set(directory, []filesystem.Entry{{Name: "needle-child"}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	folder := findVisibleIndex(t, model, "folder")
	model.selected = folder
	load := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if load == nil {
		t.Fatal("expand returned nil command")
	}
	model.Update(loadResultFromCommand(t, load))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyLeft})

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("needle"))
	if model.selected != folder || !strings.HasSuffix(model.findPrompt(), "(no match)") {
		t.Fatalf("closed-directory find state = selected %d prompt %q", model.selected, model.findPrompt())
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEsc})

	if command := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}); command != nil {
		t.Fatalf("loaded directory re-expand command = %v, want nil", command)
	}
	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("needle"))
	if got := model.selectedNode().Name(); got != "needle-child" {
		t.Fatalf("expanded-directory find selected %q, want needle-child", got)
	}
}

func TestFindRepeatUsesLastQueryAndSearchModeConsumesN(t *testing.T) {
	model, _ := newFindModel(t, "match-one", "match-two", "plain")
	model.UpdateKey(tea.KeyPressMsg{Code: '/'})
	model.UpdateKey(findTextKey("match"))
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	first := findVisibleIndex(t, model, "match-one")
	second := findVisibleIndex(t, model, "match-two")
	if model.selected != first {
		t.Fatalf("enter selected = %d, want %d", model.selected, first)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: 'n'})
	if model.selected != second {
		t.Fatalf("n selected = %d, want %d", model.selected, second)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: 'N'})
	if model.selected != first {
		t.Fatalf("N selected = %d, want %d", model.selected, first)
	}

	model.clearFindHighlights()
	selected := model.selected
	model.UpdateKey(tea.KeyPressMsg{Code: 'n'})
	if model.selected != selected {
		t.Fatalf("n without last query selected = %d, want %d", model.selected, selected)
	}

	model.UpdateKey(findTextKey("/"))
	model.UpdateKey(findTextKey("n"))
	if !model.findActive || model.findQuery != "n" || model.selected != selected {
		t.Fatalf("n in find mode = active %v query %q selected %d", model.findActive, model.findQuery, model.selected)
	}
}

func TestFindHighlightRenderingComposesStylesAndTruncates(t *testing.T) {
	root := t.TempDir()
	fake := newStatusFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.go"}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	selected := Model{
		tree:               model.tree,
		selected:           1,
		findHighlightQuery: "go",
	}
	status := browser.GitStatusModified
	background := selectedStyle(false).GetBackground()
	nameStyle := gitStatusStyle(status).Background(background)
	row := selected.renderTreeRow(1, "prefix ", "icon", " file.go.go", status, false, 80)
	if got := strings.Count(row, nameStyle.Underline(true).Render("go")); got != 2 {
		t.Fatalf("highlighted name spans = %d, want 2: %q", got, row)
	}
	if !strings.Contains(row, iconStyle("icon").Background(background).Render("icon")) {
		t.Fatalf("icon lost its non-underlined span: %q", row)
	}
	if !strings.Contains(row, gitStatusStyle(status).Background(background).Render("M")) {
		t.Fatalf("Git letter lost its non-underlined span: %q", row)
	}
	if !strings.Contains(row, "38;5;220") || !strings.Contains(row, "48;5;238") {
		t.Fatalf("selected Git-status match lost foreground or background: %q", row)
	}

	ignored := Model{findHighlightQuery: "go"}
	ignoredRow := ignored.renderTreeRow(1, "", "icon", " ignored.go", browser.GitStatusNone, true, 80)
	if !strings.Contains(ignoredRow, ignoredRowStyle.Underline(true).Render("go")) {
		t.Fatalf("ignored match lost grey underline composition: %q", ignoredRow)
	}

	rootRow := selected.renderTreeRow(0, "", "icon", " root.go", browser.GitStatusNone, false, 80)
	if strings.Contains(rootRow, lipgloss.NewStyle().Inline(true).Underline(true).Render("go")) {
		t.Fatalf("root row is highlighted: %q", rootRow)
	}

	truncated := Model{selected: 1, findHighlightQuery: "match"}
	for width := 0; width <= 12; width++ {
		got := truncated.renderTreeRow(1, "  ", "icon", " match-at-the-boundary", browser.GitStatusNone, false, width)
		if actual := lipgloss.Width(got); actual > width {
			t.Fatalf("width %d rendered width = %d, want <= %d: %q", width, actual, width, got)
		}
		plain := ansi.Strip(got)
		if strings.ContainsRune(plain, '\x1b') || !utf8.ValidString(plain) {
			t.Fatalf("width %d has malformed ANSI output: %q", width, got)
		}
	}
}

func TestFindPromptOverridesToastAndStatus(t *testing.T) {
	model, _ := newFindModel(t, "file")
	model.findActive = true
	model.findQuery = "missing"
	model.toast = "Reloaded"
	model.status = loadingStatus

	footer := strings.TrimRight(ansi.Strip(model.renderFooter()), " ")
	if footer != " find: missing (no match)" {
		t.Fatalf("find footer = %q, want prompt", footer)
	}
}

func newFindModel(t *testing.T, names ...string) (*Model, *fakeFileSystem) {
	t.Helper()
	root := t.TempDir()
	fake := newFakeFileSystem()
	entries := make([]filesystem.Entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, filesystem.Entry{Name: name})
	}
	fake.set(root, entries)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	return model, fake
}

func findTextKey(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: text}
}

func findVisibleIndex(t testing.TB, model *Model, name string) int {
	t.Helper()
	for index, row := range model.visibleRows {
		if row.Node != nil && row.Node.Name() == name {
			return index
		}
	}
	t.Fatalf("visible row %q not found", name)
	return -1
}
