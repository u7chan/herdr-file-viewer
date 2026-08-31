package app

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPreviewSelectionNormalizesInReverseOrder(t *testing.T) {
	selection := previewSelection{
		anchor: previewPosition{line: 3, col: 2},
		focus:  previewPosition{line: 1, col: 4},
	}
	want := previewSelection{
		anchor: previewPosition{line: 1, col: 4},
		focus:  previewPosition{line: 3, col: 2},
	}
	if got := selection.normalized(); got != want {
		t.Fatalf("normalized selection = %#v, want %#v", got, want)
	}
	if selection.empty() {
		t.Fatal("non-empty selection reported as empty")
	}
}

func TestPreviewPositionForMouseMapsGutterEndOffsetAndWrappedRows(t *testing.T) {
	display := []previewLine{
		{text: "abcd", origin: 0, col: 0},
		{text: "efgh", origin: 0, col: 4},
		{text: "ij", origin: 0, col: 8},
		{text: "second", origin: 1, col: 0},
		{text: "marker", origin: -1},
	}
	const (
		bodyStart   = 4
		bodyHeight  = 3
		contentX    = 8
		contentWide = 20
	)

	tests := []struct {
		name    string
		x, y    int
		offset  int
		xoffset int
		clampY  bool
		want    previewPosition
	}{
		{name: "gutter anchors at line start", x: contentX - 1, y: bodyStart, want: previewPosition{line: 0, col: 0}},
		{name: "line end clamps", x: contentX + 30, y: bodyStart, want: previewPosition{line: 0, col: 4}},
		{name: "horizontal offset is applied", x: contentX + 1, y: bodyStart, xoffset: 2, want: previewPosition{line: 0, col: 3}},
		{name: "wrapped continuation keeps original line", x: contentX + 1, y: bodyStart + 1, want: previewPosition{line: 0, col: 5}},
		{name: "continuation gutter anchors original line start", x: contentX - 1, y: bodyStart + 1, want: previewPosition{line: 0, col: 0}},
		{name: "viewport top clamps during drag", x: contentX, y: bodyStart - 3, clampY: true, want: previewPosition{line: 0, col: 0}},
		{name: "viewport bottom clamps during drag", x: contentX, y: bodyStart + bodyHeight + 3, clampY: true, want: previewPosition{line: 0, col: 8}},
		{name: "marker is not selectable", x: contentX, y: bodyStart + 2, offset: 2, want: previewPosition{}},
		{name: "drag over marker clamps to last text row", x: contentX, y: bodyStart + 2, offset: 2, clampY: true, want: previewPosition{line: 1, col: 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := previewPositionForMouse(display, test.offset, bodyStart, bodyHeight, test.x, test.y, contentX, contentWide, test.xoffset, test.clampY)
			if test.name == "marker is not selectable" {
				if ok {
					t.Fatalf("marker position = %#v, want no position", got)
				}
				return
			}
			if !ok || got != test.want {
				t.Fatalf("position = %#v, ok %v; want %#v", got, ok, test.want)
			}
		})
	}
}

func TestPreviewPositionForMouseClampsPastShortDisplayToLastLine(t *testing.T) {
	position, ok := previewPositionForMouse(
		[]previewLine{{text: "short", origin: 0}},
		0, 4, 4, 20, 20, 8, 20, 0, true,
	)
	if !ok || position != (previewPosition{line: 0, col: 5}) {
		t.Fatalf("short display position = %#v, ok %v; want line 0 col 5", position, ok)
	}
}

func TestPreviewSelectionSpansCoverSingleAndMultipleLines(t *testing.T) {
	line := previewLine{text: "abcdef", origin: 0}
	selection := previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 4},
	}
	if got, want := previewSelectionSpans(line, selection, false), []previewTextSpan{
		{text: "a"}, {text: "bcd", selected: true}, {text: "ef"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("single-line spans = %#v, want %#v", got, want)
	}

	selection = previewSelection{
		anchor: previewPosition{line: 1, col: 1},
		focus:  previewPosition{line: 0, col: 2},
	}
	first := previewSelectionSpans(previewLine{text: "abcd", origin: 0}, selection, false)
	second := previewSelectionSpans(previewLine{text: "efgh", origin: 1}, selection, false)
	if want := []previewTextSpan{{text: "ab"}, {text: "cd", selected: true}}; !reflect.DeepEqual(first, want) {
		t.Fatalf("first multi-line spans = %#v, want %#v", first, want)
	}
	if want := []previewTextSpan{{text: "e", selected: true}, {text: "fgh"}}; !reflect.DeepEqual(second, want) {
		t.Fatalf("last multi-line spans = %#v, want %#v", second, want)
	}
}

func TestPreviewSelectionSpansClassifyOnlyStandaloneHalfWidthSpaces(t *testing.T) {
	line := previewLine{text: "a  b\u3000\u00a0c", origin: 0}
	if got, want := previewSelectionSpans(line, previewSelection{}, true), []previewTextSpan{
		{text: "a"}, {text: "  ", space: true}, {text: "b\u3000\u00a0c"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("whitespace spans = %#v, want %#v", got, want)
	}

	if got, want := previewSelectionSpans(line, previewSelection{}, false), []previewTextSpan{{text: line.text}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("spaces-off spans = %#v, want %#v", got, want)
	}

	selection := previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 3},
	}
	if got, want := previewSelectionSpans(line, selection, true), []previewTextSpan{
		{text: "a"}, {text: "  ", space: true, selected: true}, {text: "b\u3000\u00a0c"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected whitespace spans = %#v, want %#v", got, want)
	}
}

func TestPreviewSelectionSpansClassifyExpandedTabsAsSpaces(t *testing.T) {
	line := previewTextLines([]byte("\t  "))[0]
	got := previewSelectionSpans(line, previewSelection{}, true)
	if want := []previewTextSpan{{text: "      ", space: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expanded-tab spans = %#v, want %#v", got, want)
	}
}

func TestPreviewSelectionSpansMapWrapContinuationAndMutedLines(t *testing.T) {
	display := buildDisplayLines([]previewLine{{text: "abcdefghij", number: 1, muted: true}}, true, 4)
	selection := previewSelection{
		anchor: previewPosition{line: 0, col: 3},
		focus:  previewPosition{line: 0, col: 9},
	}
	wants := [][]previewTextSpan{
		{{text: "abc"}, {text: "d", selected: true}},
		{{text: "efgh", selected: true}},
		{{text: "i", selected: true}, {text: "j"}},
	}
	for index, want := range wants {
		got := previewSelectionSpans(display[index], selection, false)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("display line %d spans = %#v, want %#v", index, got, want)
		}
		if !display[index].muted {
			t.Fatalf("display line %d lost muted state", index)
		}
	}
}

func TestPreviewSelectionSpansDoNotSplitWideGraphemes(t *testing.T) {
	line := previewLine{text: "a日b", origin: 0}
	if got, want := previewSelectionSpans(line, previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 3},
	}, false), []previewTextSpan{{text: "a"}, {text: "日", selected: true}, {text: "b"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wide-grapheme spans = %#v, want %#v", got, want)
	}
	if got := previewSelectionSpans(line, previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 2},
	}, false); !reflect.DeepEqual(got, []previewTextSpan{{text: "a日b"}}) {
		t.Fatalf("straddled wide-grapheme spans = %#v, want unselected text", got)
	}
}

func TestPreviewSelectionSpansRetainZeroWidthGraphemes(t *testing.T) {
	line := previewLine{text: "\u0301a", origin: 0}
	spans := previewSelectionSpans(line, previewSelection{}, false)
	if got := strings.Join(previewSpanTexts(spans), ""); got != line.text {
		t.Fatalf("zero-width grapheme spans = %#v, text %q; want %q", spans, got, line.text)
	}
}

func previewSpanTexts(spans []previewTextSpan) []string {
	texts := make([]string, 0, len(spans))
	for _, span := range spans {
		texts = append(texts, span.text)
	}
	return texts
}

func TestExtractSelectionJoinsOriginalLines(t *testing.T) {
	lines := []previewLine{{text: "first"}, {text: "second"}, {text: "third"}}
	selection := previewSelection{
		anchor: previewPosition{line: 2, col: 3},
		focus:  previewPosition{line: 0, col: 2},
	}
	if got, want := extractSelection(lines, selection), "rst\nsecond\nthi"; got != want {
		t.Fatalf("extractSelection() = %q, want %q", got, want)
	}
}

func TestPreviewVisibleSpansClipsWithoutSplittingGraphemes(t *testing.T) {
	spans := []previewTextSpan{{text: "a日本b", selected: true}}
	if got, want := previewVisibleSpans(spans, 1, 4), []previewTextSpan{{text: "日本", selected: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible spans = %#v, want %#v", got, want)
	}
	if got := previewVisibleSpans(spans, 2, 2); len(got) != 0 {
		t.Fatalf("straddled visible span = %#v, want no grapheme", got)
	}
}

func TestPreviewSelectionMouseLifecycleAndViewportClamping(t *testing.T) {
	reader := &fakePreviewReader{content: bytesRepeatContent(20)}
	model := NewPreviewModel("/abs/select.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	startY := model.bodyStartY()
	contentX := model.contentStartX()
	model.Update(tea.MouseClickMsg{X: contentX + 1, Y: startY, Button: tea.MouseLeft})
	if model.dragMode != previewDragSelect || model.selection.anchor != (previewPosition{line: 0, col: 1}) {
		t.Fatalf("selection press = mode %d selection %#v, want select at line 0 col 1", model.dragMode, model.selection)
	}
	model.Update(tea.MouseMotionMsg{X: contentX + 5, Y: startY, Button: tea.MouseLeft})
	if model.selection.focus != (previewPosition{line: 0, col: 5}) {
		t.Fatalf("selection motion focus = %#v, want line 0 col 5", model.selection.focus)
	}
	focus := model.selection.focus
	model.Update(tea.MouseMotionMsg{X: contentX + 20, Y: startY, Button: tea.MouseRight})
	if model.selection.focus != focus {
		t.Fatalf("right-button motion changed focus to %#v", model.selection.focus)
	}
	model.Update(tea.MouseReleaseMsg{X: contentX + 5, Y: startY, Button: tea.MouseRight})
	if model.dragMode != previewDragSelect {
		t.Fatal("right-button release ended selection mode")
	}
	model.Update(tea.MouseReleaseMsg{X: contentX + 5, Y: startY, Button: tea.MouseLeft})
	if model.dragMode != previewDragNone || model.selection.empty() {
		t.Fatalf("left-button release = mode %d selection %#v, want finished non-empty selection", model.dragMode, model.selection)
	}

	model.Update(tea.MouseClickMsg{X: contentX, Y: startY, Button: tea.MouseLeft})
	if !model.selection.empty() || model.selection.anchor != (previewPosition{line: 0, col: 0}) {
		t.Fatalf("second click = %#v, want empty anchor at line start", model.selection)
	}
	model.Update(tea.MouseMotionMsg{X: contentX, Y: startY - 10, Button: tea.MouseLeft})
	if model.selection.focus != (previewPosition{line: 0, col: 0}) {
		t.Fatalf("top out-of-viewport focus = %#v, want line start", model.selection.focus)
	}
	model.Update(tea.MouseMotionMsg{X: contentX, Y: startY + model.bodyHeight() + 10, Button: tea.MouseLeft})
	if model.selection.focus.line != model.offset+model.bodyHeight()-1 {
		t.Fatalf("bottom out-of-viewport focus = %#v, want visible last line", model.selection.focus)
	}
}

func TestPreviewSelectionClearsOnWrapResizeAndReloadButNotScroll(t *testing.T) {
	reader := &fakePreviewReader{content: bytesRepeatContent(20)}
	model := NewPreviewModel("/abs/select.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	model.selection = previewSelection{anchor: previewPosition{line: 0, col: 1}, focus: previewPosition{line: 1, col: 2}}

	before := model.selection
	model.moveVertical(1)
	if model.selection != before {
		t.Fatalf("vertical scrolling changed selection to %#v", model.selection)
	}
	model.moveHorizontal(1)
	if model.selection != before {
		t.Fatalf("horizontal scrolling changed selection to %#v", model.selection)
	}

	model.toggleWrap()
	if !model.selection.empty() {
		t.Fatalf("wrap toggle left selection %#v", model.selection)
	}
	model.selection = before
	model.Update(tea.WindowSizeMsg{Width: 39, Height: 8})
	if !model.selection.empty() {
		t.Fatalf("resize left selection %#v", model.selection)
	}
	model.selection = before
	model.Update(previewLoadMsg{file: model.file, category: previewCategoryText, lines: model.lines})
	if !model.selection.empty() {
		t.Fatalf("reload left selection %#v", model.selection)
	}
}

func TestPreviewSelectionIgnoresUnsupportedAndTruncatedRows(t *testing.T) {
	unsupportedReader := &fakePreviewReader{content: []byte("PK\x03\x04")}
	unsupported := NewPreviewModel("/abs/archive.zip", nil, "", unsupportedReader)
	unsupported.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	unsupported.Update(previewLoadResult(t, unsupported.Init()))
	unsupported.Update(tea.MouseClickMsg{X: unsupported.contentStartX(), Y: unsupported.bodyStartY(), Button: tea.MouseLeft})
	if unsupported.dragMode != previewDragNone || !unsupported.selection.empty() {
		t.Fatalf("unsupported click started selection: mode %d selection %#v", unsupported.dragMode, unsupported.selection)
	}

	truncatedReader := &fakePreviewReader{content: []byte("text"), truncated: true}
	truncated := NewPreviewModel("/abs/truncated.txt", nil, "", truncatedReader)
	truncated.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	truncated.Update(previewLoadResult(t, truncated.Init()))
	marker := len(truncated.displayLines) - 1
	truncated.Update(tea.MouseClickMsg{X: truncated.contentStartX(), Y: truncated.bodyStartY() + marker, Button: tea.MouseLeft})
	if truncated.displayLines[marker].origin != -1 || truncated.dragMode != previewDragNone || !truncated.selection.empty() {
		t.Fatalf("truncation marker click started selection: line %#v mode %d selection %#v", truncated.displayLines[marker], truncated.dragMode, truncated.selection)
	}
}

func TestPreviewSelectionRenderingUsesDistinctBackground(t *testing.T) {
	model := PreviewModel{lineCount: 1, selection: previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 3},
	}}
	rendered := model.renderContent(previewLine{text: "abcd", origin: 0}, 4)
	if !strings.Contains(rendered, "48;5;240") {
		t.Fatalf("rendered selection = %q, want background color 240", rendered)
	}
	if got := ansi.Strip(rendered); got != "abcd" {
		t.Fatalf("rendered selection stripped = %q, want original text", got)
	}
}
