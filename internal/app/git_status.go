package app

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
)

// gitStatusColorPalette is shared by every future status presentation. The
// bright ANSI-256 colors remain readable against the selected row background.
var gitStatusColorPalette = map[browser.GitStatus]color.Color{
	browser.GitStatusModified:  lipgloss.Color("220"),
	browser.GitStatusUntracked: lipgloss.Color("42"),
	browser.GitStatusAdded:     lipgloss.Color("42"),
	browser.GitStatusUnmerged:  lipgloss.Color("203"),
	browser.GitStatusDeleted:   lipgloss.Color("203"),
}

// ignoredRowColor greys out .gitignore-matched rows. It stays brighter than
// the selected row background (238) so ignored rows remain readable there.
var ignoredRowColor = lipgloss.Color("245")

var ignoredRowStyle = lipgloss.NewStyle().Inline(true).Foreground(ignoredRowColor)

func gitStatusStyle(status browser.GitStatus) lipgloss.Style {
	color, ok := gitStatusColorPalette[status]
	if !ok {
		return lipgloss.NewStyle().Inline(true)
	}
	return lipgloss.NewStyle().Inline(true).Foreground(color)
}

// gitStatusLetter maps a status to its right-hand letter-column glyph. The
// decision record in issue #44 assigns ? to new (untracked) files and U to
// conflicts (unmerged).
func gitStatusLetter(status browser.GitStatus) string {
	switch status {
	case browser.GitStatusModified:
		return "M"
	case browser.GitStatusAdded:
		return "A"
	case browser.GitStatusUntracked:
		return "?"
	case browser.GitStatusUnmerged:
		return "U"
	case browser.GitStatusDeleted:
		return "D"
	default:
		return ""
	}
}
