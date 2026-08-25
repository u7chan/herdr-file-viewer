package app

import (
	"strings"
)

// sanitizeDisplay makes untrusted filesystem-derived text safe for a
// terminal cell. Paths, names, warnings, and errors all pass through this
// boundary before they are rendered.
func sanitizeDisplay(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")

	var sanitized strings.Builder
	sanitized.Grow(len(value))
	for _, r := range value {
		if isTerminalControl(r) {
			sanitized.WriteRune('\uFFFD')
			continue
		}
		sanitized.WriteRune(r)
	}
	return sanitized.String()
}

func isTerminalControl(r rune) bool {
	return r <= 0x1f || r == 0x7f || r >= 0x80 && r <= 0x9f
}
