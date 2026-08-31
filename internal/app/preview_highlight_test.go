package app

import (
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
