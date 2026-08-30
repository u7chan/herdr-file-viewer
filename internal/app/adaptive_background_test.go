package app

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInitRequestsBackgroundColorForTreeAndPreview(t *testing.T) {
	treeFS := newFakeFileSystem()
	treeRoot := t.TempDir()
	treeFS.set(treeRoot, nil)
	tree := NewModel(treeRoot, "", treeFS)
	treeCommands := initCommandBatch(t, tree.Init())
	if len(treeCommands) != 2 {
		t.Fatalf("tree Init() command count = %d, want background request and load", len(treeCommands))
	}
	if !reflect.DeepEqual(treeCommands[0](), tea.RequestBackgroundColor()) {
		t.Fatalf("tree Init() first command = %T, want background color request", treeCommands[0]())
	}

	preview := NewPreviewModel("/abs/file.txt", nil, "", &fakePreviewReader{content: []byte("x")})
	previewCommands := initCommandBatch(t, preview.Init())
	if len(previewCommands) != 2 {
		t.Fatalf("preview Init() command count = %d, want background request and load", len(previewCommands))
	}
	if !reflect.DeepEqual(previewCommands[0](), tea.RequestBackgroundColor()) {
		t.Fatalf("preview Init() first command = %T, want background color request", previewCommands[0]())
	}
}

func TestTreeSelectionBackgroundDefaultsDarkAndAdaptsToTerminal(t *testing.T) {
	model := Model{selected: 0}

	assertContainsBackground(t, model.renderTreeRow(0, "", "icon", "file", 0, false, 20), "238")

	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	assertContainsBackground(t, model.renderTreeRow(0, "", "icon", "file", 0, false, 20), "254")

	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{A: 255}})
	assertContainsBackground(t, model.renderTreeRow(0, "", "icon", "file", 0, false, 20), "238")
}

func TestPreviewSelectionBackgroundDefaultsDarkAndAdaptsToTerminal(t *testing.T) {
	model := PreviewModel{
		lineCount: 1,
		selection: previewSelection{
			anchor: previewPosition{line: 0, col: 1},
			focus:  previewPosition{line: 0, col: 3},
		},
	}

	assertContainsBackground(t, model.renderContent(previewLine{text: "abcd", origin: 0}, 4), "240")

	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	assertContainsBackground(t, model.renderContent(previewLine{text: "abcd", origin: 0}, 4), "252")

	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{A: 255}})
	assertContainsBackground(t, model.renderContent(previewLine{text: "abcd", origin: 0}, 4), "240")
}

func assertContainsBackground(t *testing.T, rendered, value string) {
	t.Helper()
	if want := "48;5;" + value + "m"; !strings.Contains(rendered, want) {
		t.Fatalf("rendered output = %q, want background code %q", rendered, want)
	}
}

func initCommandBatch(t *testing.T, cmd tea.Cmd) tea.BatchMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Init() returned nil command")
	}
	message := cmd()
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init() message = %T, want tea.BatchMsg", message)
	}
	return batch
}
