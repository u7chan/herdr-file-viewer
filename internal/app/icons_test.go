package app

import (
	"io/fs"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestIconsForDefinesTheFourSelectableSets(t *testing.T) {
	tests := []struct {
		name string
		set  treeIconSet
		want treeIcons
	}{
		{
			name: "Font Awesome solid",
			set:  iconSetFontAwesomeSolid,
			want: treeIcons{directory: "", directoryOpen: "", file: ""},
		},
		{
			name: "Font Awesome outline",
			set:  iconSetFontAwesomeOutline,
			want: treeIcons{directory: "", directoryOpen: "", file: ""},
		},
		{
			name: "Material",
			set:  iconSetMaterial,
			want: treeIcons{directory: "󰉋", directoryOpen: "󰝰", file: "󰈙"},
		},
		{
			name: "Codicon",
			set:  iconSetCodicon,
			want: treeIcons{directory: "", directoryOpen: "", file: ""},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := iconsFor(test.set); got != test.want {
				t.Fatalf("iconsFor(%v) = %#v, want %#v", test.set, got, test.want)
			}
		})
	}
}

func TestDefaultTreeIconSetIsFontAwesomeSolid(t *testing.T) {
	if got, want := iconsFor(defaultTreeIconSet), iconsFor(iconSetFontAwesomeSolid); got != want {
		t.Fatalf("defaultTreeIconSet icons = %#v, want %#v", got, want)
	}
}

func TestIconsForUnknownSetFallsBackToFontAwesomeSolid(t *testing.T) {
	if got, want := iconsFor(treeIconSet(255)), iconsFor(iconSetFontAwesomeSolid); got != want {
		t.Fatalf("unknown icon set = %#v, want %#v", got, want)
	}
}

func TestFileIconForPrefersExactNamesAndMatchesExtensionsCaseInsensitively(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "Dockerfile", want: "\ue7b0"},
		{name: "README.MD", want: "\uf405"},
		{name: "notes.MD", want: "\ue609"},
		{name: "main.GO", want: "\ue627"},
		{name: "archive.tar.GZ", want: "\uf1c6"},
		{name: ".ENV", want: "\U000f0613"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := fileIconFor(test.name, "fallback"); got != test.want {
				t.Fatalf("fileIconFor(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}
}

func TestFileIconForUnknownNamesUsesTheProvidedFallback(t *testing.T) {
	for _, name := range []string{"unknown.xyz", "without-extension"} {
		t.Run(name, func(t *testing.T) {
			if got, want := fileIconFor(name, "fallback"), "fallback"; got != want {
				t.Fatalf("fileIconFor(%q) = %q, want %q", name, got, want)
			}
		})
	}
}

func TestAllFileIconsAreSingleCellWide(t *testing.T) {
	for name, icon := range exactNameIcons {
		if got := lipgloss.Width(icon); got != 1 {
			t.Errorf("exactNameIcons[%q] = %q has width %d, want 1", name, icon, got)
		}
	}
	for extension, icon := range extensionIcons {
		if got := lipgloss.Width(icon); got != 1 {
			t.Errorf("extensionIcons[%q] = %q has width %d, want 1", extension, icon, got)
		}
	}
}

func TestIconForNodeUsesFileIconsAndPreservesNodeKinds(t *testing.T) {
	icons := iconsFor(iconSetFontAwesomeOutline)

	file := iconTestNode(t, "main.go", 0)
	if got, want := iconForNode(file, icons), "\ue627"; got != want {
		t.Fatalf("file icon = %q, want %q", got, want)
	}

	directoryTree := iconTestTree(t, "directory", fs.ModeDir)
	directory := directoryTree.Root().Children()[0]
	if got, want := iconForNode(directory, icons), icons.directory; got != want {
		t.Fatalf("collapsed directory icon = %q, want %q", got, want)
	}
	if _, ok := directoryTree.Expand(directory); !ok {
		t.Fatal("tree.Expand(directory) started no load")
	}
	if got, want := iconForNode(directory, icons), icons.directoryOpen; got != want {
		t.Fatalf("expanded directory icon = %q, want %q", got, want)
	}

	symlink := iconTestNode(t, "linked.go", fs.ModeSymlink)
	if got, want := iconForNode(symlink, icons), symlinkTreeIcon; got != want {
		t.Fatalf("symlink icon = %q, want %q", got, want)
	}
}

func iconTestNode(t *testing.T, name string, mode fs.FileMode) *browser.Node {
	t.Helper()

	tree := iconTestTree(t, name, mode)
	return tree.Root().Children()[0]
}

func iconTestTree(t *testing.T, name string, mode fs.FileMode) *browser.Tree {
	t.Helper()

	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: name, Mode: mode}})
	tree, err := browser.NewTree(root, fake)
	if err != nil {
		t.Fatalf("browser.NewTree() error = %v", err)
	}
	request, ok := tree.Expand(tree.Root())
	if !ok {
		t.Fatal("tree.Expand(root) started no load")
	}
	if !tree.ApplyLoad(tree.Read(request)) {
		t.Fatal("tree.ApplyLoad(root) rejected result")
	}
	children := tree.Root().Children()
	if len(children) != 1 {
		t.Fatalf("root children = %d, want 1", len(children))
	}
	return tree
}
