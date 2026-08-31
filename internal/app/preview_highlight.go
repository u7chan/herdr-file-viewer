package app

import (
	"math"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const (
	previewDarkSyntaxTheme  = "github-dark"
	previewLightSyntaxTheme = "github"
)

// previewSyntaxSpan maps a token type to a cell range in its original line.
// Keeping the range separate from the token text lets wrapping and selection
// continue to use the already-sanitized previewLine text as their source of
// truth.
type previewSyntaxSpan struct {
	start int
	end   int
	token chroma.TokenType
}

// highlightPreviewLines applies best-effort syntax metadata to display lines.
// A lexer or tokenizer failure returns the original lines unchanged so
// highlighting can never make an otherwise readable preview fail.
func highlightPreviewLines(path string, lines []previewLine) (result []previewLine) {
	result = lines
	defer func() {
		if recover() != nil {
			result = lines
		}
	}()

	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		return lines
	}

	source := previewSourceForLines(lines)
	tokens, ok := previewTokensForLexer(lexer, source)
	if !ok {
		return lines
	}
	spans, ok := previewSyntaxSpansForTokens(lines, tokens)
	if !ok {
		return lines
	}

	result = make([]previewLine, len(lines))
	copy(result, lines)
	for index := range result {
		result[index].spans = spans[index]
	}
	return result
}

func previewSourceForLines(lines []previewLine) string {
	if len(lines) == 0 {
		return ""
	}
	var source strings.Builder
	source.Grow(len(lines) * 2)
	for index, line := range lines {
		if index > 0 {
			source.WriteByte('\n')
		}
		source.WriteString(line.text)
	}
	return source.String()
}

// previewTokensForLexer validates the tokenizer output before it is converted
// to spans. Chroma lexers can panic while iterating, so the fallback boundary
// also covers iterator panics as well as ordinary errors.
func previewTokensForLexer(lexer chroma.Lexer, source string) (tokens []chroma.Token, ok bool) {
	defer func() {
		if recover() != nil {
			tokens = nil
			ok = false
		}
	}()

	tokens, err := chroma.Tokenise(lexer, &chroma.TokeniseOptions{
		State:    "root",
		EnsureLF: false,
	}, source)
	if err != nil {
		return nil, false
	}

	var reconstructed strings.Builder
	reconstructed.Grow(len(source))
	for _, token := range tokens {
		reconstructed.WriteString(token.Value)
	}
	if reconstructed.String() != source {
		return nil, false
	}
	return tokens, true
}

func previewSyntaxSpansForTokens(lines []previewLine, tokens []chroma.Token) ([][]previewSyntaxSpan, bool) {
	if len(lines) == 0 {
		return nil, len(tokens) == 0
	}

	spans := make([][]previewSyntaxSpan, len(lines))
	lineIndex := 0
	column := 0
	for _, token := range tokens {
		value := token.Value
		for len(value) > 0 {
			if lineIndex >= len(lines) {
				return nil, false
			}

			newline := strings.IndexByte(value, '\n')
			piece := value
			if newline >= 0 {
				piece = value[:newline]
			}
			if piece != "" {
				width := lipgloss.Width(piece)
				if width > 0 {
					spans[lineIndex] = appendPreviewSyntaxSpan(
						spans[lineIndex], column, column+width, token.Type,
					)
				}
				column += width
			}

			if newline < 0 {
				break
			}
			lineIndex++
			column = 0
			value = value[newline+1:]
		}
	}
	return spans, true
}

func appendPreviewSyntaxSpan(spans []previewSyntaxSpan, start, end int, token chroma.TokenType) []previewSyntaxSpan {
	if end <= start {
		return spans
	}
	if len(spans) > 0 {
		last := &spans[len(spans)-1]
		if last.end == start && last.token == token {
			last.end = end
			return spans
		}
	}
	return append(spans, previewSyntaxSpan{start: start, end: end, token: token})
}

// previewSyntaxStyle resolves a cached token type for the current terminal
// palette. Only foreground and text attributes are retained; Chroma's theme
// background never replaces the pane's existing background.
func previewSyntaxStyle(token chroma.TokenType, lightBackground bool) lipgloss.Style {
	style := lipgloss.NewStyle().Inline(true)
	if token == chroma.EOFType || token == chroma.None || token == chroma.Ignore {
		return style
	}

	theme := previewDarkSyntaxTheme
	if lightBackground {
		theme = previewLightSyntaxTheme
	}
	entry := styles.Get(theme).Get(token)
	if entry.Colour.IsSet() {
		style = style.Foreground(lipgloss.ANSIColor(previewANSI256Index(entry.Colour)))
	}
	if entry.Bold == chroma.Yes {
		style = style.Bold(true)
	}
	if entry.Italic == chroma.Yes {
		style = style.Italic(true)
	}
	if entry.Underline == chroma.Yes {
		style = style.Underline(true)
	}
	return style
}

type previewANSIColour struct {
	index int
	value chroma.Colour
}

var previewANSI256Palette = buildPreviewANSI256Palette()

func buildPreviewANSI256Palette() []previewANSIColour {
	standard := [...]uint32{
		0x000000, 0x800000, 0x008000, 0x808000,
		0x000080, 0x800080, 0x008080, 0xc0c0c0,
		0x808080, 0xff0000, 0x00ff00, 0xffff00,
		0x0000ff, 0xff00ff, 0x00ffff, 0xffffff,
	}
	palette := make([]previewANSIColour, 0, 256)
	for index, value := range standard {
		palette = append(palette, previewANSIColour{
			index: index,
			value: chroma.NewColour(uint8(value>>16), uint8(value>>8), uint8(value)),
		})
	}

	levels := [...]uint8{0, 95, 135, 175, 215, 255}
	for red := range levels {
		for green := range levels {
			for blue := range levels {
				palette = append(palette, previewANSIColour{
					index: 16 + red*36 + green*6 + blue,
					value: chroma.NewColour(levels[red], levels[green], levels[blue]),
				})
			}
		}
	}
	for index := 232; index <= 255; index++ {
		level := uint8(8 + (index-232)*10)
		palette = append(palette, previewANSIColour{
			index: index,
			value: chroma.NewColour(level, level, level),
		})
	}
	return palette
}

// previewANSI256Index uses the same colour-distance metric as Chroma's TTY
// formatter and resolves ties by the stable ANSI palette order.
func previewANSI256Index(value chroma.Colour) int {
	bestIndex := 0
	bestDistance := math.MaxFloat64
	for _, candidate := range previewANSI256Palette {
		distance := value.Distance(candidate.value)
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = candidate.index
		}
	}
	return bestIndex
}
