package app

import (
	"errors"
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPreviewCategoryForClassifiesByExtensionAndSniffing(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		content  []byte
		category previewCategory
	}{
		{name: "png is an image", path: "photo.png", content: []byte("not real png"), category: previewCategoryImage},
		{name: "extension is case-insensitive", path: "PHOTO.PNG", category: previewCategoryImage},
		{name: "jpeg variant is an image", path: "photo.jpeg", content: []byte("x"), category: previewCategoryImage},
		{name: "mp4 is a video", path: "clip.mp4", content: []byte("x"), category: previewCategoryVideo},
		{name: "mp3 is audio", path: "song.mp3", content: []byte("x"), category: previewCategoryAudio},
		{name: "zip is binary", path: "bundle.zip", content: []byte("PK\x03\x04"), category: previewCategoryBinary},
		{name: "exe is binary", path: "tool.exe", content: []byte("MZ"), category: previewCategoryBinary},
		{name: "shared library is binary", path: "lib.so", content: []byte("ELF"), category: previewCategoryBinary},
		{name: "pdf is binary", path: "doc.pdf", content: []byte("%PDF"), category: previewCategoryBinary},
		{name: "markdown stays text", path: "notes.md", content: []byte("# hi"), category: previewCategoryText},
		{name: "json stays text", path: "data.json", content: []byte(`{"a":1}`), category: previewCategoryText},
		{name: "svg stays text", path: "icon.svg", content: []byte("<svg/>"), category: previewCategoryText},
		{name: "no extension with text content", path: "README", content: []byte("hello"), category: previewCategoryText},
		{name: "no extension with NUL byte", path: "blob", content: []byte("ab\x00cd"), category: previewCategoryBinary},
		{name: "no extension with invalid UTF-8", path: "blob", content: []byte{0xff, 0xfe, 0x01}, category: previewCategoryBinary},
		{name: "unknown extension with text content", path: "data.xyz", content: []byte("plain"), category: previewCategoryText},
		{name: "unknown extension with NUL beyond sniff head", path: "data.xyz", content: append(bytesRepeat('a', previewSniffBytes), 0), category: previewCategoryText},
		{name: "empty content is text", path: "empty", content: nil, category: previewCategoryText},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := previewCategoryFor(test.path, test.content); got != test.category {
				t.Fatalf("previewCategoryFor(%q) = %q, want %q", test.path, got, test.category)
			}
		})
	}
}

func bytesRepeat(byteValue byte, count int) []byte {
	value := make([]byte, count)
	for index := range value {
		value[index] = byteValue
	}
	return value
}

func TestPreviewCategoryForBoundaryCutUTF8StaysText(t *testing.T) {
	tests := []struct {
		name   string
		rune   string
		offset int // rune bytes placed before the sniff boundary
	}{
		{name: "2-byte rune cut after its lead byte", rune: "é", offset: 1},
		{name: "3-byte rune cut after its lead byte", rune: "　", offset: 1},
		{name: "3-byte rune cut after one continuation byte", rune: "　", offset: 2},
		{name: "3-byte rune ends exactly at the boundary", rune: "　", offset: 3},
		{name: "4-byte rune cut after its lead byte", rune: "😀", offset: 1},
		{name: "4-byte rune cut after one continuation byte", rune: "😀", offset: 2},
		{name: "4-byte rune cut after two continuation bytes", rune: "😀", offset: 3},
		{name: "4-byte rune ends exactly at the boundary", rune: "😀", offset: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := []byte(test.rune)
			if test.offset > len(encoded) {
				t.Fatalf("test setup: offset %d exceeds rune size %d", test.offset, len(encoded))
			}
			content := make([]byte, 0, previewSniffBytes+len(encoded))
			content = append(content, bytesRepeat('a', previewSniffBytes-test.offset)...)
			content = append(content, encoded[:test.offset]...)
			content = append(content, encoded[test.offset:]...)
			content = append(content, bytesRepeat('b', 64)...)
			if !utf8.Valid(content) {
				t.Fatalf("test setup: content is not valid UTF-8")
			}
			if got := previewCategoryFor("data.xyz", content); got != previewCategoryText {
				t.Fatalf("previewCategoryFor() = %q, want text for rune %q cut at byte %d", got, test.rune, test.offset)
			}
		})
	}
}

func TestPreviewCategoryForIgnoresInvalidBytesBeyondSniffHead(t *testing.T) {
	content := append(bytesRepeat('a', previewSniffBytes), 0xff)
	if got := previewCategoryFor("data.xyz", content); got != previewCategoryText {
		t.Fatalf("previewCategoryFor() = %q, want text: bytes beyond the sniff head are not sniffed", got)
	}
}

func TestPreviewCategoryForNULAndTrueInvalidUTF8StayBinary(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "NUL byte inside the sniff head", content: append(append(bytesRepeat('a', 4096), 0), bytesRepeat('b', 8192)...)},
		{name: "invalid byte inside the sniff head", content: append(append(bytesRepeat('a', 8188), 0xff), bytesRepeat('b', 64)...)},
		{name: "invalid byte at the cut position", content: append(append(bytesRepeat('a', 8191), 0xff), bytesRepeat('b', 64)...)},
		{name: "invalid lead byte at the cut", content: append(append(bytesRepeat('a', 8191), 0xc0), bytesRepeat('b', 64)...)},
		{name: "out-of-range lead byte at the cut", content: append(append(bytesRepeat('a', 8191), 0xf5), bytesRepeat('b', 64)...)},
		{name: "lone continuation byte at the cut", content: append(append(bytesRepeat('a', 8191), 0x80), bytesRepeat('b', 64)...)},
		{name: "overlong prefix at the cut", content: append(append(bytesRepeat('a', 8190), 0xe0, 0x80), bytesRepeat('b', 64)...)},
		{name: "ED continuation above its range at the cut", content: append(append(bytesRepeat('a', 8190), 0xed, 0xa0), bytesRepeat('b', 64)...)},
		{name: "F0 continuation below its range at the cut", content: append(append(bytesRepeat('a', 8190), 0xf0, 0x80), bytesRepeat('b', 64)...)},
		{name: "F4 continuation above its range at the cut", content: append(append(bytesRepeat('a', 8190), 0xf4, 0x90), bytesRepeat('b', 64)...)},
		{name: "extra continuation beyond the rune size at the cut", content: append(append(bytesRepeat('a', 8188), 0xe3, 0x80, 0x80, 0x80), bytesRepeat('b', 64)...)},
		{name: "small file ending with a partial rune", content: append([]byte("abc"), 0xe3)},
		{name: "small file with NUL byte", content: []byte("a\x00b")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := previewCategoryFor("data.xyz", test.content); got != previewCategoryBinary {
				t.Fatalf("previewCategoryFor() = %q, want binary", got)
			}
		})
	}
}

func TestPreviewTextLinesNormalizesCRLFTabsAndControls(t *testing.T) {
	content := []byte("first\r\nsecond\tend\r\n\x1b[31mred\x00")
	lines := previewTextLines(content)
	if len(lines) != 3 {
		t.Fatalf("previewTextLines() = %d lines, want 3", len(lines))
	}
	want := []struct {
		text   string
		number int
	}{
		{text: "first", number: 1},
		{text: "second    end", number: 2},
		{text: "\uFFFD[31mred\uFFFD", number: 3},
	}
	for index, expected := range want {
		if lines[index].text != expected.text || lines[index].number != expected.number {
			t.Errorf("line %d = %#v, want %#v", index, lines[index], expected)
		}
	}
}

func TestPreviewTextLinesPreservesEmptyLinesAndTrailingNewline(t *testing.T) {
	lines := previewTextLines([]byte("a\n\nb\n"))
	if len(lines) != 4 {
		t.Fatalf("previewTextLines() = %d lines, want 4", len(lines))
	}
	for index, want := range []string{"a", "", "b", ""} {
		if lines[index].text != want {
			t.Errorf("line %d = %q, want %q", index, lines[index].text, want)
		}
	}
}

func TestCutHeadToWidthKeepsWholeGraphemes(t *testing.T) {
	for _, value := range []string{"日本語の長い行", "emoji🙂混合", "e\u0301combining", "simple text"} {
		for width := 0; width < lipgloss.Width(value); width++ {
			head := cutHeadToWidth(value, width)
			if got := lipgloss.Width(head); got > width {
				t.Errorf("cutHeadToWidth(%q, %d) width = %d, want <= %d", value, width, got, width)
			}
			if !strings.HasPrefix(value, head) {
				t.Errorf("cutHeadToWidth(%q, %d) = %q, want a prefix", value, width, head)
			}
		}
	}
	if got := cutHeadToWidth("abc", 0); got != "" {
		t.Fatalf("cutHeadToWidth width 0 = %q, want empty", got)
	}
}

func TestWrapToWidthHardWrapsAtCellWidth(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		width int
		want  []string
	}{
		{name: "fits", line: "ab", width: 4, want: []string{"ab"}},
		{name: "exact width", line: "abcd", width: 4, want: []string{"abcd"}},
		{name: "wraps once", line: "abcdef", width: 4, want: []string{"abcd", "ef"}},
		{name: "wraps with empty tail end", line: "abcdef", width: 3, want: []string{"abc", "def"}},
		{name: "wide characters", line: "日本語", width: 3, want: []string{"日", "本", "語"}},
		{name: "wide characters two cells", line: "日本語", width: 4, want: []string{"日本", "語"}},
		{name: "empty line", line: "", width: 4, want: []string{""}},
		{name: "narrower than one grapheme", line: "日本語", width: 1, want: []string{"日本語"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := wrapToWidth(test.line, test.width)
			if len(got) != len(test.want) {
				t.Fatalf("wrapToWidth(%q, %d) = %q, want %q", test.line, test.width, got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("wrapToWidth(%q, %d) = %q, want %q", test.line, test.width, got, test.want)
				}
			}
		})
	}
}

func TestBuildDisplayLinesKeepsHeadNumbersAndBlanksContinuations(t *testing.T) {
	lines := []previewLine{
		{text: "short", number: 1},
		{text: "abcdefghij", number: 2},
	}
	display := buildDisplayLines(lines, true, 4)
	want := []previewLine{
		{text: "shor", number: 1, origin: 0, col: 0},
		{text: "t", number: 0, origin: 0, col: 4},
		{text: "abcd", number: 2, origin: 1, col: 0},
		{text: "efgh", number: 0, origin: 1, col: 4},
		{text: "ij", number: 0, origin: 1, col: 8},
	}
	if len(display) != len(want) {
		t.Fatalf("buildDisplayLines() = %#v, want %#v", display, want)
	}
	for index := range want {
		if !reflect.DeepEqual(display[index], want[index]) {
			t.Errorf("display line %d = %#v, want %#v", index, display[index], want[index])
		}
	}

	if display := buildDisplayLines(lines, false, 4); len(display) != 2 {
		t.Fatalf("unwrapped buildDisplayLines() = %d lines, want 2", len(display))
	}
}

func TestMaxContentWidthForSpansRawLines(t *testing.T) {
	lines := []previewLine{{text: "ab"}, {text: "日本語"}, {text: ""}}
	if got := maxContentWidthFor(lines); got != 6 {
		t.Fatalf("maxContentWidthFor() = %d, want 6", got)
	}
}

func TestAddWarningAppendsDistinctWarningsOnly(t *testing.T) {
	if got := addWarning("", "a"); got != "a" {
		t.Fatalf("addWarning() = %q, want a", got)
	}
	if got := addWarning("a", "b"); got != "a; b" {
		t.Fatalf("addWarning() = %q, want a; b", got)
	}
	if got := addWarning("a; b", "b"); got != "a; b" {
		t.Fatalf("addWarning(duplicate) = %q, want unchanged", got)
	}
	if got := addWarning("a", ""); got != "a" {
		t.Fatalf("addWarning(empty) = %q, want unchanged", got)
	}
}

type fakePreviewReader struct {
	content   []byte
	truncated bool
	err       error
	calls     []string
	limits    []int
}

func (f *fakePreviewReader) ReadFileHead(path string, limit int) ([]byte, bool, error) {
	f.calls = append(f.calls, path)
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, false, f.err
	}
	return f.content, f.truncated, nil
}

type stubPreviewClient struct {
	panes      []PreviewPane
	listErr    error
	getPane    PreviewPane
	getFound   bool
	getErr     error
	openPaneID string
	openErr    error
	closeErr   error
	tagErr     error

	openFiles   []string
	openTargets []string
	closed      []string
	listed      []string
	tagged      [][2]string
	getCalls    []string
}

func (s *stubPreviewClient) OpenPreview(file, targetPane string) (string, error) {
	s.openFiles = append(s.openFiles, file)
	s.openTargets = append(s.openTargets, targetPane)
	if s.openErr != nil {
		return "", s.openErr
	}
	return s.openPaneID, nil
}

func (s *stubPreviewClient) ClosePane(paneID string) error {
	s.closed = append(s.closed, paneID)
	return s.closeErr
}

func (s *stubPreviewClient) GetPane(paneID string) (PreviewPane, bool, error) {
	s.getCalls = append(s.getCalls, paneID)
	return s.getPane, s.getFound, s.getErr
}

func (s *stubPreviewClient) ListPanes(workspaceID string) ([]PreviewPane, error) {
	s.listed = append(s.listed, workspaceID)
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.panes, nil
}

func (s *stubPreviewClient) TagPreview(paneID, file string) error {
	s.tagged = append(s.tagged, [2]string{paneID, file})
	return s.tagErr
}

func TestPreviewModelWithoutFileWaitsForQuit(t *testing.T) {
	model := NewPreviewModel("", nil, "")
	if got := model.status; got != "Warning: "+model.warning {
		t.Fatalf("preview status = %q, want warning", got)
	}
	if cmd := model.Init(); cmd == nil {
		t.Fatal("Init() returned nil; want background color request")
	}
	if model.loading {
		t.Fatal("model without file is loading")
	}
	if _, cmd := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Fatal("q returned nil command, want quit")
	}
}

func TestPreviewModelLoadsTextThroughCommand(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("hello\r\nworld")}
	model := NewPreviewModel("/abs/hello.txt", nil, "", reader)
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil command")
	}
	message := previewLoadResult(t, cmd)
	if message.err != "" || message.category != previewCategoryText {
		t.Fatalf("load message = %#v, want text without error", message)
	}
	if len(message.lines) != 2 || message.lines[0].text != "hello" || message.lines[0].number != 1 {
		t.Fatalf("load lines = %#v, want normalized numbered lines", message.lines)
	}
	if len(reader.calls) != 1 || reader.calls[0] != "/abs/hello.txt" || reader.limits[0] != previewMaxBytes {
		t.Fatalf("reader calls = %v limits %v, want one call at previewMaxBytes", reader.calls, reader.limits)
	}

	model.Update(message)
	if model.loading || model.status != readyStatus {
		t.Fatalf("after load: loading %v, status %q; want idle and %q", model.loading, model.status, readyStatus)
	}
	if model.category != previewCategoryText || model.lineCount != 2 {
		t.Fatalf("model category = %q, lineCount = %d; want text, 2", model.category, model.lineCount)
	}
}

func TestPreviewWhitespaceToggleRendersDotsWithoutChangingDisplayState(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("a  b\n\tc")}
	model := NewPreviewModel("/abs/whitespace.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	model.selection = previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 3},
	}
	model.offset = 0
	model.xoffset = 0
	displayBefore := append([]previewLine(nil), model.displayLines...)
	selectionBefore := model.selection
	offsetBefore, xoffsetBefore := model.offset, model.xoffset
	off := ansi.Strip(model.View().Content)
	if strings.Contains(off, previewWhitespaceGlyph) {
		t.Fatalf("spaces-off view = %q, must not contain whitespace glyph", off)
	}

	_, cmd := model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if cmd != nil {
		t.Fatalf("whitespace toggle returned command %v, want nil", cmd)
	}
	if !model.showWhitespace {
		t.Fatal("whitespace toggle did not enable display")
	}
	if !reflect.DeepEqual(model.displayLines, displayBefore) {
		t.Fatalf("whitespace toggle rebuilt displayLines: %#v, want %#v", model.displayLines, displayBefore)
	}
	if model.selection != selectionBefore || model.offset != offsetBefore || model.xoffset != xoffsetBefore {
		t.Fatalf("whitespace toggle changed state: selection %#v/%#v offset %d/%d xoffset %d/%d", model.selection, selectionBefore, model.offset, offsetBefore, model.xoffset, xoffsetBefore)
	}
	on := ansi.Strip(model.View().Content)
	if !strings.Contains(on, "a⋅⋅b") || !strings.Contains(on, "⋅⋅⋅⋅c") {
		t.Fatalf("spaces-on view = %q, want visible spaces", on)
	}

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if model.showWhitespace {
		t.Fatal("second whitespace toggle did not disable display")
	}
	if got := ansi.Strip(model.View().Content); got != off {
		t.Fatalf("spaces-off view after second toggle = %q, want %q", got, off)
	}
}

func TestPreviewWhitespaceTogglePersistsAcrossReloadResizeAndWrap(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("first  line\nsecond    line\nthird line")}
	model := NewPreviewModel("/abs/whitespace-state.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !model.showWhitespace {
		t.Fatal("whitespace toggle did not enable display")
	}

	reader.content = []byte("reloaded  line\nnext line")
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	model.Update(cmd())
	if !model.showWhitespace {
		t.Fatal("reload reset whitespace visibility")
	}

	model.Update(tea.WindowSizeMsg{Width: 30, Height: 8})
	if !model.showWhitespace {
		t.Fatal("resize reset whitespace visibility")
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !model.showWhitespace {
		t.Fatal("wrap toggle reset whitespace visibility")
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !model.showWhitespace {
		t.Fatal("second wrap toggle reset whitespace visibility")
	}
}

func TestPreviewWhitespaceRenderingUsesThemeForegroundAndSelectionBackground(t *testing.T) {
	model := PreviewModel{
		lineCount:      1,
		showWhitespace: true,
		selection: previewSelection{
			anchor: previewPosition{line: 0, col: 1},
			focus:  previewPosition{line: 0, col: 3},
		},
	}
	rendered := model.renderContent(previewLine{text: "a  b", origin: 0}, 4)
	if got := ansi.Strip(rendered); got != "a⋅⋅b" {
		t.Fatalf("dark whitespace rendering = %q, want visible spaces", got)
	}
	if !strings.Contains(rendered, "38;5;241") || !strings.Contains(rendered, "48;5;240") {
		t.Fatalf("dark whitespace rendering = %q, want foreground 241 and selection background 240", rendered)
	}

	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	rendered = model.renderContent(previewLine{text: "a  b", origin: 0}, 4)
	if !strings.Contains(rendered, "38;5;250") || !strings.Contains(rendered, "48;5;252") {
		t.Fatalf("light whitespace rendering = %q, want foreground 250 and selection background 252", rendered)
	}
}

func TestPreviewWhitespaceViewBodyMatchesGlyphSubstitutedFullLines(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("first  line\nsecond    line\n  indented  last")}
	model := NewPreviewModel("/abs/ws-full.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	if got := len(model.displayLines); got != 3 {
		t.Fatalf("setup: displayLines = %d, want 3", got)
	}

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !model.showWhitespace {
		t.Fatalf("whitespace toggle did not enable display")
	}
	rows := strings.Split(ansi.Strip(model.View().Content), "\n")
	bodyStart := model.bodyStartY()
	for index, original := range model.lines {
		want := strings.ReplaceAll(original.text, " ", previewWhitespaceGlyph)
		if got := previewBodyContent(model, rows[bodyStart+index]); got != want {
			t.Errorf("whitespace ON body line %d = %q, want %q", index, got, want)
		}
	}

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if model.showWhitespace {
		t.Fatalf("whitespace toggle did not disable display")
	}
	rows = strings.Split(ansi.Strip(model.View().Content), "\n")
	for index, original := range model.lines {
		if got := previewBodyContent(model, rows[bodyStart+index]); got != original.text {
			t.Fatalf("whitespace OFF body line %d = %q, want %q", index, got, original.text)
		}
	}
}

// previewBodyContent extracts the line content from an ansi-stripped body row,
// removing the left padding, line-number gutter, separator, trailing scrollbar
// cell, and the right padding.
func previewBodyContent(model *PreviewModel, row string) string {
	content := row[model.contentLeftPadding():]
	content = content[model.gutterWidth():]
	content = strings.TrimPrefix(content, previewGutterDividerGlyph)
	content = strings.TrimPrefix(content, " ")
	content = strings.TrimSuffix(content, scrollbarTrackGlyph)
	content = strings.TrimSuffix(content, scrollbarThumbGlyph)
	return strings.TrimRight(content, " ")
}

func TestPreviewWhitespaceRenderingEdgeCasesForEmptyAndSpaceOnlyLines(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "", want: ""},
		{text: "  ", want: "⋅⋅"},
		{text: "   ", want: "⋅⋅⋅"},
		{text: "trailing  ", want: "trailing⋅⋅"},
		{text: "ab  cd", want: "ab⋅⋅cd"},
		{text: "a\u3000b c", want: "a\u3000b⋅c"},
	}
	model := PreviewModel{showWhitespace: true}
	for _, test := range tests {
		line := previewLine{text: test.text, origin: 0}
		got := strings.TrimRight(ansi.Strip(model.renderContent(line, 20)), " ")
		if got != test.want {
			t.Errorf("whitespace ON renderContent(%q) = %q, want %q", test.text, got, test.want)
		}
		if width := lipgloss.Width(got); width != lipgloss.Width(test.text) {
			t.Errorf("whitespace ON renderContent(%q) cell width = %d, want %d", test.text, width, lipgloss.Width(test.text))
		}
	}

	model.showWhitespace = false
	for _, test := range tests {
		line := previewLine{text: test.text, origin: 0}
		got := strings.TrimRight(ansi.Strip(model.renderContent(line, 20)), " ")
		if got != strings.TrimRight(test.text, " ") {
			t.Errorf("whitespace OFF renderContent(%q) = %q, want %q", test.text, got, strings.TrimRight(test.text, " "))
		}
		if strings.Contains(got, previewWhitespaceGlyph) {
			t.Errorf("whitespace OFF renderContent(%q) contains glyph", test.text)
		}
	}
}

func TestPreviewWhitespaceToggleLeavesMutedAndUnsupportedContentUnchanged(t *testing.T) {
	truncatedReader := &fakePreviewReader{content: []byte("trailing  "), truncated: true}
	truncated := NewPreviewModel("/abs/truncated.txt", nil, "", truncatedReader)
	truncated.Update(tea.WindowSizeMsg{Width: 70, Height: 8})
	truncated.Update(previewLoadResult(t, truncated.Init()))
	marker := truncated.displayLines[len(truncated.displayLines)-1]
	markerBefore := ansi.Strip(truncated.renderContent(marker, truncated.contentWidth()))
	truncated.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	markerAfter := ansi.Strip(truncated.renderContent(truncated.displayLines[len(truncated.displayLines)-1], truncated.contentWidth()))
	if markerBefore != markerAfter || strings.Contains(markerAfter, previewWhitespaceGlyph) {
		t.Fatalf("truncation marker changed from %q to %q", markerBefore, markerAfter)
	}

	unsupportedReader := &fakePreviewReader{content: []byte("ignored")}
	unsupported := NewPreviewModel("/abs/archive.zip", nil, "", unsupportedReader)
	unsupported.Update(tea.WindowSizeMsg{Width: 70, Height: 8})
	unsupported.Update(previewLoadResult(t, unsupported.Init()))
	unsupportedBefore := ansi.Strip(unsupported.View().Content)
	unsupported.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if got := ansi.Strip(unsupported.View().Content); got != unsupportedBefore {
		t.Fatalf("unsupported view changed after whitespace toggle: %q -> %q", unsupportedBefore, got)
	}
}

func TestPreviewReloadKeyRefreshesContentAndPreservesViewState(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: bytesRepeatContent(20)}
	model := NewPreviewModel("/abs/reload.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	model.wrap = true
	model.rebuildDisplayLines()
	model.offset = 3
	model.selection = previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 1, col: 2},
	}
	reader.content = []byte("new first\nnew second\nnew third\nnew fourth\nnew fifth")

	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	if !model.loading || model.status != previewLoadingStatus {
		t.Fatalf("reload state = loading %v, status %q; want loading", model.loading, model.status)
	}
	message, ok := cmd().(previewLoadMsg)
	if !ok {
		t.Fatalf("reload command message = %T, want previewLoadMsg", cmd())
	}
	if !message.reload {
		t.Fatal("reload message is not marked as a manual reload")
	}
	_, toastCmd := model.Update(message)
	if toastCmd == nil {
		t.Fatal("successful reload returned no toast command")
	}

	if len(reader.calls) != 2 || reader.calls[1] != "/abs/reload.txt" {
		t.Fatalf("reader calls = %v, want initial load and reload of the displayed path", reader.calls)
	}
	if len(reader.limits) != 2 || reader.limits[1] != previewMaxBytes {
		t.Fatalf("reader limits = %v, want previewMaxBytes for reload", reader.limits)
	}
	if model.loading || model.status != readyStatus {
		t.Fatalf("after reload: loading %v, status %q; want ready", model.loading, model.status)
	}
	if !model.wrap {
		t.Fatal("reload changed wrap mode")
	}
	if model.offset != 1 {
		t.Fatalf("reload offset = %d, want clamped offset 1", model.offset)
	}
	if !model.selection.empty() {
		t.Fatalf("reload left selection %#v", model.selection)
	}
	if len(model.lines) != 5 || model.lines[0].text != "new first" {
		t.Fatalf("reloaded lines = %#v, want new content", model.lines)
	}
	if model.toast != reloadToastText {
		t.Fatalf("reload toast = %q, want %q", model.toast, reloadToastText)
	}
	if got, ok := toastCmd().(previewToastTimeoutMsg); !ok || got.seq != model.toastSeq {
		t.Fatalf("reload toast command message = %#v, want current preview timeout", toastCmd())
	}
}

func TestPreviewReloadErrorKeepsPreviousContentAndViewState(t *testing.T) {
	reader := &fakePreviewReader{
		content:   []byte(strings.Repeat("0123456789abcdefghij\n", 20)),
		truncated: true,
	}
	model := NewPreviewModel("/abs/reload-error.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 24, Height: 7})
	model.Update(previewLoadResult(t, model.Init()))

	model.offset = 5
	model.xoffset = 1
	model.dragMode = previewDragVScroll
	model.selection = previewSelection{
		anchor: previewPosition{line: 1, col: 2},
		focus:  previewPosition{line: 3, col: 4},
	}
	lines := append([]previewLine(nil), model.lines...)
	displayLines := append([]previewLine(nil), model.displayLines...)
	lineCount := model.lineCount
	category := model.category
	truncated := model.truncated
	maxContentWidth := model.maxContentWidth
	offset := model.offset
	xoffset := model.xoffset
	dragMode := model.dragMode
	selection := model.selection

	reader.err = errors.New("file disappeared")
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("reload returned nil command")
	}
	loadMessage := cmd()
	message, ok := loadMessage.(previewLoadMsg)
	if !ok {
		t.Fatalf("reload command message = %T, want previewLoadMsg", loadMessage)
	}
	if !message.reload {
		t.Fatal("reload message is not marked as a manual reload")
	}
	if _, cmd := model.Update(message); cmd != nil {
		t.Fatalf("failed reload returned command %v, want nil", cmd)
	}

	if model.loading {
		t.Fatal("model still loading after failed reload")
	}
	if !strings.Contains(model.status, "Warning: file disappeared") {
		t.Fatalf("status = %q, want reload warning", model.status)
	}
	if model.warning != "file disappeared" {
		t.Fatalf("warning = %q, want reload error", model.warning)
	}
	if !reflect.DeepEqual(model.lines, lines) {
		t.Fatalf("lines changed after failed reload: %#v, want %#v", model.lines, lines)
	}
	if !reflect.DeepEqual(model.displayLines, displayLines) {
		t.Fatalf("displayLines changed after failed reload: %#v, want %#v", model.displayLines, displayLines)
	}
	if model.lineCount != lineCount || model.category != category || model.truncated != truncated {
		t.Fatalf("content metadata changed after failed reload: lineCount %d/%d category %q/%q truncated %v/%v", model.lineCount, lineCount, model.category, category, model.truncated, truncated)
	}
	if model.maxContentWidth != maxContentWidth {
		t.Fatalf("maxContentWidth = %d, want unchanged %d", model.maxContentWidth, maxContentWidth)
	}
	if model.offset != offset || model.xoffset != xoffset {
		t.Fatalf("scroll position changed after failed reload: offset %d/%d xoffset %d/%d", model.offset, offset, model.xoffset, xoffset)
	}
	if model.dragMode != dragMode {
		t.Fatalf("drag mode = %d, want unchanged %d", model.dragMode, dragMode)
	}
	if model.selection != selection {
		t.Fatalf("selection changed after failed reload: %#v, want %#v", model.selection, selection)
	}
}

func TestPreviewReloadRetriesMissingFileAndClearsWarning(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{err: errors.New("missing")}
	model := NewPreviewModel("/abs/appears.txt", nil, "", reader)
	model.Update(previewLoadResult(t, model.Init()))
	if model.warning == "" {
		t.Fatal("initial missing file produced no warning")
	}

	reader.err = nil
	reader.content = []byte("file appeared")
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("retry returned nil command")
	}
	_, toastCmd := model.Update(cmd())
	if toastCmd == nil {
		t.Fatal("successful retry returned no toast command")
	}
	if model.warning != "" || model.status != readyStatus {
		t.Fatalf("after retry: warning %q, status %q; want cleared warning and ready", model.warning, model.status)
	}
	if len(model.lines) != 1 || model.lines[0].text != "file appeared" {
		t.Fatalf("retried lines = %#v, want appeared file", model.lines)
	}
	if model.toast != reloadToastText {
		t.Fatalf("retry toast = %q, want %q", model.toast, reloadToastText)
	}
}

func TestPreviewReloadIsNoOpWithoutPreviewFile(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("ignored")}
	model := NewPreviewModel("", nil, "", reader)
	loading, status, warning := model.loading, model.status, model.warning
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd != nil {
		t.Fatalf("reload without preview file returned command %v, want nil", cmd)
	}
	if model.loading != loading || model.status != status || model.warning != warning || len(reader.calls) != 0 {
		t.Fatalf("reload without preview file changed state: loading %v/%v status %q/%q warning %q/%q calls %v", model.loading, loading, model.status, status, model.warning, warning, reader.calls)
	}
}

func TestPreviewModelReportsMetadataTokenAtInit(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("x")}
	client := &stubPreviewClient{}
	model := NewPreviewModel("/abs/token.md", client, "wY:p9Z", reader)
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init() message = %T, want tea.BatchMsg", cmd())
	}
	for _, command := range batch {
		model.Update(command())
	}
	if len(client.tagged) != 1 || client.tagged[0] != [2]string{"wY:p9Z", "/abs/token.md"} {
		t.Fatalf("TagPreview calls = %v, want own pane and file", client.tagged)
	}
	if model.loading {
		t.Fatal("load did not complete after batch")
	}
}

func TestPreviewModelWarnsOnLoadErrorAndKeepsWaiting(t *testing.T) {
	reader := &fakePreviewReader{err: errors.New("no such file\x1b")}
	model := NewPreviewModel("/abs/missing.txt", nil, "", reader)
	model.Update(previewLoadResult(t, model.Init()))
	if model.loading {
		t.Fatal("model still loading after error")
	}
	if !strings.Contains(model.status, "Warning:") || strings.Contains(model.status, "\x1b") {
		t.Fatalf("status = %q, want sanitized warning", model.status)
	}
	if _, cmd := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Fatal("q after error returned nil command")
	}
}

func TestPreviewModelShowsUnsupportedLabelForBinaryCategory(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("PK\x03\x04")}
	model := NewPreviewModel("/abs/bundle.zip", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 6})
	model.Update(previewLoadResult(t, model.Init()))

	content := ansi.Strip(model.View().Content)
	lines := strings.Split(content, "\n")
	bodyStart := 1 + headerDividerHeight(model.height)
	if !strings.Contains(lines[bodyStart], "Unsupported preview: binary") {
		t.Fatalf("body = %q, want unsupported binary label", lines[bodyStart])
	}
	if strings.Contains(content, "PK") {
		t.Fatalf("view = %q, must not show binary content", content)
	}
	if got := strings.TrimRight(lines[len(lines)-1], " "); got != " w wrap    s spaces    space copy    r reload    q close" {
		t.Fatalf("footer = %q, want preview shortcuts", got)
	}
}

func TestPreviewLayoutHeightsReservesHorizontalBarRow(t *testing.T) {
	for _, test := range []struct {
		height     int
		showHBar   bool
		wantHeader int
		wantBody   int
		wantHBar   int
		wantFooter int
	}{
		{height: 0, wantHeader: 0, wantBody: 0, wantHBar: 0, wantFooter: 0},
		{height: 1, wantHeader: 1, wantBody: 0, wantHBar: 0, wantFooter: 0},
		{height: 2, wantHeader: 1, wantBody: 0, wantHBar: 0, wantFooter: 1},
		{height: 3, wantHeader: 1, wantBody: 1, wantHBar: 0, wantFooter: 1},
		{height: 4, showHBar: true, wantHeader: 1, wantBody: 1, wantHBar: 0, wantFooter: 1},
		{height: 5, showHBar: true, wantHeader: 1, wantBody: 1, wantHBar: 0, wantFooter: 1},
		{height: 5, wantHeader: 1, wantBody: 1, wantHBar: 0, wantFooter: 1},
		{height: 6, showHBar: true, wantHeader: 1, wantBody: 1, wantHBar: 1, wantFooter: 1},
		{height: 6, wantHeader: 1, wantBody: 2, wantHBar: 0, wantFooter: 1},
		{height: 7, showHBar: true, wantHeader: 1, wantBody: 2, wantHBar: 1, wantFooter: 1},
		{height: 8, showHBar: true, wantHeader: 1, wantBody: 3, wantHBar: 1, wantFooter: 1},
	} {
		header, body, hbar, footer := previewLayoutHeights(test.height, test.showHBar)
		if header != test.wantHeader || body != test.wantBody || hbar != test.wantHBar || footer != test.wantFooter {
			t.Errorf("previewLayoutHeights(%d, %v) = (%d, %d, %d, %d), want (%d, %d, %d, %d)",
				test.height, test.showHBar, header, body, hbar, footer, test.wantHeader, test.wantBody, test.wantHBar, test.wantFooter)
		}
	}
}

func TestPreviewVerticalNavigationAndBoundaries(t *testing.T) {
	reader := &fakePreviewReader{content: bytesRepeatContent(100)}
	model := NewPreviewModel("/abs/lines.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 7})
	model.Update(previewLoadResult(t, model.Init()))

	body := model.bodyHeight()
	maxOffset := model.maxVerticalOffset()

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if model.offset != 2 {
		t.Fatalf("j offset = %d, want 2", model.offset)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if model.offset != 1 {
		t.Fatalf("k offset = %d, want 1", model.offset)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if model.offset != body {
		t.Fatalf("ctrl+f offset = %d, want %d", model.offset, body)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if model.offset != 1 {
		t.Fatalf("ctrl+b offset = %d, want 1", model.offset)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyHome})
	if model.offset != 0 {
		t.Fatalf("home offset = %d, want 0", model.offset)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyEnd})
	if model.offset != maxOffset {
		t.Fatalf("end offset = %d, want %d", model.offset, maxOffset)
	}
	for range maxOffset + 2 {
		model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if model.offset != maxOffset {
		t.Fatalf("j past bottom offset = %d, want clamped %d", model.offset, maxOffset)
	}
	for range maxOffset + 2 {
		model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'k', Text: "k"})
	}
	if model.offset != 0 {
		t.Fatalf("k past top offset = %d, want 0", model.offset)
	}
}

func bytesRepeatContent(count int) []byte {
	var content []byte
	for index := 0; index < count; index++ {
		if len(content) > 0 {
			content = append(content, '\n')
		}
		content = append(content, []byte("line-"+strings.Repeat("x", 3))...)
	}
	return content
}

func TestPreviewHorizontalScrollOnlyWhenWrapOff(t *testing.T) {
	reader := &fakePreviewReader{content: []byte(strings.Repeat("abcdefghij", 20))}
	model := NewPreviewModel("/abs/wide.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 24, Height: 7})
	model.Update(previewLoadResult(t, model.Init()))

	if !model.showHorizontalScrollbar() {
		t.Fatal("wide line did not activate the horizontal scrollbar")
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyRight})
	if model.xoffset != 1 {
		t.Fatalf("right xoffset = %d, want 1", model.xoffset)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyLeft})
	if model.xoffset != 0 {
		t.Fatalf("left xoffset = %d, want 0", model.xoffset)
	}
	for range model.maxHorizontalOffset() + 3 {
		model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if model.xoffset != model.maxHorizontalOffset() {
		t.Fatalf("right past end xoffset = %d, want %d", model.xoffset, model.maxHorizontalOffset())
	}

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !model.wrap || model.xoffset != 0 {
		t.Fatalf("wrap toggle = %v, xoffset %d; want wrap with reset offset", model.wrap, model.xoffset)
	}
	if model.showHorizontalScrollbar() {
		t.Fatal("wrap mode still shows the horizontal scrollbar")
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyRight})
	if model.xoffset != 0 {
		t.Fatalf("right in wrap mode changed xoffset to %d, want 0", model.xoffset)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if model.wrap || model.xoffset != 0 {
		t.Fatalf("wrap off state = wrap %v, xoffset %d; want off and 0", model.wrap, model.xoffset)
	}
}

func TestPreviewWrapClampsVerticalOffsetToWrappedRows(t *testing.T) {
	reader := &fakePreviewReader{content: []byte(strings.Join([]string{
		"abcdefghij", "abcdefghij", "abcdefghij", "abcdefghij", "abcdefghij",
		"abcdefghij", "abcdefghij", "abcdefghij", "abcdefghij", "abcdefghij",
	}, "\n"))}
	model := NewPreviewModel("/abs/wrap.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 14, Height: 7})
	model.Update(previewLoadResult(t, model.Init()))

	// contentWidth is 8 (14 - padding - 2-digit gutter - 2-cell separator - bar), so
	// every 10-cell line wraps into two segments; the horizontal bar takes a
	// row, leaving a two-row body.
	if model.contentWidth() != 8 {
		t.Fatalf("contentWidth = %d, want 8", model.contentWidth())
	}
	body := model.bodyHeight()
	unwrappedMax := len(model.displayLines) - body
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyEnd})
	if model.offset != unwrappedMax {
		t.Fatalf("end offset = %d, want %d", model.offset, unwrappedMax)
	}

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	wrappedTotal := len(model.displayLines)
	if wrappedTotal != 20 {
		t.Fatalf("wrapped rows = %d, want 20", wrappedTotal)
	}
	if model.offset != unwrappedMax {
		t.Fatalf("offset after wrap = %d, want unchanged in-range %d", model.offset, unwrappedMax)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyEnd})
	// Wrap mode hides the horizontal bar, so the body grows by one row and
	// the wrapped maximum is one larger than unwrappedTotal-body.
	if model.offset != len(model.displayLines)-model.bodyHeight() {
		t.Fatalf("end offset = %d, want %d", model.offset, len(model.displayLines)-model.bodyHeight())
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if model.offset != unwrappedMax {
		t.Fatalf("offset after unwrap clamp = %d, want %d", model.offset, unwrappedMax)
	}
}

func TestPreviewWhitespaceToggleLeavesWrappedDisplayLineSegmentsIdentical(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("aa bb cc dd ee ff\nsecond line here\nthird")}
	model := NewPreviewModel("/abs/ws-wrap.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !model.wrap {
		t.Fatal("wrap toggle did not enable wrap")
	}
	if len(model.displayLines) <= 3 {
		t.Fatalf("setup did not wrap: displayLines = %#v", model.displayLines)
	}

	record := func() []string {
		rows := make([]string, len(model.displayLines))
		for index, line := range model.displayLines {
			rows[index] = fmt.Sprintf("%d:%d:%q", line.origin, line.col, line.text)
		}
		return rows
	}
	baseline := record()

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !model.showWhitespace {
		t.Fatal("whitespace toggle did not enable display")
	}
	model.rebuildDisplayLines()
	if got := record(); !reflect.DeepEqual(got, baseline) {
		t.Fatalf("whitespace ON wrap segments = %#v, want %#v", got, baseline)
	}

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if model.showWhitespace {
		t.Fatal("whitespace toggle did not disable display")
	}
	model.rebuildDisplayLines()
	if got := record(); !reflect.DeepEqual(got, baseline) {
		t.Fatalf("whitespace OFF wrap segments = %#v, want %#v", got, baseline)
	}
}

func TestPreviewRendersGutterNumbersAndBlankContinuations(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("one\nalphabetabcdefghij\nxyz")}
	model := NewPreviewModel("/abs/multi.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 16, Height: 8})
	model.Update(previewLoadResult(t, model.Init())) // "alphabetabcdefghij" wraps at the 10-cell content width
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	bodyStart := 1 + headerDividerHeight(model.height)
	if !strings.HasPrefix(lines[bodyStart], "  1"+previewGutterDividerGlyph+" one") {
		t.Fatalf("first body line = %q, want gutter 1", lines[bodyStart])
	}
	if !strings.HasPrefix(lines[bodyStart+1], "  2"+previewGutterDividerGlyph+" alphabetab") {
		t.Fatalf("wrapped head line = %q, want line number 2", lines[bodyStart+1])
	}
	if !strings.HasPrefix(lines[bodyStart+2], "   "+previewGutterDividerGlyph+" cdefghij") {
		t.Fatalf("continuation line = %q, want blank gutter", lines[bodyStart+2])
	}
	if !strings.HasPrefix(lines[bodyStart+3], "  3"+previewGutterDividerGlyph+" xyz") {
		t.Fatalf("last body line = %q, want gutter 3", lines[bodyStart+3])
	}
}

func TestPreviewBodyOmitsDividerWithoutGutter(t *testing.T) {
	tests := []struct {
		name  string
		model *PreviewModel
	}{
		{
			name:  "loading",
			model: NewPreviewModel("/abs/loading.txt", nil, ""),
		},
		{
			name: "warning",
			model: func() *PreviewModel {
				reader := &fakePreviewReader{err: errors.New("boom")}
				model := NewPreviewModel("/abs/missing.txt", nil, "", reader)
				model.Update(previewLoadResult(t, model.Init()))
				return model
			}(),
		},
		{
			name: "unsupported",
			model: func() *PreviewModel {
				reader := &fakePreviewReader{content: []byte("PK\x03\x04")}
				model := NewPreviewModel("/abs/bundle.zip", nil, "", reader)
				model.Update(previewLoadResult(t, model.Init()))
				return model
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := test.model
			model.Update(tea.WindowSizeMsg{Width: 30, Height: 6})
			lines := strings.Split(ansi.Strip(model.View().Content), "\n")
			separatorIndex := model.contentLeftPadding() + model.gutterWidth()
			bodyLine := []rune(lines[model.bodyStartY()])
			if got := string(bodyLine[separatorIndex]); got == previewGutterDividerGlyph {
				t.Fatalf("body line = %q, has divider without gutter", lines[model.bodyStartY()])
			}
		})
	}
}

func TestPreviewRendersTitleFooterAndTruncatedMarker(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("short"), truncated: true}
	model := NewPreviewModel("/abs/very-long-directory-name/file.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 70, Height: 6})
	model.Update(previewLoadResult(t, model.Init()))

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	title := strings.TrimSpace(lines[0])
	if !strings.HasSuffix(title, "/file.txt") {
		t.Fatalf("title = %q, want the file path tail", title)
	}
	body := model.bodyHeight()
	markerRow := 1 + headerDividerHeight(model.height) + body - 1
	if !strings.Contains(lines[markerRow], "truncated (2 MiB limit)") {
		t.Fatalf("body marker = %q, want truncated marker", lines[markerRow])
	}
	if got := strings.TrimRight(lines[len(lines)-1], " "); got != " w wrap    s spaces    space copy    r reload    q close" {
		t.Fatalf("footer = %q, want preview shortcuts", got)
	}
}

func TestPreviewFooterShowsLoadingThenReadyAndWarning(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("x")}
	model := NewPreviewModel("/abs/s.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 70, Height: 6})

	if got := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " "); got != " "+previewLoadingStatus {
		t.Fatalf("loading footer = %q, want %q", got, " "+previewLoadingStatus)
	}
	model.Update(previewLoadResult(t, model.Init()))
	if got := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " "); got != " w wrap    s spaces    space copy    r reload    q close" {
		t.Fatalf("ready footer = %q, want shortcuts", got)
	}

	errorReader := &fakePreviewReader{err: errors.New("boom")}
	errorModel := NewPreviewModel("/abs/missing", nil, "", errorReader)
	errorModel.Update(tea.WindowSizeMsg{Width: 50, Height: 6})
	errorModel.Update(previewLoadResult(t, errorModel.Init()))
	if got := strings.TrimRight(ansi.Strip(strings.Split(errorModel.View().Content, "\n")[5]), " "); !strings.HasPrefix(got, " Warning: boom") {
		t.Fatalf("warning footer = %q, want Warning prefix", got)
	}
}

func TestPreviewMouseWheelAndScrollbarDrag(t *testing.T) {
	reader := &fakePreviewReader{content: bytesRepeatContent(100)}
	model := NewPreviewModel("/abs/wheel.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	startY := model.bodyStartY()
	model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelDown})
	if model.offset != mouseWheelScrollLines {
		t.Fatalf("wheel-down offset = %d, want %d", model.offset, mouseWheelScrollLines)
	}
	model.Update(tea.MouseWheelMsg{X: 0, Y: startY, Button: tea.MouseWheelUp})
	if model.offset != 0 {
		t.Fatalf("wheel-up offset = %d, want 0", model.offset)
	}
	model.Update(tea.MouseWheelMsg{X: 0, Y: startY - 1, Button: tea.MouseWheelDown})
	if model.offset != 0 {
		t.Fatalf("outside-body wheel changed offset to %d, want 0", model.offset)
	}

	model.offset = 0
	model.Update(tea.MouseClickMsg{X: model.width - 1, Y: startY + 1, Button: tea.MouseLeft})
	if model.dragMode != previewDragVScroll {
		t.Fatal("vertical scrollbar click did not start a drag")
	}
	model.Update(tea.MouseMotionMsg{X: model.width - 1, Y: startY + model.bodyHeight(), Button: tea.MouseLeft})
	if model.offset != model.maxVerticalOffset() {
		t.Fatalf("vertical drag offset = %d, want %d", model.offset, model.maxVerticalOffset())
	}
	model.Update(tea.MouseReleaseMsg{X: model.width - 1, Y: startY + model.bodyHeight(), Button: tea.MouseLeft})
	if model.dragMode != previewDragNone {
		t.Fatal("vertical release left dragging enabled")
	}
}

func TestPreviewHorizontalScrollbarRendersAndDrags(t *testing.T) {
	reader := &fakePreviewReader{content: []byte(strings.Repeat("abcdefghij", 30))}
	model := NewPreviewModel("/abs/wide.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 26, Height: 7})
	model.Update(previewLoadResult(t, model.Init()))

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	hbarRow := model.horizontalBarY()
	bar := lines[hbarRow]
	if !strings.Contains(bar, previewHorizontalThumbGlyph) || !strings.Contains(bar, previewHorizontalTrackGlyph) {
		t.Fatalf("horizontal bar = %q, want track and thumb", bar)
	}

	model.Update(tea.MouseClickMsg{X: 1, Y: hbarRow, Button: tea.MouseLeft})
	if model.dragMode != previewDragHScroll {
		t.Fatal("horizontal bar click did not start a drag")
	}
	model.Update(tea.MouseMotionMsg{X: model.width - 1, Y: hbarRow, Button: tea.MouseLeft})
	if model.xoffset != model.maxHorizontalOffset() {
		t.Fatalf("horizontal drag xoffset = %d, want %d", model.xoffset, model.maxHorizontalOffset())
	}
	model.Update(tea.MouseReleaseMsg{X: model.width - 1, Y: hbarRow, Button: tea.MouseLeft})
	if model.dragMode != previewDragNone {
		t.Fatal("horizontal release left dragging enabled")
	}

	// Clicking the bar track while wrap is on must stay inert.
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	model.Update(tea.MouseClickMsg{X: 1, Y: model.horizontalBarY(), Button: tea.MouseLeft})
	if model.dragMode == previewDragHScroll {
		t.Fatal("wrap mode started a horizontal drag")
	}
}

func TestPreviewRenderingStaysWithinCellWidth(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("日本語の長い行\tと絵文字🙂\r\nsecond line")}
	for _, wrap := range []bool{false, true} {
		model := NewPreviewModel("/abs/wide-jp.txt", nil, "", reader)
		model.Update(previewLoadResult(t, model.Init()))
		if wrap {
			model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
		} else {
			model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyEnd})
			for range model.maxHorizontalOffset() {
				model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyRight})
			}
		}
		for width := 4; width <= 30; width += 2 {
			model.Update(tea.WindowSizeMsg{Width: width, Height: 8})
			content := ansi.Strip(model.View().Content)
			for lineIndex, line := range strings.Split(content, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("wrap %v width %d line %d cell width = %d, want <= %d: %q", wrap, width, lineIndex, got, width, line)
				}
				if strings.ContainsAny(line, "\x00\x1b\n\r\t") {
					t.Fatalf("wrap %v width %d line %d contains terminal controls: %q", wrap, width, lineIndex, line)
				}
			}
		}
	}
}

func TestPreviewCopySelectionWithoutSelectionShowsNoSelectionToast(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("first\nsecond")}
	model := NewPreviewModel("/abs/copy.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	if !model.selection.empty() {
		t.Fatalf("fresh model selection = %#v, want empty", model.selection)
	}

	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := model.toast; got != previewNoSelectionStatus {
		t.Fatalf("toast = %q, want %q", got, previewNoSelectionStatus)
	}
	if model.status != model.readyStatus() {
		t.Fatalf("empty copy changed persistent status to %q, want %q", model.status, model.readyStatus())
	}
	// The only command is the toast timer; nothing is put on the clipboard.
	timeout, ok := cmd().(previewToastTimeoutMsg)
	if !ok {
		t.Fatalf("space without selection command message = %T, want previewToastTimeoutMsg", cmd())
	}
	model.Update(timeout)
	if model.toast != "" {
		t.Fatalf("toast = %q, want cleared after timeout", model.toast)
	}
}

func TestPreviewCopySelectionCommandsClipboardAndKeepsHighlight(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("first\nsecond\nthird")}
	model := NewPreviewModel("/abs/copy.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	selection := previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 1, col: 4},
	}
	model.selection = selection
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := clipboardText(t, cmd); got != "irst\nseco" {
		t.Fatalf("copied text = %q, want %q", got, "irst\nseco")
	}
	if want := "Copied 9 chars (2 lines)"; model.toast != want {
		t.Fatalf("toast = %q, want %q", model.toast, want)
	}
	if model.status != model.readyStatus() {
		t.Fatalf("copy changed persistent status to %q, want %q", model.status, model.readyStatus())
	}
	if model.selection != selection {
		t.Fatalf("copy changed selection to %#v, want kept %#v", model.selection, selection)
	}

	// A second space re-copies the same selection.
	_, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := clipboardText(t, cmd); got != "irst\nseco" {
		t.Fatalf("second copy text = %q, want %q", got, "irst\nseco")
	}
}

func TestPreviewCopySelectionAcceptsFullWidthSpace(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("first\nsecond")}
	model := NewPreviewModel("/abs/copy.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	model.selection = previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 3},
	}

	_, cmd := model.Update(tea.KeyPressMsg{Code: '　', Text: "　"})
	if got := clipboardText(t, cmd); got != "ir" {
		t.Fatalf("full-width space copied text = %q, want %q", got, "ir")
	}
	if want := "Copied 2 chars"; model.toast != want {
		t.Fatalf("toast = %q, want %q", model.toast, want)
	}
}

func TestPreviewCopySelectionSingleLineStatusCountsRunes(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("日本語a\nsecond")}
	model := NewPreviewModel("/abs/copy.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	// CJK runes are three bytes each; the count must be in runes, not bytes.
	model.selection = previewSelection{
		anchor: previewPosition{line: 0, col: 0},
		focus:  previewPosition{line: 0, col: 8},
	}
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := clipboardText(t, cmd); got != "日本語a" {
		t.Fatalf("copied text = %q, want %q", got, "日本語a")
	}
	if want := "Copied 4 chars"; model.toast != want {
		t.Fatalf("toast = %q, want %q", model.toast, want)
	}
}

func TestPreviewCopyStatusFormatsRuneAndLineCounts(t *testing.T) {
	single := previewSelection{anchor: previewPosition{line: 0, col: 1}, focus: previewPosition{line: 0, col: 4}}
	if got, want := previewCopyStatus("abc", single), "Copied 3 chars"; got != want {
		t.Fatalf("single-line copyStatus() = %q, want %q", got, want)
	}
	multi := previewSelection{anchor: previewPosition{line: 0, col: 0}, focus: previewPosition{line: 2, col: 1}}
	if got, want := previewCopyStatus("a\nb\nc", multi), "Copied 5 chars (3 lines)"; got != want {
		t.Fatalf("multi-line copyStatus() = %q, want %q", got, want)
	}
	if got := previewCopyStatus("", previewSelection{}); got != "" {
		t.Fatalf("empty copyStatus() = %q, want empty", got)
	}
}

func TestPreviewCopyOnUnsupportedCategoryReportsNoSelection(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("PK\x03\x04")}
	model := NewPreviewModel("/abs/bundle.zip", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := model.toast; got != previewNoSelectionStatus {
		t.Fatalf("toast = %q, want %q", got, previewNoSelectionStatus)
	}
	if model.status != model.readyStatus() {
		t.Fatalf("unsupported copy changed persistent status to %q, want %q", model.status, model.readyStatus())
	}
	if _, ok := cmd().(previewToastTimeoutMsg); !ok {
		t.Fatalf("space on unsupported category command message = %T, want previewToastTimeoutMsg", cmd())
	}
}

func TestPreviewCopySelectionIsIndependentOfWrapAndXOffset(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("aaaa bbbb cccc\ndddd eeee")}
	model := NewPreviewModel("/abs/wrapcopy.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 18, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyRight})
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyRight})
	if model.wrap || model.xoffset == 0 {
		t.Fatalf("setup = wrap %v xoffset %d, want wrap off and scrolled right", model.wrap, model.xoffset)
	}
	selection := previewSelection{
		anchor: previewPosition{line: 0, col: 2},
		focus:  previewPosition{line: 1, col: 4},
	}
	model.selection = selection

	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	unwrapped := clipboardText(t, cmd)

	// Toggling wrap clears the selection; the same content selection stays
	// valid in original coordinates and must extract identically.
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	model.selection = selection
	_, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	wrapped := clipboardText(t, cmd)
	if wrapped != unwrapped {
		t.Fatalf("wrapped copy = %q, unwrapped = %q; extraction must not depend on wrap", wrapped, unwrapped)
	}
	if want := "aa bbbb cccc\ndddd"; wrapped != want {
		t.Fatalf("copied text = %q, want %q", wrapped, want)
	}
	if want := "Copied 17 chars (2 lines)"; model.toast != want {
		t.Fatalf("toast = %q, want %q", model.toast, want)
	}
}

func TestPreviewCopyToastTimesOutAndFooterReturnsToHelp(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("first\nsecond")}
	model := NewPreviewModel("/abs/copy.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 70, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	model.selection = previewSelection{
		anchor: previewPosition{line: 0, col: 1},
		focus:  previewPosition{line: 0, col: 3},
	}

	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	timeout := toastTimeoutOf(t, cmd)
	footer := func() string {
		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		return strings.TrimRight(lines[len(lines)-1], " ")
	}
	if got := footer(); got != " Copied 2 chars" {
		t.Fatalf("footer while toast visible = %q, want %q", got, " Copied 2 chars")
	}
	if strings.Contains(footer(), "space copy") {
		t.Fatal("help row is visible while the toast is showing")
	}

	model.Update(timeout)
	if model.toast != "" {
		t.Fatalf("toast = %q, want cleared after timeout", model.toast)
	}
	if got := footer(); got != " w wrap    s spaces    space copy    r reload    q close" {
		t.Fatalf("footer after timeout = %q, want shortcuts", got)
	}
}

func TestPreviewFooterToastOutranksStatusAndHelp(t *testing.T) {
	shortenPreviewToast(t)
	reader := &fakePreviewReader{content: []byte("first\nsecond")}
	model := NewPreviewModel("/abs/copy.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 70, Height: 6})
	model.Update(previewLoadResult(t, model.Init()))

	footer := func() string {
		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		return strings.TrimRight(lines[len(lines)-1], " ")
	}
	if got := footer(); got != " w wrap    s spaces    space copy    r reload    q close" {
		t.Fatalf("ready footer = %q, want shortcuts", got)
	}

	model.selection = previewSelection{
		anchor: previewPosition{line: 0, col: 0},
		focus:  previewPosition{line: 0, col: 2},
	}
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	timeout := toastTimeoutOf(t, cmd)
	if got := footer(); got != " Copied 2 chars" {
		t.Fatalf("toast footer = %q, want %q", got, " Copied 2 chars")
	}
	if strings.Contains(footer(), "space copy") {
		t.Fatal("help row is visible while the toast is showing")
	}

	// A persistent status is lower priority than the toast: it stays hidden
	// until the toast times out.
	model.status = "Error: boom"
	if got := footer(); got != " Copied 2 chars" {
		t.Fatalf("toast footer with non-ready status = %q, want %q", got, " Copied 2 chars")
	}
	model.Update(timeout)
	if got := footer(); got != " Error: boom" {
		t.Fatalf("footer after timeout = %q, want the status line", got)
	}

	model.status = model.readyStatus()
	if got := footer(); got != " w wrap    s spaces    space copy    r reload    q close" {
		t.Fatalf("help footer when ready = %q, want shortcuts", got)
	}
}

// shortenPreviewToast shortens the toast display time so running the timer
// command in tests is fast and deterministic.
func shortenPreviewToast(t testing.TB) {
	t.Helper()
	previousDuration := previewToastDuration
	previewToastDuration = time.Millisecond
	t.Cleanup(func() { previewToastDuration = previousDuration })
}

// copyMessages executes a copy command and returns every produced message,
// unwrapping the toast/clipboard batch.
func copyMessages(t testing.TB, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a copy command, got nil")
	}
	message := cmd()
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{message}
	}
	messages := make([]tea.Msg, 0, len(batch))
	for _, command := range batch {
		messages = append(messages, command())
	}
	return messages
}

// toastTimeoutOf executes a copy command and returns its toast timer
// message.
func toastTimeoutOf(t testing.TB, cmd tea.Cmd) previewToastTimeoutMsg {
	t.Helper()
	for _, message := range copyMessages(t, cmd) {
		if timeout, ok := message.(previewToastTimeoutMsg); ok {
			return timeout
		}
	}
	t.Fatalf("copy command produced no toast timer: %v", cmd())
	return previewToastTimeoutMsg{}
}

// clipboardText runs a clipboard command and returns the copied text.
// setClipboardMsg is unexported, so the payload is read via reflection.
// Copy feedback batches the clipboard command with the toast timer.
func clipboardText(t testing.TB, cmd tea.Cmd) string {
	t.Helper()
	for _, message := range copyMessages(t, cmd) {
		value := reflect.ValueOf(message)
		if value.Kind() == reflect.String {
			return value.String()
		}
	}
	t.Fatalf("copy command produced no clipboard message: %v", cmd())
	return ""
}

func TestPreviewQuitsOnQAndCtrlC(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("x")}
	model := NewPreviewModel("/abs/q.txt", nil, "", reader)
	model.Update(previewLoadResult(t, model.Init()))
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		_, cmd := model.Update(key)
		if cmd == nil {
			t.Fatalf("Update(%q) returned nil command", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("Update(%q) command = %T, want tea.QuitMsg", key.String(), cmd())
		}
	}
	if _, cmd := model.Update(tea.InterruptMsg{}); cmd == nil {
		t.Fatal("InterruptMsg returned nil command")
	}
}

func TestPreviewUnassignedKeysAreInert(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("x")}
	model := NewPreviewModel("/abs/q.txt", nil, "", reader)
	model.Update(previewLoadResult(t, model.Init()))
	offset, xoffset, wrap := model.offset, model.xoffset, model.wrap
	for _, key := range []tea.KeyPressMsg{
		{Code: 'a', Text: "a"},
		{Code: 'z', Text: "z"},
		{Code: 'y', Text: "y"},
	} {
		if _, cmd := model.Update(key); cmd != nil {
			t.Fatalf("Update(%q) returned command %v, want nil", key.String(), cmd)
		}
	}
	if model.offset != offset || model.xoffset != xoffset || model.wrap != wrap {
		t.Fatalf("unassigned keys changed state: offset %d/%d xoffset %d/%d wrap %v/%v", model.offset, offset, model.xoffset, xoffset, model.wrap, wrap)
	}
}

func TestPreviewVerticalScrollbarRendersInBodyRows(t *testing.T) {
	reader := &fakePreviewReader{content: bytesRepeatContent(50)}
	model := NewPreviewModel("/abs/scroll.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 30, Height: 7})
	model.Update(previewLoadResult(t, model.Init()))

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	startY := model.bodyStartY()
	body := model.bodyHeight()
	for row := 0; row < body; row++ {
		line := lines[startY+row]
		if !strings.HasSuffix(line, scrollbarTrackGlyph) && !strings.HasSuffix(line, scrollbarThumbGlyph) {
			t.Fatalf("body row %d = %q, want scrollbar glyph", row, line)
		}
	}
}

func TestPreviewGutterWidthFollowsLineCountDigits(t *testing.T) {
	for _, test := range []struct {
		lines int
		width int
	}{
		{lines: 0, width: 2},
		{lines: 9, width: 2},
		{lines: 10, width: 2},
		{lines: 99, width: 2},
		{lines: 100, width: 3},
		{lines: 123, width: 3},
	} {
		model := PreviewModel{lineCount: test.lines}
		if got := model.gutterWidth(); got != test.width {
			t.Errorf("gutterWidth for %d lines = %d, want %d", test.lines, got, test.width)
		}
	}
}

// UpdateKeyPreview keeps key-driven preview tests focused on state.
func (m *PreviewModel) UpdateKeyPreview(key tea.KeyPressMsg) {
	_, _ = m.Update(key)
}

func previewLoadResult(t testing.TB, cmd tea.Cmd) previewLoadMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Init() returned nil command")
	}

	message := cmd()
	if result, ok := message.(previewLoadMsg); ok {
		return result
	}
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init() message = %T, want previewLoadMsg or tea.BatchMsg", message)
	}
	for _, command := range batch {
		if result, ok := command().(previewLoadMsg); ok {
			return result
		}
	}
	t.Fatalf("Init() batch contains no previewLoadMsg")
	return previewLoadMsg{}
}
