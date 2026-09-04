package app

import (
	"bytes"
	"errors"
	"image/color"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

func TestHighlightPreviewLinesMatchesBaseNameLexers(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		token   chroma.TokenType
	}{
		{name: "Go", path: "/tmp/main.go", content: "package main\nfunc main() {}", token: chroma.KeywordDeclaration},
		{name: "JSON", path: "/tmp/config.json", content: `{"enabled": true}`, token: chroma.NameTag},
		{name: "Markdown", path: "/tmp/README.md", content: "# Heading\n", token: chroma.GenericHeading},
		{name: "Makefile", path: "/tmp/Makefile", content: "all:\n    echo ok\n", token: chroma.NameFunction},
		{name: "Dockerfile", path: "/tmp/Dockerfile", content: "FROM alpine\n", token: chroma.Keyword},
		{name: "Bash rc", path: "/tmp/.bashrc", content: "if true; then\nfi\n", token: chroma.Keyword},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lines := highlightPreviewLines(test.path, previewTextLines([]byte(test.content)))
			if !hasPreviewToken(lines, test.token) {
				t.Fatalf("highlighted lines = %#v, want token %s", lines, test.token)
			}
			if got := previewSourceForLines(lines); got != strings.ReplaceAll(test.content, "\r\n", "\n") {
				t.Fatalf("highlighted source = %q, want %q", got, test.content)
			}
		})
	}
}

func TestHighlightPreviewLinesFallsBackToPlainText(t *testing.T) {
	for _, test := range []struct {
		name    string
		path    string
		content string
	}{
		{name: "unknown extension", path: "/tmp/file.unknown", content: "plain text"},
		{name: "unknown extension with controls", path: "/tmp/file.unknown", content: "a\x1b[31mb"},
		{name: "empty recognized file", path: "/tmp/empty.go", content: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			plain := previewTextLines([]byte(test.content))
			got := highlightPreviewLines(test.path, plain)
			if !reflect.DeepEqual(got, plain) {
				t.Fatalf("fallback lines = %#v, want plain lines %#v", got, plain)
			}
		})
	}
}

func TestPreviewHighlightByteCapHighlightsAtOrBelowThreshold(t *testing.T) {
	for _, size := range []int{previewHighlightMaxBytes - 1, previewHighlightMaxBytes} {
		t.Run(strconv.Itoa(size)+" bytes", func(t *testing.T) {
			reader := &fakePreviewReader{content: previewHighlightTestContent(size)}
			model := NewPreviewModel("/tmp/large.go", nil, "", reader)
			message := previewLoadResult(t, model.Init())
			if message.highlightSkipped {
				t.Fatalf("highlight skipped at %d bytes", size)
			}
			if !hasPreviewToken(message.lines, chroma.KeywordDeclaration) {
				t.Fatalf("lines at %d bytes = %#v, want Go syntax spans", size, message.lines[:2])
			}

			model.Update(message)
			if model.highlightSkipped || model.status != readyStatus {
				t.Fatalf("model at %d bytes = skipped %v, status %q; want highlighted and ready", size, model.highlightSkipped, model.status)
			}
		})
	}
}

func TestPreviewHighlightByteCapSkipsLargeContentAndShowsMutedFooterStatus(t *testing.T) {
	reader := &fakePreviewReader{content: previewHighlightTestContent(previewHighlightMaxBytes + 1)}
	model := NewPreviewModel("/tmp/large.go", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	message := previewLoadResult(t, model.Init())
	if !message.highlightSkipped {
		t.Fatal("large content did not skip highlighting")
	}
	if hasPreviewSyntaxSpans(message.lines) {
		t.Fatal("large content produced syntax spans")
	}

	model.Update(message)
	if !model.highlightSkipped || model.status != previewHighlightSkippedStatus {
		t.Fatalf("loaded model = skipped %v, status %q; want skipped status", model.highlightSkipped, model.status)
	}
	if hasPreviewSyntaxSpans(model.lines) {
		t.Fatal("loaded large content produced syntax spans")
	}
	if len(model.displayLines) != len(model.lines) {
		t.Fatalf("display lines = %d, want %d without a body marker", len(model.displayLines), len(model.lines))
	}

	view := model.View().Content
	rows := strings.Split(view, "\n")
	footer := rows[len(rows)-1]
	if got := strings.TrimRight(ansi.Strip(footer), " "); got != " "+previewHighlightSkippedStatus {
		t.Fatalf("footer = %q, want muted highlight status", got)
	}
	if !strings.Contains(footer, "38;5;240") {
		t.Fatalf("footer = %q, want muted foreground", footer)
	}
	for _, row := range rows[model.bodyStartY() : model.bodyStartY()+model.bodyHeight()] {
		if strings.Contains(ansi.Strip(row), previewHighlightSkippedStatus) {
			t.Fatalf("body row = %q, must not include a highlight marker", row)
		}
	}
}

func TestPreviewHighlightByteCapAppliesOnReload(t *testing.T) {
	reader := &fakePreviewReader{content: previewHighlightTestContent(previewHighlightMaxBytes)}
	model := NewPreviewModel("/tmp/reload.go", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	if model.highlightSkipped || !hasPreviewToken(model.lines, chroma.KeywordDeclaration) {
		t.Fatalf("initial content = skipped %v, lines %#v; want highlighted", model.highlightSkipped, model.lines[:2])
	}

	reader.content = previewHighlightTestContent(previewHighlightMaxBytes + 1)
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	message, ok := cmd().(previewLoadMsg)
	if !ok {
		t.Fatalf("reload message = %T, want previewLoadMsg", cmd())
	}
	if !message.reload || !message.highlightSkipped || hasPreviewSyntaxSpans(message.lines) {
		t.Fatalf("large reload message = %#v, want skipped plain content", message)
	}
	model.Update(message)
	if !model.highlightSkipped || model.status != previewHighlightSkippedStatus || hasPreviewSyntaxSpans(model.lines) {
		t.Fatalf("large reload = skipped %v, status %q, lines %#v; want skipped plain content", model.highlightSkipped, model.status, model.lines[:2])
	}

	model.Update(previewToastTimeoutMsg{seq: model.toastSeq})
	if footer := strings.TrimRight(ansi.Strip(model.renderFooter()), " "); footer != " "+previewHighlightSkippedStatus {
		t.Fatalf("footer after reload toast = %q, want highlight status", footer)
	}

	reader.content = previewHighlightTestContent(previewHighlightMaxBytes)
	_, cmd = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	message, ok = cmd().(previewLoadMsg)
	if !ok {
		t.Fatalf("second reload message = %T, want previewLoadMsg", cmd())
	}
	if message.highlightSkipped || !hasPreviewToken(message.lines, chroma.KeywordDeclaration) {
		t.Fatalf("small reload message = %#v, want highlighted content", message)
	}
	model.Update(message)
	if model.highlightSkipped || model.status != readyStatus || !hasPreviewToken(model.lines, chroma.KeywordDeclaration) {
		t.Fatalf("small reload = skipped %v, status %q, lines %#v; want highlighted and ready", model.highlightSkipped, model.status, model.lines[:2])
	}
}

func TestPreviewHighlightByteCapKeepsPreviewInvariants(t *testing.T) {
	reader := &fakePreviewReader{
		content:   previewHighlightWideTestContent(previewHighlightMaxBytes + 1),
		truncated: true,
	}
	model := NewPreviewModel("/tmp/large.go", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	if !model.highlightSkipped || !model.truncated || !model.showHorizontalScrollbar() {
		t.Fatalf("initial model = skipped %v, truncated %v, horizontal bar %v; want large plain preview", model.highlightSkipped, model.truncated, model.showHorizontalScrollbar())
	}
	if hasPreviewSyntaxSpans(model.lines) {
		t.Fatal("large content produced syntax spans")
	}
	marker := model.displayLines[len(model.displayLines)-1]
	if marker.text != previewTruncatedMarker || !marker.muted || marker.origin != -1 || len(marker.spans) != 0 {
		t.Fatalf("truncation marker = %#v, want muted marker without syntax spans", marker)
	}
	if rendered := model.renderContent(marker, model.contentWidth()); !strings.Contains(rendered, "38;5;240") {
		t.Fatalf("truncation marker = %q, want muted foreground", rendered)
	}

	lines := clonePreviewLines(model.lines)
	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	if !reflect.DeepEqual(model.lines, lines) || hasPreviewSyntaxSpans(model.lines) {
		t.Fatalf("theme switch changed plain lines: %#v", model.lines[:2])
	}

	selection := previewSelection{
		anchor: previewPosition{line: 2, col: 0},
		focus:  previewPosition{line: 2, col: 4},
	}
	model.selection = selection
	if got := extractSelection(model.lines, selection); got != "xxxx" {
		t.Fatalf("selection = %q, want plain content", got)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: tea.KeyRight})
	if model.xoffset == 0 {
		t.Fatal("horizontal scroll did not advance on large plain content")
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !model.wrap || model.xoffset != 0 || hasPreviewSyntaxSpans(model.displayLines) {
		t.Fatalf("wrapped large preview = wrap %v, xoffset %d, lines %#v; want plain wrapped content", model.wrap, model.xoffset, model.displayLines[:2])
	}
	model.selection = selection
	if got := extractSelection(model.lines, selection); got != "xxxx" {
		t.Fatalf("wrapped selection = %q, want plain content", got)
	}

	lines = clonePreviewLines(model.lines)
	displayLines := clonePreviewLines(model.displayLines)
	reader.err = errors.New("reload failed")
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model.Update(cmd())
	if !model.highlightSkipped || !strings.Contains(model.status, "Warning: reload failed") || !reflect.DeepEqual(model.lines, lines) || !reflect.DeepEqual(model.displayLines, displayLines) {
		t.Fatalf("failed reload changed skipped preview: skipped %v, status %q, lines %#v, display %#v", model.highlightSkipped, model.status, model.lines[:2], model.displayLines[:2])
	}

	unsupportedReader := &fakePreviewReader{content: previewHighlightTestContent(previewHighlightMaxBytes + 1)}
	unsupported := NewPreviewModel("/tmp/large.zip", nil, "", unsupportedReader)
	unsupported.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	unsupported.Update(previewLoadResult(t, unsupported.Init()))
	if unsupported.highlightSkipped || unsupported.status != readyStatus {
		t.Fatalf("unsupported model = skipped %v, status %q; want unchanged unsupported behavior", unsupported.highlightSkipped, unsupported.status)
	}
	unsupportedRenderedView := unsupported.View().Content
	unsupportedView := ansi.Strip(unsupportedRenderedView)
	if !strings.Contains(unsupportedView, "Unsupported preview: binary") || strings.Contains(unsupportedView, previewHighlightSkippedStatus) {
		t.Fatalf("unsupported view = %q, want muted binary label without highlight status", unsupportedView)
	}
	unsupportedBody := strings.Split(unsupportedRenderedView, "\n")[unsupported.bodyStartY()]
	if !strings.Contains(unsupportedBody, "38;5;240") {
		t.Fatalf("unsupported body = %q, want muted foreground", unsupportedBody)
	}
}

func TestPreviewTokensForLexerFailuresPreservePlainText(t *testing.T) {
	plain := previewTextLines([]byte("package main\nfunc main() {}"))
	source := previewSourceForLines(plain)
	for _, test := range []struct {
		name  string
		lexer *stubPreviewLexer
	}{
		{
			name: "tokenize error",
			lexer: &stubPreviewLexer{tokenize: func(*chroma.TokeniseOptions, string) (chroma.Iterator, error) {
				return nil, errors.New("tokenize failed")
			}},
		},
		{
			name: "token reconstruction mismatch",
			lexer: &stubPreviewLexer{tokenize: func(*chroma.TokeniseOptions, string) (chroma.Iterator, error) {
				return chroma.Literator(chroma.Token{Type: chroma.Text, Value: "different"}), nil
			}},
		},
		{
			name: "token iterator panic",
			lexer: &stubPreviewLexer{tokenize: func(*chroma.TokeniseOptions, string) (chroma.Iterator, error) {
				return func() chroma.Token { panic("tokenize iterator failed") }, nil
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tokens, ok := previewTokensForLexer(test.lexer, source)
			if ok || tokens != nil {
				t.Fatalf("previewTokensForLexer() = (%#v, %t), want (nil, false)", tokens, ok)
			}
			if got := previewSourceForLines(plain); got != source {
				t.Fatalf("plain source = %q, want %q", got, source)
			}
			if got := plain[0].spans; got != nil {
				t.Fatalf("plain spans = %#v, want nil", got)
			}
		})
	}
}

func TestPreviewSyntaxStyleUsesThemeForegroundWithoutBackground(t *testing.T) {
	for _, test := range []struct {
		name  string
		light bool
		token chroma.TokenType
	}{
		{name: "dark keyword", token: chroma.Keyword},
		{name: "light keyword", light: true, token: chroma.Keyword},
		{name: "dark comment", token: chroma.CommentSingle},
		{name: "light comment", light: true, token: chroma.CommentSingle},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := syntaxStyleEntry(test.token, test.light)
			color := previewANSI256Index(entry.Colour)
			rendered := previewSyntaxStyle(test.token, test.light).Render("token")
			if !strings.Contains(rendered, "38;5;"+strconv.Itoa(color)) {
				t.Fatalf("rendered style = %q, want ANSI foreground %d", rendered, color)
			}
			if strings.Contains(rendered, "48;") {
				t.Fatalf("rendered style = %q, must not apply theme background", rendered)
			}
		})
	}
}

func TestPreviewThemeSwitchKeepsTokenSpansAndPlainView(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("package main\nfunc main() {}")}
	model := NewPreviewModel("/tmp/main.go", nil, "", reader)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))
	if !hasPreviewToken(model.lines, chroma.KeywordDeclaration) {
		t.Fatalf("loaded lines = %#v, want Go token spans", model.lines)
	}
	spans := clonePreviewLines(model.lines)
	dark := model.View().Content
	model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	light := model.View().Content
	if !reflect.DeepEqual(model.lines, spans) {
		t.Fatalf("theme switch changed token spans: %#v, want %#v", model.lines, spans)
	}
	if dark == light {
		t.Fatalf("dark and light views are identical: %q", dark)
	}
	if got := ansi.Strip(light); !strings.Contains(got, "package main") || strings.Contains(got, "\x1b") {
		t.Fatalf("stripped light view = %q, want plain source without escapes", got)
	}
	if !strings.Contains(dark, "38;5;") || !strings.Contains(light, "38;5;") {
		t.Fatalf("views lack syntax foreground: dark %q light %q", dark, light)
	}
}

func TestPreviewSyntaxSelectionKeepsForegroundAndPlainCopy(t *testing.T) {
	lines := highlightPreviewLines("main.go", previewTextLines([]byte("package main")))
	line := lines[0]
	selection := previewSelection{
		anchor: previewPosition{line: 0, col: 0},
		focus:  previewPosition{line: 0, col: 7},
	}
	spans := previewSelectionSpans(line, selection, false)
	var selected previewTextSpan
	for _, span := range spans {
		if span.selected {
			selected = span
			break
		}
	}
	if !selected.selected || selected.text != "package" {
		t.Fatalf("selected spans = %#v, want selected package", spans)
	}
	rendered := previewSyntaxStyle(selected.token, false).Inherit(previewSelectionStyle(false)).Render(selected.text)
	if !strings.Contains(rendered, "38;5;") || !strings.Contains(rendered, "48;5;240") {
		t.Fatalf("selected rendered span = %q, want foreground and selection background", rendered)
	}
	if got := ansi.Strip(rendered); got != selected.text {
		t.Fatalf("selected stripped text = %q, want %q", got, selected.text)
	}
	if got := extractSelection(lines, selection); got != "package" {
		t.Fatalf("selection copy = %q, want package", got)
	}
}

func TestPreviewReloadUpdatesSyntaxAndKeepsFailedContent(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("package main")}
	model := NewPreviewModel("/tmp/reload.go", nil, "", reader)
	model.Update(previewLoadResult(t, model.Init()))
	if !hasPreviewToken(model.lines, chroma.KeywordNamespace) {
		t.Fatalf("initial lines = %#v, want package token", model.lines)
	}
	reader.content = []byte("func main() {}")
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model.Update(cmd())
	if !hasPreviewToken(model.lines, chroma.KeywordDeclaration) {
		t.Fatalf("reloaded lines = %#v, want func token", model.lines)
	}
	lines := clonePreviewLines(model.lines)
	reader.err = errors.New("reload failed")
	_, cmd = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model.Update(cmd())
	if !reflect.DeepEqual(model.lines, lines) {
		t.Fatalf("failed reload lines = %#v, want %#v", model.lines, lines)
	}
}

func hasPreviewToken(lines []previewLine, want chroma.TokenType) bool {
	for _, line := range lines {
		for _, span := range line.spans {
			if span.token == want {
				return true
			}
		}
	}
	return false
}

func hasPreviewSyntaxSpans(lines []previewLine) bool {
	for _, line := range lines {
		if len(line.spans) > 0 {
			return true
		}
	}
	return false
}

func previewHighlightTestContent(size int) []byte {
	const prefix = "package main\nfunc main() {}\n"
	if size < len(prefix) {
		panic("preview highlight test content is smaller than its prefix")
	}
	content := []byte(prefix)
	for len(content)+len("// x\n") <= size {
		content = append(content, "// x\n"...)
	}
	return append(content, bytes.Repeat([]byte(" "), size-len(content))...)
}

func previewHighlightWideTestContent(size int) []byte {
	const prefix = "package main\nfunc main() {}\n"
	wideLine := strings.Repeat("x", 80) + "\n"
	if size < len(prefix)+len(wideLine) {
		panic("preview highlight test content is smaller than its prefix")
	}
	content := append([]byte(prefix), wideLine...)
	for len(content)+len("// x\n") <= size {
		content = append(content, "// x\n"...)
	}
	return append(content, bytes.Repeat([]byte(" "), size-len(content))...)
}

func syntaxStyleEntry(token chroma.TokenType, light bool) chroma.StyleEntry {
	theme := previewDarkSyntaxTheme
	if light {
		theme = previewLightSyntaxTheme
	}
	return styles.Get(theme).Get(token)
}

func clonePreviewLines(lines []previewLine) []previewLine {
	cloned := make([]previewLine, len(lines))
	for index, line := range lines {
		cloned[index] = line
		cloned[index].spans = append([]previewSyntaxSpan(nil), line.spans...)
	}
	return cloned
}

type stubPreviewLexer struct {
	tokenize func(*chroma.TokeniseOptions, string) (chroma.Iterator, error)
}

func (l *stubPreviewLexer) Config() *chroma.Config {
	return &chroma.Config{Name: "stub"}
}

func (l *stubPreviewLexer) Tokenise(options *chroma.TokeniseOptions, text string) (chroma.Iterator, error) {
	return l.tokenize(options, text)
}

func (l *stubPreviewLexer) SetRegistry(*chroma.LexerRegistry) chroma.Lexer {
	return l
}

func (l *stubPreviewLexer) SetAnalyser(func(string) float32) chroma.Lexer {
	return l
}

func (*stubPreviewLexer) AnalyseText(string) float32 {
	return 0
}
