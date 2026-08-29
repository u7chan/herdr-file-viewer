package app

import (
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
)

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

// Special names take precedence over extension matches.
var exactNameIcons = map[string]string{
	"makefile":   "\ue673",
	"dockerfile": "\ue7b0",
	"go.mod":     "\ue627",
	"go.sum":     "\ue627",
	"license":    "\uf0a3",
	"readme":     "\uf405",
	"readme.md":  "\uf405",
	"readme.txt": "\uf405",
}

var extensionIcons = map[string]string{
	// Languages
	".go": "\ue627", ".rs": "\ue68b", ".py": "\ue606", ".pyi": "\ue606",
	".js": "\ue60c", ".mjs": "\ue60c", ".cjs": "\ue60c", ".ts": "\ue628", ".tsx": "\ue628", ".mts": "\ue628", ".cts": "\ue628",
	".c": "\ue61e", ".h": "\ue61e", ".cc": "\ue61d", ".cpp": "\ue61d", ".cxx": "\ue61d", ".hpp": "\ue61d", ".hh": "\ue61d", ".hxx": "\ue61d",
	".cs": "\U000f031b", ".java": "\ue738", ".rb": "\ue605", ".php": "\ue73d", ".swift": "\ue755",
	".kt": "\U000f1219", ".lua": "\ue620", ".vim": "\ue62b", ".hs": "\U000f0c92", ".r": "\U000f07d4", ".pl": "\ue769", ".erl": "\ue7b1",
	// Web
	".html": "\ue60e", ".htm": "\ue60e", ".css": "\ue614", ".scss": "\ue603", ".sass": "\ue603",
	".vue": "\ue6a0", ".graphql": "\ue662", ".gql": "\ue662",
	// Data and configuration
	".json": "\ue60b", ".jsonc": "\ue60b", ".yaml": "\ue6a8", ".yml": "\ue6a8", ".toml": "\ue615",
	".ini": "\ue615", ".conf": "\ue615", ".cfg": "\ue615", ".properties": "\ue615", ".editorconfig": "\ue615",
	".xml": "\U000f05c0", ".plist": "\U000f05c0", ".csv": "\ue64a", ".tsv": "\ue64a", ".sql": "\ue706",
	// Documentation
	".md": "\ue609", ".markdown": "\ue609", ".pdf": "\uf1c1",
	// Shells
	".sh": "\ue691", ".bash": "\ue691", ".zsh": "\ue691", ".fish": "\ue691", ".ps1": "\ue683",
	// Archives
	".zip": "\uf1c6", ".gz": "\uf1c6", ".tgz": "\uf1c6", ".tar": "\uf1c6", ".xz": "\uf1c6",
	".bz2": "\uf1c6", ".7z": "\uf1c6", ".rar": "\uf1c6", ".zst": "\uf1c6", ".jar": "\uf1c6",
	// Images and fonts
	".png": "\ue60d", ".jpg": "\ue60d", ".jpeg": "\ue60d", ".gif": "\ue60d", ".bmp": "\ue60d",
	".svg": "\ue60d", ".webp": "\ue60d", ".ico": "\ue60d",
	".ttf": "\ue659", ".otf": "\ue659", ".woff": "\ue659", ".woff2": "\ue659", ".eot": "\ue659",
	// State and special dotfiles. filepath.Ext(".env") is ".env".
	".lock": "\ue672", ".env": "\U000f0613", ".gitignore": "\ue702", ".gitattributes": "\ue702", ".gitmodules": "\ue702",
	".dockerignore": "\ue7b0", ".mk": "\ue673",
}

// iconColorPalette assigns VSCode-icon-theme-like foreground colors to tree
// glyphs. Keys are glyphs from the icon tables, so families sharing a glyph
// share one color; glyphs without an entry render in the default foreground.
var iconColorPalette = newIconColorPalette()

func newIconColorPalette() map[string]color.Color {
	palette := map[string]color.Color{
		// Languages
		"\ue627":     lipgloss.Color("38"),
		"\ue68b":     lipgloss.Color("180"),
		"\ue606":     lipgloss.Color("68"),
		"\ue60c":     lipgloss.Color("185"),
		"\ue628":     lipgloss.Color("74"),
		"\ue61e":     lipgloss.Color("145"),
		"\ue61d":     lipgloss.Color("204"),
		"\U000f031b": lipgloss.Color("140"),
		"\ue738":     lipgloss.Color("172"),
		"\ue605":     lipgloss.Color("167"),
		"\ue73d":     lipgloss.Color("140"),
		"\ue755":     lipgloss.Color("202"),
		"\U000f1219": lipgloss.Color("141"),
		"\ue620":     lipgloss.Color("74"),
		"\ue62b":     lipgloss.Color("34"),
		"\U000f0c92": lipgloss.Color("60"),
		"\U000f07d4": lipgloss.Color("33"),
		"\ue769":     lipgloss.Color("38"),
		"\ue7b1":     lipgloss.Color("131"),
		// Web
		"\ue60e": lipgloss.Color("166"),
		"\ue614": lipgloss.Color("74"),
		"\ue603": lipgloss.Color("204"),
		"\ue6a0": lipgloss.Color("72"),
		"\ue662": lipgloss.Color("200"),
		// Data and configuration
		"\ue60b":     lipgloss.Color("185"),
		"\ue6a8":     lipgloss.Color("103"),
		"\ue615":     lipgloss.Color("245"),
		"\U000f05c0": lipgloss.Color("173"),
		"\ue64a":     lipgloss.Color("114"),
		"\ue706":     lipgloss.Color("209"),
		// Documentation
		"\ue609": lipgloss.Color("74"),
		"\uf1c1": lipgloss.Color("203"),
		// Shells
		"\ue691": lipgloss.Color("114"),
		"\ue683": lipgloss.Color("80"),
		// Archives
		"\uf1c6": lipgloss.Color("178"),
		// Images and fonts
		"\ue60d": lipgloss.Color("140"),
		"\ue659": lipgloss.Color("137"),
		// State and special dotfiles
		"\ue672":     lipgloss.Color("184"),
		"\U000f0613": lipgloss.Color("37"),
		"\ue702":     lipgloss.Color("202"),
		"\ue673":     lipgloss.Color("135"),
		"\ue7b0":     lipgloss.Color("39"),
		"\uf0a3":     lipgloss.Color("143"),
		"\uf405":     lipgloss.Color("74"),
	}

	// Directory glyphs come from the icon sets instead of private-use escapes
	// repeated here.
	directoryColor := lipgloss.Color("179")
	for _, set := range []treeIconSet{
		iconSetFontAwesomeSolid,
		iconSetFontAwesomeOutline,
		iconSetMaterial,
		iconSetCodicon,
	} {
		icons := iconsFor(set)
		palette[icons.directory] = directoryColor
		palette[icons.directoryOpen] = directoryColor
	}
	palette[symlinkTreeIcon] = lipgloss.Color("81")
	return palette
}

// iconStyle colors a tree glyph with its palette foreground, or returns a
// plain style for glyphs without an entry.
func iconStyle(glyph string) lipgloss.Style {
	if glyphColor, ok := iconColorPalette[glyph]; ok {
		return lipgloss.NewStyle().Foreground(glyphColor)
	}
	return lipgloss.NewStyle()
}

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

func fileIconFor(name, fallback string) string {
	if icon, ok := exactNameIcons[strings.ToLower(name)]; ok {
		return icon
	}

	if icon, ok := extensionIcons[strings.ToLower(filepath.Ext(name))]; ok {
		return icon
	}
	return fallback
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
	return fileIconFor(node.Name(), icons.file)
}
