package app

import (
	"path/filepath"
	"strings"

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
