package app

import "testing"

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
