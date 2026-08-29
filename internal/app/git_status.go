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

func gitStatusStyle(status browser.GitStatus) lipgloss.Style {
	color, ok := gitStatusColorPalette[status]
	if !ok {
		return lipgloss.NewStyle().Inline(true)
	}
	return lipgloss.NewStyle().Inline(true).Foreground(color)
}
