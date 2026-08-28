package app

import "github.com/u7chan/herdr-file-viewer/internal/browser"

type treeIconSet uint8

const (
	iconSetFontAwesomeSolid treeIconSet = iota
	iconSetFontAwesomeOutline
	iconSetMaterial
	iconSetCodicon

	defaultTreeIconSet = iconSetFontAwesomeSolid
)

type treeIcons struct {
	directory     string
	directoryOpen string
	file          string
}

const (
	collapsedTreeIcon = ""
	expandedTreeIcon  = ""
	rootTreeIcon      = ""
	symlinkTreeIcon   = ""
)

func iconsFor(set treeIconSet) treeIcons {
	switch set {
	case iconSetFontAwesomeSolid:
		return treeIcons{directory: "", directoryOpen: "", file: ""}
	case iconSetFontAwesomeOutline:
		return treeIcons{directory: "", directoryOpen: "", file: ""}
	case iconSetMaterial:
		return treeIcons{directory: "󰉋", directoryOpen: "󰝰", file: "󰈙"}
	case iconSetCodicon:
		return treeIcons{directory: "", directoryOpen: "", file: ""}
	default:
		return iconsFor(iconSetFontAwesomeSolid)
	}
}

func iconForNode(node *browser.Node, icons treeIcons) string {
	if node == nil {
		return icons.file
	}
	if node.IsDirectory() {
		if node.Expanded() {
			return icons.directoryOpen
		}
		return icons.directory
	}
	if node.IsSymlink() {
		return symlinkTreeIcon
	}
	return icons.file
}
