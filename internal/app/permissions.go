package app

import (
	"io/fs"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
)

// treePermissionMode is the resolved tree.permissions preference: the widest
// permission column the tree may render. The pane width can still drop it.
type treePermissionMode uint8

const (
	treePermissionsOff treePermissionMode = iota
	treePermissionsNumeric
	treePermissionsFull
)

const (
	permissionSymbolicWidth = 10
	permissionNumericWidth  = 4
	permissionGapWidth      = 1

	// The octal block is one gap plus the fixed-width number; the full block
	// adds the ls-style text and its own gap in front of the number.
	permissionBlockNumericWidth = permissionGapWidth + permissionNumericWidth
	permissionBlockFullWidth    = permissionBlockNumericWidth + permissionGapWidth + permissionSymbolicWidth

	// A permission column only appears while the name keeps at least this
	// many cells. Below it the column is dropped instead of squeezing names.
	permissionNumericMinContentWidth = 21
	permissionFullMinContentWidth    = 36

	// Muted neutral grays: clearly weaker than the tree content, brighter
	// than the selected row background on both palettes.
	permissionForegroundDark  = "245"
	permissionForegroundLight = "244"
)

var (
	permissionStyleDark  = lipgloss.NewStyle().Inline(true).Foreground(lipgloss.Color(permissionForegroundDark))
	permissionStyleLight = lipgloss.NewStyle().Inline(true).Foreground(lipgloss.Color(permissionForegroundLight))
)

func permissionStyle(lightBackground bool) lipgloss.Style {
	if lightBackground {
		return permissionStyleLight
	}
	return permissionStyleDark
}

// treePermissionModeForName maps the resolved preference onto the widest
// column. Empty and unknown values fall back to the built-in full column, so
// a model constructed without preferences behaves like the default document.
func treePermissionModeForName(name string) treePermissionMode {
	switch name {
	case "off":
		return treePermissionsOff
	case "numeric":
		return treePermissionsNumeric
	default:
		return treePermissionsFull
	}
}

// permissionBlockWidth returns the cells reserved at the right of one tree
// row for permissions, or 0 when the column is off or the pane is too narrow
// to keep a readable name next to it.
func (m *Model) permissionBlockWidth(contentWidth int) int {
	if contentWidth <= 0 {
		return 0
	}
	switch m.treePermissions {
	case treePermissionsOff:
		return 0
	case treePermissionsNumeric:
		if contentWidth >= permissionNumericMinContentWidth {
			return permissionBlockNumericWidth
		}
		return 0
	default:
		if contentWidth >= permissionFullMinContentWidth {
			return permissionBlockFullWidth
		}
		if contentWidth >= permissionNumericMinContentWidth {
			return permissionBlockNumericWidth
		}
		return 0
	}
}

// permissionText renders the padded permission block for one row. The sticky
// root row is a location header without an entry of its own, and a failed
// lstat leaves the mode unknown, so both stay blank instead of inventing a
// value. A non-zero width always returns exactly that many cells.
func permissionText(node *browser.Node, blockWidth int) string {
	if blockWidth <= 0 {
		return ""
	}
	mode, ok := node.Permissions()
	if !ok || node.Parent() == nil {
		return strings.Repeat(" ", blockWidth)
	}

	symbolic, numeric := permissionModeText(mode)
	if blockWidth == permissionBlockFullWidth {
		return strings.Repeat(" ", permissionGapWidth) + symbolic + " " + numeric
	}
	return strings.Repeat(" ", permissionGapWidth) + numeric
}

// permissionModeText renders one entry mode in both forms: the ten-cell
// ls -l column and the four-cell chmod octal number. The special-bit digit
// of the number appears only when a special bit is set, so an ordinary file
// reads as 644 rather than 0644. fs.FileMode.String is deliberately not used
// because it renders special bits as leading type letters and a symlink as
// "L", neither of which matches ls -l.
func permissionModeText(mode fs.FileMode) (symbolic, numeric string) {
	perm := mode.Perm()
	symbolic = string(permissionTypeChar(mode)) +
		permissionTriple(perm>>6, mode&fs.ModeSetuid, 's', 'S') +
		permissionTriple(perm>>3, mode&fs.ModeSetgid, 's', 'S') +
		permissionTriple(perm, mode&fs.ModeSticky, 't', 'T')
	return symbolic, permissionNumber(mode)
}

// permissionTriple renders one rwx group. A set special bit replaces the
// execute column with s/S or t/T, matching ls -l.
func permissionTriple(perm, special fs.FileMode, set, unset byte) string {
	triple := []byte("rwx")
	if perm&0o4 == 0 {
		triple[0] = '-'
	}
	if perm&0o2 == 0 {
		triple[1] = '-'
	}
	if perm&0o1 == 0 {
		triple[2] = '-'
	}
	if special != 0 {
		if perm&0o1 != 0 {
			triple[2] = set
		} else {
			triple[2] = unset
		}
	}
	return string(triple)
}

// permissionTypeChar maps the type bits onto the ls -l leading column. Device
// entries are checked before block devices because ModeCharDevice implies
// ModeDevice.
func permissionTypeChar(mode fs.FileMode) byte {
	switch {
	case mode&fs.ModeCharDevice != 0:
		return 'c'
	case mode&fs.ModeDevice != 0:
		return 'b'
	case mode.IsDir():
		return 'd'
	case mode&fs.ModeSymlink != 0:
		return 'l'
	case mode&fs.ModeNamedPipe != 0:
		return 'p'
	case mode&fs.ModeSocket != 0:
		return 's'
	case mode&fs.ModeIrregular != 0:
		return '?'
	default:
		return '-'
	}
}

// permissionNumber renders the chmod octal number right aligned in the fixed
// numeric field. It builds the four cells directly instead of formatting,
// because every visible row runs this on the render path.
func permissionNumber(mode fs.FileMode) string {
	special := 0
	if mode&fs.ModeSetuid != 0 {
		special += 4
	}
	if mode&fs.ModeSetgid != 0 {
		special += 2
	}
	if mode&fs.ModeSticky != 0 {
		special++
	}

	perm := mode.Perm()
	field := [permissionNumericWidth]byte{' ', ' ', ' ', ' '}
	for group := 0; group < 3; group++ {
		field[permissionNumericWidth-1-group] = '0' + byte(perm>>(3*group))&0o7
	}
	if special != 0 {
		field[0] = byte('0' + special)
	}
	return string(field[:])
}
