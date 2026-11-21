package app

import (
	"io/fs"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

// permissionFileSystem is a fake directory reader that also advertises the
// lstat capability, so the model reserves and renders the permission column.
type permissionFileSystem struct {
	*fakeFileSystem
}

func (permissionFileSystem) ReadsPermissions() bool {
	return true
}

func newPermissionFileSystem() permissionFileSystem {
	return permissionFileSystem{fakeFileSystem: newFakeFileSystem()}
}

func TestPermissionModeTextMatchesLsStyleAndOctal(t *testing.T) {
	tests := []struct {
		name     string
		mode     fs.FileMode
		symbolic string
		numeric  string
	}{
		{name: "regular readable", mode: 0o644, symbolic: "-rw-r--r--", numeric: " 644"},
		{name: "private file", mode: 0o600, symbolic: "-rw-------", numeric: " 600"},
		{name: "directory", mode: fs.ModeDir | 0o755, symbolic: "drwxr-xr-x", numeric: " 755"},
		{name: "symlink", mode: fs.ModeSymlink | 0o777, symbolic: "lrwxrwxrwx", numeric: " 777"},
		{name: "named pipe", mode: fs.ModeNamedPipe | 0o644, symbolic: "prw-r--r--", numeric: " 644"},
		{name: "socket", mode: fs.ModeSocket | 0o755, symbolic: "srwxr-xr-x", numeric: " 755"},
		{name: "block device", mode: fs.ModeDevice | 0o660, symbolic: "brw-rw----", numeric: " 660"},
		{name: "character device", mode: fs.ModeDevice | fs.ModeCharDevice | 0o600, symbolic: "crw-------", numeric: " 600"},
		{name: "unspecified", mode: 0, symbolic: "----------", numeric: " 000"},
		{name: "setuid executable", mode: fs.ModeSetuid | 0o755, symbolic: "-rwsr-xr-x", numeric: "4755"},
		{name: "setuid without execute", mode: fs.ModeSetuid | 0o644, symbolic: "-rwSr--r--", numeric: "4644"},
		{name: "setgid executable", mode: fs.ModeSetgid | 0o2755, symbolic: "-rwxr-sr-x", numeric: "2755"},
		{name: "sticky directory", mode: fs.ModeDir | fs.ModeSticky | 0o1777, symbolic: "drwxrwxrwt", numeric: "1777"},
		{name: "sticky without execute", mode: fs.ModeDir | fs.ModeSticky | 0o666, symbolic: "drw-rw-rwT", numeric: "1666"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			symbolic, numeric := permissionModeText(test.mode)
			if symbolic != test.symbolic {
				t.Fatalf("symbolic = %q, want %q", symbolic, test.symbolic)
			}
			if numeric != test.numeric {
				t.Fatalf("numeric = %q, want %q", numeric, test.numeric)
			}
		})
	}
}

func TestPermissionBlockWidthFollowsPreferenceAndPaneWidth(t *testing.T) {
	tests := []struct {
		name         string
		mode         treePermissionMode
		contentWidth int
		want         int
	}{
		{name: "off stays hidden in a wide pane", mode: treePermissionsOff, contentWidth: 200, want: 0},
		{name: "numeric at the minimum width", mode: treePermissionsNumeric, contentWidth: permissionNumericMinContentWidth, want: permissionBlockNumericWidth},
		{name: "numeric below the minimum width", mode: treePermissionsNumeric, contentWidth: permissionNumericMinContentWidth - 1, want: 0},
		{name: "numeric ignores the full threshold", mode: treePermissionsNumeric, contentWidth: 200, want: permissionBlockNumericWidth},
		{name: "full at the minimum width", mode: treePermissionsFull, contentWidth: permissionFullMinContentWidth, want: permissionBlockFullWidth},
		{name: "full degrades to numeric", mode: treePermissionsFull, contentWidth: permissionFullMinContentWidth - 1, want: permissionBlockNumericWidth},
		{name: "full hides below the numeric threshold", mode: treePermissionsFull, contentWidth: permissionNumericMinContentWidth - 1, want: 0},
		{name: "zero width", mode: treePermissionsFull, contentWidth: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := Model{treePermissions: test.mode}
			if got := model.permissionBlockWidth(test.contentWidth); got != test.want {
				t.Fatalf("permissionBlockWidth(%d) = %d, want %d", test.contentWidth, got, test.want)
			}
		})
	}
}

func TestTreePermissionModeForNameFallsBackToFull(t *testing.T) {
	tests := map[string]treePermissionMode{
		"off":     treePermissionsOff,
		"numeric": treePermissionsNumeric,
		"full":    treePermissionsFull,
		"":        treePermissionsFull,
		"bogus":   treePermissionsFull,
	}
	for name, want := range tests {
		if got := treePermissionModeForName(name); got != want {
			t.Fatalf("treePermissionModeForName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPermissionTextPadsRootAndUnknownModes(t *testing.T) {
	tree, err := browser.NewTree(t.TempDir(), newPermissionFileSystem())
	if err != nil {
		t.Fatalf("NewTree() error = %v", err)
	}
	root := tree.Root()
	if got := permissionText(root, permissionBlockFullWidth); got != strings.Repeat(" ", permissionBlockFullWidth) {
		t.Fatalf("root permission text = %q, want %d blank cells", got, permissionBlockFullWidth)
	}
	if got := permissionText(nil, permissionBlockNumericWidth); got != strings.Repeat(" ", permissionBlockNumericWidth) {
		t.Fatalf("nil node permission text = %q, want %d blank cells", got, permissionBlockNumericWidth)
	}
	if got := permissionText(root, 0); got != "" {
		t.Fatalf("zero-width permission text = %q, want empty", got)
	}
}

func TestTreeRowsRightAlignThePermissionColumn(t *testing.T) {
	root := t.TempDir()
	fake := newPermissionFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "secret.env", Mode: 0, Perm: 0o600, HasPerm: true},
		{Name: "notes.md", Mode: 0, Perm: 0o644, HasPerm: true},
	})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	secret := rowContent(findRow(t, lines, "secret.env"))
	notes := rowContent(findRow(t, lines, "notes.md"))
	if !strings.HasSuffix(secret, "-rw-------  600") {
		t.Fatalf("secret row = %q, want the octal mode right aligned", secret)
	}
	if !strings.HasSuffix(notes, "-rw-r--r--  644") {
		t.Fatalf("notes row = %q, want the octal mode right aligned", notes)
	}
	// Equal-width octal fields keep the digits of every row in one column.
	// The rows differ in byte length because the row icons do, so compare
	// cell positions rather than string offsets.
	secretCell := lipgloss.Width(secret[:strings.Index(secret, "600")])
	notesCell := lipgloss.Width(notes[:strings.Index(notes, "644")])
	if secretCell != notesCell {
		t.Fatalf("octal column starts at cells %d and %d, want the same cell", secretCell, notesCell)
	}
	// The sticky root row is a location header, so its permission cells stay
	// blank while keeping the column width.
	rootRow := findRow(t, lines, rootTreeIcon)
	if strings.Contains(rootRow, "-rw-") || strings.Contains(rootRow, "rwx") {
		t.Fatalf("root row = %q, want blank permission cells", rootRow)
	}
}

func TestNumericPreferenceRendersOnlyTheOctalColumn(t *testing.T) {
	root := t.TempDir()
	fake := newPermissionFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "secret.env", Mode: 0, Perm: 0o600, HasPerm: true}})
	model := NewModelConfigured(root, "", ModelConfig{Preferences: Preferences{TreePermissions: "numeric"}}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	row := rowContent(findRow(t, strings.Split(ansi.Strip(model.View().Content), "\n"), "secret.env"))
	if !strings.HasSuffix(row, "  600") {
		t.Fatalf("row = %q, want the octal column only", row)
	}
	if strings.Contains(row, "rw-") {
		t.Fatalf("row = %q, want no ls-style text under the numeric preference", row)
	}
}

func TestPermissionColumnTruncatesTheNameBeforeTheBlock(t *testing.T) {
	root := t.TempDir()
	const longName = "a-very-long-file-name-that-does-not-fit-in-the-name-column.txt"
	fake := newPermissionFileSystem()
	fake.set(root, []filesystem.Entry{{Name: longName, Mode: 0, Perm: 0o640, HasPerm: true}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 12})

	rows := strings.Split(ansi.Strip(model.View().Content), "\n")
	row := findRow(t, rows, "a-very-long")
	if strings.Contains(row, longName) {
		t.Fatalf("row = %q, want the long name truncated before the permission block", row)
	}
	if !strings.Contains(row, ellipsis) {
		t.Fatalf("row = %q, want an ellipsis on the truncated name", row)
	}
	if !strings.HasSuffix(rowContent(row), "-rw-r-----  640") {
		t.Fatalf("row = %q, want the permission block intact after truncation", row)
	}
	if got := lipgloss.Width(row); got != 60 {
		t.Fatalf("row cell width = %d, want 60", got)
	}
}

func TestPermissionColumnKeepsEveryLineAtThePaneWidth(t *testing.T) {
	manyEntries := make([]filesystem.Entry, 0, 40)
	for index := 0; index < 40; index++ {
		manyEntries = append(manyEntries, filesystem.Entry{
			Name:    "file-" + string(rune('a'+index%26)) + ".md",
			Perm:    fs.FileMode(0o600 + index),
			HasPerm: true,
		})
	}
	fixtures := []struct {
		name    string
		entries []filesystem.Entry
	}{
		// More rows than the viewport: every tree row is populated and the
		// scrollbar thumb moves.
		{name: "scrolling", entries: manyEntries},
		// Fewer rows than the viewport: blank padding rows below the entries
		// must keep the same permission and scrollbar cells.
		{name: "padding rows", entries: manyEntries[:2]},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			fake := newPermissionFileSystem()
			fake.set(root, fixture.entries)

			for _, mode := range []treePermissionMode{treePermissionsOff, treePermissionsNumeric, treePermissionsFull} {
				model := NewModelConfigured(root, "", ModelConfig{Preferences: Preferences{TreePermissions: permissionModePreferences[mode]}}, fake)
				completeInitialLoad(t, model)
				// Widths below 4 are covered by the existing narrow-window
				// test; they keep the pre-existing blank-row letter cells.
				for width := 4; width <= 70; width++ {
					model.Update(tea.WindowSizeMsg{Width: width, Height: 20})
					lines := strings.Split(ansi.Strip(model.View().Content), "\n")
					for _, line := range lines {
						if got := lipgloss.Width(line); got > width {
							t.Fatalf("mode %v width %d: line %q is %d cells wide", mode, width, line, got)
						}
					}
					// The scrollbar is the last cell of every scrollable row.
					// The sticky root row is not scrollable and keeps a blank
					// cell, so the walk starts below it.
					_, treeHeight, _ := layoutHeights(model.height)
					for index := model.treeStartY() + 1; index < model.treeStartY()+treeHeight && index < len(lines); index++ {
						line := lines[index]
						if !strings.HasSuffix(line, scrollbarTrackGlyph) && !strings.HasSuffix(line, scrollbarThumbGlyph) {
							t.Fatalf("mode %v width %d line %d = %q, want the scrollbar cell last", mode, width, index, line)
						}
					}
				}
			}
		})
	}
}

func TestPermissionColumnStylesSelectionAndIgnoredRows(t *testing.T) {
	selected := Model{selected: 2, treePermissions: treePermissionsFull}
	row := selected.renderTreeRow(2, "", "icon", " file", " -rw-------  600", 0, false, permissionFullMinContentWidth)
	if !strings.Contains(row, "48;5;238") {
		t.Fatalf("selected row = %q, want the permission span on the selection background", row)
	}
	if !strings.Contains(row, "38;5;245") {
		t.Fatalf("row = %q, want the muted permission foreground", row)
	}

	ignored := Model{selected: 1, treePermissions: treePermissionsFull}
	row = ignored.renderTreeRow(2, "", "icon", " file", " -rw-------  600", 0, true, permissionFullMinContentWidth)
	if got := strings.Count(row, "38;5;245"); got < 3 {
		t.Fatalf("ignored row = %q, want icon, name, and permissions greyed (%d spans)", row, got)
	}
}

// rowContent drops the trailing scrollbar cell and the padding cells so a
// test can compare the row's visible content.
func rowContent(row string) string {
	row = strings.TrimSuffix(row, scrollbarTrackGlyph)
	row = strings.TrimSuffix(row, scrollbarThumbGlyph)
	return strings.TrimRight(row, " ")
}

func TestPermissionColumnFollowsTheReaderCapability(t *testing.T) {
	tests := []struct {
		name string
		pref string
		fs   filesystem.FileSystem
		want treePermissionMode
	}{
		{
			name: "default preference with the permission reader",
			pref: "",
			fs:   newPermissionFileSystem(),
			want: treePermissionsFull,
		},
		{
			name: "reader without the capability hides the column",
			pref: "full",
			fs:   newFakeFileSystem(),
			want: treePermissionsOff,
		},
		{
			name: "explicit numeric",
			pref: "numeric",
			fs:   newPermissionFileSystem(),
			want: treePermissionsNumeric,
		},
		{
			name: "explicit off",
			pref: "off",
			fs:   newPermissionFileSystem(),
			want: treePermissionsOff,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := NewModelConfigured(t.TempDir(), "", ModelConfig{
				Preferences: Preferences{TreePermissions: test.pref},
			}, test.fs)
			if model.treePermissions != test.want {
				t.Fatalf("treePermissions = %v, want %v", model.treePermissions, test.want)
			}
		})
	}

	// The default reader fills permissions, so a model built without an
	// injected filesystem keeps the default column available.
	model := NewModel(t.TempDir(), "")
	reader, ok := model.fileSystem.(filesystem.PermissionReader)
	if !ok || !reader.ReadsPermissions() {
		t.Fatalf("default filesystem = %T, want a reader with permission support", model.fileSystem)
	}
	if model.treePermissions != treePermissionsFull {
		t.Fatalf("default treePermissions = %v, want full", model.treePermissions)
	}
}

// findRow returns the single rendered line that contains marker. Tests use it
// instead of fixed row indexes so the sticky root row and viewport layout do
// not leak into the assertion.
func findRow(t *testing.T, lines []string, marker string) string {
	t.Helper()
	found := ""
	for _, line := range lines {
		if strings.Contains(line, marker) {
			if found != "" {
				t.Fatalf("marker %q appears in more than one line", marker)
			}
			found = line
		}
	}
	if found == "" {
		t.Fatalf("marker %q not found in %q", marker, lines)
	}
	return found
}

// permissionModePreferences maps a mode onto the preference string, so a test
// can ask the model for the same cap it asserts.
var permissionModePreferences = map[treePermissionMode]string{
	treePermissionsOff:     "off",
	treePermissionsNumeric: "numeric",
	treePermissionsFull:    "full",
}
