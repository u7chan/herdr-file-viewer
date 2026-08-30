package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

// previewPosition identifies a cell boundary in one of the original lines.
// line is zero-based and col is the cell width from the beginning of that
// line.
type previewPosition struct {
	line int
	col  int
}

// previewSelection keeps the two drag endpoints in content coordinates. The
// display-line coordinate is deliberately not stored because wrapping and
// resizing rebuild displayLines.
type previewSelection struct {
	anchor previewPosition
	focus  previewPosition
}

func (p previewPosition) before(other previewPosition) bool {
	return p.line < other.line || p.line == other.line && p.col < other.col
}

func (s previewSelection) empty() bool {
	return s.anchor == s.focus
}

// normalized returns the selection in document order without changing the
// endpoints kept for the next drag.
func (s previewSelection) normalized() previewSelection {
	if s.focus.before(s.anchor) {
		return previewSelection{anchor: s.focus, focus: s.anchor}
	}
	return s
}

// selectionRange returns the half-open cell range used by the renderer and
// extractor. Equal endpoints intentionally represent an empty selection.
func (s previewSelection) selectionRange() (previewPosition, previewPosition, bool) {
	if s.empty() {
		return previewPosition{}, previewPosition{}, false
	}
	normalized := s.normalized()
	return normalized.anchor, normalized.focus, true
}

// previewTextSpan is a terminal-safe text fragment and its selection state.
// The text is never split in the middle of a grapheme.
type previewTextSpan struct {
	text     string
	selected bool
}

// previewSelectionSpans splits one display line into at most three spans.
// Selection ranges are evaluated in original-line coordinates, so all wrap
// continuations of one line share the same selection interval.
func previewSelectionSpans(line previewLine, selection previewSelection) []previewTextSpan {
	spans := []previewTextSpan{{text: line.text}}
	if line.origin < 0 || selection.empty() {
		return spans
	}

	start, end, ok := selection.selectionRange()
	if !ok || line.origin < start.line || line.origin > end.line {
		return spans
	}

	lineStart := line.col
	lineEnd := lineStart + lipgloss.Width(line.text)
	selectedStart, selectedEnd := lineStart, lineEnd
	if start.line == end.line {
		selectedStart = start.col
		selectedEnd = end.col
	} else if line.origin == start.line {
		selectedStart = start.col
	} else if line.origin == end.line {
		selectedEnd = end.col
	}

	if selectedStart < lineStart {
		selectedStart = lineStart
	}
	if selectedEnd > lineEnd {
		selectedEnd = lineEnd
	}
	if selectedStart >= selectedEnd {
		return spans
	}

	firstSelectedByte, lastSelectedByte := -1, -1
	cellOffset := 0
	byteOffset := 0
	iter := graphemes.FromString(line.text)
	for iter.Next() {
		text := iter.Value()
		width := lipgloss.Width(text)
		absoluteStart := lineStart + cellOffset
		absoluteEnd := absoluteStart + width
		if absoluteStart >= selectedStart && absoluteEnd <= selectedEnd {
			if firstSelectedByte < 0 {
				firstSelectedByte = byteOffset
			}
			lastSelectedByte = byteOffset + len(text)
		}
		cellOffset += width
		byteOffset += len(text)
	}
	if firstSelectedByte < 0 {
		return spans
	}

	spans = make([]previewTextSpan, 0, 3)
	if firstSelectedByte > 0 {
		spans = append(spans, previewTextSpan{text: line.text[:firstSelectedByte]})
	}
	spans = append(spans, previewTextSpan{
		text:     line.text[firstSelectedByte:lastSelectedByte],
		selected: true,
	})
	if lastSelectedByte < len(line.text) {
		spans = append(spans, previewTextSpan{text: line.text[lastSelectedByte:]})
	}
	return spans
}

// previewVisibleSpans applies horizontal scrolling and the content width to
// already-classified spans. A grapheme straddling either viewport edge is
// dropped, matching the existing width helpers rather than splitting it.
func previewVisibleSpans(spans []previewTextSpan, offset, width int) []previewTextSpan {
	if width <= 0 {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + width
	visible := make([]previewTextSpan, 0, len(spans))
	cellOffset := 0
	for _, span := range spans {
		if cellOffset >= end {
			break
		}
		spanWidth := lipgloss.Width(span.text)
		if cellOffset+spanWidth <= offset {
			cellOffset += spanWidth
			continue
		}
		localStart := max(0, offset-cellOffset)
		localEnd := min(end-cellOffset, spanWidth)
		if clipped, ok := previewVisibleSpan(span, localStart, localEnd); ok {
			visible = append(visible, clipped)
		}
		cellOffset += spanWidth
	}
	return visible
}

// extractSelection is the future copy connection point. It only extracts
// sanitized text; it does not interact with a clipboard or terminal escape
// sequence.
func extractSelection(lines []previewLine, selection previewSelection) string {
	start, end, ok := selection.selectionRange()
	if !ok || len(lines) == 0 {
		return ""
	}
	if start.line < 0 || end.line < 0 || start.line >= len(lines) || end.line >= len(lines) {
		return ""
	}

	parts := make([]string, 0, end.line-start.line+1)
	for lineIndex := start.line; lineIndex <= end.line; lineIndex++ {
		lineStart := 0
		lineEnd := lipgloss.Width(lines[lineIndex].text)
		if lineIndex == start.line {
			lineStart = start.col
		}
		if lineIndex == end.line {
			lineEnd = end.col
		}
		parts = append(parts, previewSliceByCells(lines[lineIndex].text, lineStart, lineEnd))
	}
	return strings.Join(parts, "\n")
}

// previewPositionForMouse maps a body coordinate to an original-line
// position. It is pure so click handling can keep all state changes in
// Update. When clampY is true, y outside the body is pinned to the visible
// first or last row for an in-progress drag.
func previewPositionForMouse(displayLines []previewLine, offset, bodyStartY, bodyHeight, x, y, contentStartX, contentWidth, xoffset int, clampY bool) (previewPosition, bool) {
	if len(displayLines) == 0 || bodyHeight <= 0 {
		return previewPosition{}, false
	}

	localY := y - bodyStartY
	if clampY {
		if localY < 0 {
			localY = 0
		}
		if localY >= bodyHeight {
			localY = bodyHeight - 1
		}
	} else if localY < 0 || localY >= bodyHeight {
		return previewPosition{}, false
	}

	displayIndex := offset + localY
	if clampY {
		if displayIndex < 0 {
			displayIndex = 0
		}
		if displayIndex >= len(displayLines) {
			displayIndex = len(displayLines) - 1
		}
	} else if displayIndex < 0 || displayIndex >= len(displayLines) {
		return previewPosition{}, false
	}
	line := displayLines[displayIndex]
	if line.origin < 0 {
		if !clampY {
			return previewPosition{}, false
		}
		for distance := 1; distance < len(displayLines); distance++ {
			previous := displayIndex - distance
			if previous >= 0 && displayLines[previous].origin >= 0 {
				line = displayLines[previous]
				break
			}
			next := displayIndex + distance
			if next < len(displayLines) && displayLines[next].origin >= 0 {
				line = displayLines[next]
				break
			}
		}
		if line.origin < 0 {
			return previewPosition{}, false
		}
	}

	col := 0
	if x >= contentStartX {
		col = line.col
	}
	if contentWidth > 0 && x >= contentStartX {
		localX := x - contentStartX
		if localX >= contentWidth {
			localX = contentWidth - 1
		}
		if localX < 0 {
			localX = 0
		}
		if xoffset > 0 {
			localX += xoffset
		}
		lineWidth := lipgloss.Width(line.text)
		if localX > lineWidth {
			localX = lineWidth
		}
		col += localX
	}
	return previewPosition{line: line.origin, col: col}, true
}

type previewGrapheme struct {
	text       string
	start, end int
}

func previewGraphemePieces(value string) []previewGrapheme {
	iter := graphemes.FromString(value)
	pieces := make([]previewGrapheme, 0)
	cellOffset := 0
	for iter.Next() {
		text := iter.Value()
		width := lipgloss.Width(text)
		pieces = append(pieces, previewGrapheme{
			text:  text,
			start: cellOffset,
			end:   cellOffset + width,
		})
		cellOffset += width
	}
	return pieces
}

func previewSliceByCells(value string, start, end int) string {
	if end <= start {
		return ""
	}
	if start < 0 {
		start = 0
	}
	width := lipgloss.Width(value)
	if end > width {
		end = width
	}
	if start >= end {
		return ""
	}

	var result strings.Builder
	for _, grapheme := range previewGraphemePieces(value) {
		if grapheme.start >= start && grapheme.end <= end {
			result.WriteString(grapheme.text)
		}
	}
	return result.String()
}

func previewVisibleSpan(span previewTextSpan, start, end int) (previewTextSpan, bool) {
	if end <= start {
		return previewTextSpan{}, false
	}

	firstByte, lastByte := -1, -1
	cellOffset := 0
	byteOffset := 0
	iter := graphemes.FromString(span.text)
	for iter.Next() {
		text := iter.Value()
		width := lipgloss.Width(text)
		if cellOffset >= end {
			break
		}
		finish := cellOffset + width
		if finish <= start {
			cellOffset = finish
			byteOffset += len(text)
			continue
		}
		if cellOffset >= start && finish <= end {
			if firstByte < 0 {
				firstByte = byteOffset
			}
			lastByte = byteOffset + len(text)
		}
		cellOffset = finish
		byteOffset += len(text)
	}
	if firstByte < 0 {
		return previewTextSpan{}, false
	}
	return previewTextSpan{
		text:     span.text[firstByte:lastByte],
		selected: span.selected,
	}, true
}
