package app

import (
	"errors"
	"strings"
	"testing"

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
		if display[index] != want[index] {
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
	if cmd := model.Init(); cmd != nil {
		t.Fatal("Init() returned a command for an unset file")
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
	message, ok := cmd().(previewLoadMsg)
	if !ok {
		t.Fatalf("Init() message = %T, want previewLoadMsg", cmd())
	}
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
	model.Update(model.Init()().(previewLoadMsg))
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
	model.Update(model.Init()().(previewLoadMsg))

	content := ansi.Strip(model.View().Content)
	lines := strings.Split(content, "\n")
	bodyStart := 1 + headerDividerHeight(model.height)
	if !strings.Contains(lines[bodyStart], "Unsupported preview: binary") {
		t.Fatalf("body = %q, want unsupported binary label", lines[bodyStart])
	}
	if strings.Contains(content, "PK") {
		t.Fatalf("view = %q, must not show binary content", content)
	}
	if got := strings.TrimRight(lines[len(lines)-1], " "); got != " w wrap    q close" {
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
	model.Update(model.Init()().(previewLoadMsg))

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
	model.Update(model.Init()().(previewLoadMsg))

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
	model.Update(model.Init()().(previewLoadMsg))

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

func TestPreviewRendersGutterNumbersAndBlankContinuations(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("one\nalphabetabcdefghij\nxyz")}
	model := NewPreviewModel("/abs/multi.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 16, Height: 8})
	model.Update(model.Init()().(previewLoadMsg)) // "alphabetabcdefghij" wraps at the 10-cell content width
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
				model.Update(model.Init()().(previewLoadMsg))
				return model
			}(),
		},
		{
			name: "unsupported",
			model: func() *PreviewModel {
				reader := &fakePreviewReader{content: []byte("PK\x03\x04")}
				model := NewPreviewModel("/abs/bundle.zip", nil, "", reader)
				model.Update(model.Init()().(previewLoadMsg))
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
	model.Update(tea.WindowSizeMsg{Width: 32, Height: 6})
	model.Update(model.Init()().(previewLoadMsg))

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
	if got := strings.TrimRight(lines[len(lines)-1], " "); got != " w wrap    q close" {
		t.Fatalf("footer = %q, want preview shortcuts", got)
	}
}

func TestPreviewFooterShowsLoadingThenReadyAndWarning(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("x")}
	model := NewPreviewModel("/abs/s.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 6})

	if got := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " "); got != " "+previewLoadingStatus {
		t.Fatalf("loading footer = %q, want %q", got, " "+previewLoadingStatus)
	}
	model.Update(model.Init()().(previewLoadMsg))
	if got := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " "); got != " w wrap    q close" {
		t.Fatalf("ready footer = %q, want shortcuts", got)
	}

	errorReader := &fakePreviewReader{err: errors.New("boom")}
	errorModel := NewPreviewModel("/abs/missing", nil, "", errorReader)
	errorModel.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	errorModel.Update(errorModel.Init()().(previewLoadMsg))
	if got := strings.TrimRight(ansi.Strip(strings.Split(errorModel.View().Content, "\n")[5]), " "); !strings.HasPrefix(got, " Warning: boom") {
		t.Fatalf("warning footer = %q, want Warning prefix", got)
	}
}

func TestPreviewMouseWheelAndScrollbarDrag(t *testing.T) {
	reader := &fakePreviewReader{content: bytesRepeatContent(100)}
	model := NewPreviewModel("/abs/wheel.txt", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	model.Update(model.Init()().(previewLoadMsg))

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
	model.Update(model.Init()().(previewLoadMsg))

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
		model.Update(model.Init()().(previewLoadMsg))
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

func TestPreviewQuitsOnQAndCtrlC(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("x")}
	model := NewPreviewModel("/abs/q.txt", nil, "", reader)
	model.Update(model.Init()().(previewLoadMsg))
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
	model.Update(model.Init()().(previewLoadMsg))
	offset, xoffset, wrap := model.offset, model.xoffset, model.wrap
	for _, key := range []tea.KeyPressMsg{
		{Code: 'a', Text: "a"},
		{Code: 'z', Text: "z"},
		{Code: 'r', Text: "r"},
		{Code: ' ', Text: " "},
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
	model.Update(model.Init()().(previewLoadMsg))

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
