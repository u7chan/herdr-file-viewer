package app

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/clipperhouse/uax29/v2/graphemes"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
)

type findMatchRange struct {
	start int
	end   int
}

func findNameMatchRanges(name, query string) []findMatchRange {
	if name == "" || query == "" {
		return nil
	}

	queryRuneCount := utf8.RuneCountInString(query)
	if queryRuneCount == 0 {
		return nil
	}

	boundaries := make([]int, 0, utf8.RuneCountInString(name)+1)
	for offset := 0; offset < len(name); {
		boundaries = append(boundaries, offset)
		_, size := utf8.DecodeRuneInString(name[offset:])
		offset += size
	}
	boundaries = append(boundaries, len(name))
	if queryRuneCount > len(boundaries)-1 {
		return nil
	}

	var matches []findMatchRange
	for start := 0; start+queryRuneCount < len(boundaries); start++ {
		end := start + queryRuneCount
		if strings.EqualFold(name[boundaries[start]:boundaries[end]], query) {
			matches = append(matches, findMatchRange{start: boundaries[start], end: boundaries[end]})
		}
	}
	return matches
}

func firstMatchingVisibleRow(rows []browser.VisibleRow, query string) int {
	for index := stickyRootHeight; index < len(rows); index++ {
		if visibleRowMatchesFindQuery(rows[index], query) {
			return index
		}
	}
	return -1
}

func previousMatchingVisibleRow(rows []browser.VisibleRow, query string, selected int) int {
	return adjacentMatchingVisibleRow(rows, query, selected, -1)
}

func nextMatchingVisibleRow(rows []browser.VisibleRow, query string, selected int) int {
	return adjacentMatchingVisibleRow(rows, query, selected, 1)
}

func adjacentMatchingVisibleRow(rows []browser.VisibleRow, query string, selected, direction int) int {
	if query == "" || len(rows) <= stickyRootHeight {
		return -1
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= len(rows) {
		selected = len(rows) - 1
	}
	for step := 1; step <= len(rows); step++ {
		index := (selected + direction*step) % len(rows)
		if index < 0 {
			index += len(rows)
		}
		if index >= stickyRootHeight && visibleRowMatchesFindQuery(rows[index], query) {
			return index
		}
	}
	return -1
}

func visibleRowMatchesFindQuery(row browser.VisibleRow, query string) bool {
	return row.Node != nil && len(findNameMatchRanges(row.Node.Name(), query)) > 0
}

func (m *Model) startFind() {
	m.findActive = true
	m.findQuery = ""
	m.findAnchorPath = ""
	if node := m.selectedNode(); node != nil {
		m.findAnchorPath = node.Path()
	}
}

func (m *Model) handleFindKey(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.String() == "ctrl+c":
		return tea.Quit
	case isFindEscapeKey(key):
		m.cancelFind()
	case key.String() == "enter":
		m.confirmFind()
	case key.String() == "up":
		m.moveFindMatch(m.findQuery, -1)
	case key.String() == "down":
		m.moveFindMatch(m.findQuery, 1)
	case key.String() == "backspace":
		m.deleteFindGrapheme()
	case isFindTextKey(key):
		m.setFindQuery(m.findQuery + key.Text)
	}
	return nil
}

func isFindStartKey(key tea.KeyPressMsg) bool {
	return key.String() == "/"
}

func isFindTextKey(key tea.KeyPressMsg) bool {
	return key.Text != "" && (key.Mod == 0 || key.Mod == tea.ModShift)
}

func isFindEscapeKey(key tea.KeyPressMsg) bool {
	return key.String() == "esc"
}

func findRepeatDirection(key tea.KeyPressMsg) int {
	switch key.String() {
	case "n":
		return 1
	case "N":
		return -1
	default:
		return 0
	}
}

func (m *Model) setFindQuery(query string) {
	m.findQuery = query
	m.findHighlightQuery = query
	m.moveToFirstFindMatch()
}

func (m *Model) deleteFindGrapheme() {
	if m.findQuery == "" {
		return
	}

	lastStart := len(m.findQuery)
	iterator := graphemes.FromString(m.findQuery)
	for iterator.Next() {
		lastStart = iterator.Start()
	}
	m.setFindQuery(m.findQuery[:lastStart])
}

func (m *Model) moveToFirstFindMatch() {
	if index := firstMatchingVisibleRow(m.visibleRows, m.findQuery); index >= 0 {
		m.selected = index
		m.keepSelectionVisible()
	}
}

func (m *Model) moveFindMatch(query string, direction int) {
	var index int
	if direction < 0 {
		index = previousMatchingVisibleRow(m.visibleRows, query, m.selected)
	} else {
		index = nextMatchingVisibleRow(m.visibleRows, query, m.selected)
	}
	if index >= 0 {
		m.selected = index
		m.keepSelectionVisible()
	}
}

func (m *Model) confirmFind() {
	if m.findQuery != "" {
		m.lastQuery = m.findQuery
		m.findHighlightQuery = m.findQuery
	}
	m.findActive = false
	m.findQuery = ""
	m.findAnchorPath = ""
}

func (m *Model) cancelFind() {
	anchorPath := m.findAnchorPath
	m.findActive = false
	m.findQuery = ""
	m.findAnchorPath = ""
	m.findHighlightQuery = ""
	m.restoreFindAnchor(anchorPath)
}

func (m *Model) restoreFindAnchor(path string) {
	if path == "" {
		return
	}
	for index, row := range m.visibleRows {
		if row.Node != nil && row.Node.Path() == path {
			m.selected = index
			m.keepSelectionVisible()
			return
		}
	}
	if len(m.visibleRows) > 0 {
		m.selected = len(m.visibleRows) - 1
		m.keepSelectionVisible()
	}
}

func (m *Model) clearFindHighlights() {
	m.lastQuery = ""
	m.findHighlightQuery = ""
}

func (m *Model) findPrompt() string {
	prompt := "find: " + m.findQuery
	if m.findQuery != "" && firstMatchingVisibleRow(m.visibleRows, m.findQuery) < 0 {
		prompt += " (no match)"
	}
	return prompt
}

func (m *Model) renderFindName(index int, name string, style lipgloss.Style) string {
	if index == 0 || m.findHighlightQuery == "" {
		return style.Render(name)
	}

	nameStart := 0
	if strings.HasPrefix(name, " ") {
		nameStart = len(" ")
	}
	matches := mergeFindMatchRanges(findNameMatchRanges(name[nameStart:], m.findHighlightQuery))
	if len(matches) == 0 {
		return style.Render(name)
	}

	var rendered strings.Builder
	rendered.Grow(len(name))
	current := 0
	for _, match := range matches {
		start := nameStart + match.start
		end := nameStart + match.end
		if current < start {
			rendered.WriteString(style.Render(name[current:start]))
		}
		rendered.WriteString(style.Underline(true).Render(name[start:end]))
		current = end
	}
	if current < len(name) {
		rendered.WriteString(style.Render(name[current:]))
	}
	return rendered.String()
}

func mergeFindMatchRanges(matches []findMatchRange) []findMatchRange {
	if len(matches) < 2 {
		return matches
	}

	merged := make([]findMatchRange, 0, len(matches))
	for _, match := range matches {
		last := len(merged) - 1
		if last < 0 || match.start > merged[last].end {
			merged = append(merged, match)
			continue
		}
		if match.end > merged[last].end {
			merged[last].end = match.end
		}
	}
	return merged
}
