package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func FuzzSanitizeDisplay(f *testing.F) {
	for _, seed := range terminalSafetySeeds() {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		assertTerminalSafe(t, sanitizeDisplay(string(input)))
	})
}

func FuzzTruncateToWidth(f *testing.F) {
	for _, seed := range terminalSafetySeeds() {
		for _, width := range []int{-1, 0, 1, 2, 8, 80} {
			f.Add(seed, width)
		}
	}

	f.Fuzz(func(t *testing.T, input string, width int) {
		got := truncateToWidth(sanitizeDisplay(input), width)
		assertTerminalSafe(t, got)

		if width <= 0 {
			if got != "" {
				t.Fatalf("truncateToWidth(%q, %d) = %q, want empty output", input, width, got)
			}
			return
		}
		if actual := lipgloss.Width(got); actual > width {
			t.Fatalf("truncateToWidth(%q, %d) cell width = %d, want <= %d: %q", input, width, actual, width, got)
		}
	})
}

func terminalSafetySeeds() []string {
	return []string{
		"plain ASCII filename.txt",
		"C0\x00\x01\x1f DEL\x7f",
		"C1\u0080\u0085\u009f",
		"escape\x1b[31mred\x1b[0m",
		"line\ncarriage\rreturn\ttab",
		string([]byte{0xff, 0xfe, 0xc3, 0x28, 0xa0, 0xaf}),
		"日本語/ファイル名.txt",
		"emoji🙂🚀",
		"e\u0301 cafe\u0301",
		"",
		"/workspace/" + strings.Repeat("directory/", 64) + strings.Repeat("long-name-", 16) + "file.txt",
	}
}

func assertTerminalSafe(t *testing.T, value string) {
	t.Helper()
	if !utf8.ValidString(value) {
		t.Fatalf("output is invalid UTF-8: %q", value)
	}
	for _, r := range value {
		if r <= 0x1f || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("output contains terminal control U+%04X: %q", r, value)
		}
	}
}
