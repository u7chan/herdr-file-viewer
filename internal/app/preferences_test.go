package app

import (
	"errors"
	"image/color"
	"io/fs"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestResolveAppearanceMapsModes(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		wantLight bool
		wantFixed bool
	}{
		{name: "light", mode: "light", wantLight: true, wantFixed: true},
		{name: "dark", mode: "dark", wantLight: false, wantFixed: true},
		{name: "auto", mode: "auto", wantLight: false, wantFixed: false},
		{name: "empty falls back to auto", mode: "", wantLight: false, wantFixed: false},
		{name: "unknown falls back to auto", mode: "sepia", wantLight: false, wantFixed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotLight, gotFixed := resolveAppearance(test.mode)
			if gotLight != test.wantLight || gotFixed != test.wantFixed {
				t.Fatalf("resolveAppearance(%q) = light %v, fixed %v; want light %v, fixed %v",
					test.mode, gotLight, gotFixed, test.wantLight, test.wantFixed)
			}
		})
	}
}

func TestFixedLightAppearanceSkipsDetectionAndIgnoresTerminalResponses(t *testing.T) {
	fake := newFakeFileSystem()
	root := t.TempDir()
	fake.set(root, nil)
	tree := NewModelConfigured(root, "", ModelConfig{Preferences: Preferences{AppearanceMode: "light"}}, fake)
	command := tree.Init()
	if command == nil {
		t.Fatal("Init() returned nil")
	}
	if _, ok := command().(directoryLoadMsg); !ok {
		t.Fatalf("Init() message = %T, want only the directory load (no background request)", command())
	}
	if !tree.lightBackground {
		t.Fatal("lightBackground = false, want true from the fixed light preference")
	}
	// A dark OSC 11 response must not override the fixed preference.
	tree.Update(tea.BackgroundColorMsg{Color: color.RGBA{A: 255}})
	if !tree.lightBackground {
		t.Fatal("lightBackground = false after a dark response, want the fixed light palette")
	}

	preview := NewPreviewModelConfigured("/abs/file.txt", nil, "", PreviewModelConfig{
		Preferences: Preferences{AppearanceMode: "light"},
	}, &fakePreviewReader{content: []byte("x")})
	command = preview.Init()
	if command == nil {
		t.Fatal("preview Init() returned nil")
	}
	if _, ok := command().(previewLoadMsg); !ok {
		t.Fatalf("preview Init() message = %T, want only the file load (no background request)", command())
	}
	if !preview.lightBackground {
		t.Fatal("preview lightBackground = false, want true from the fixed light preference")
	}
	preview.Update(tea.BackgroundColorMsg{Color: color.RGBA{A: 255}})
	if !preview.lightBackground {
		t.Fatal("preview lightBackground = false after a dark response, want the fixed light palette")
	}
}

func TestFixedDarkAppearanceIgnoresLightTerminalResponses(t *testing.T) {
	fake := newFakeFileSystem()
	root := t.TempDir()
	fake.set(root, nil)
	tree := NewModelConfigured(root, "", ModelConfig{Preferences: Preferences{AppearanceMode: "dark"}}, fake)
	tree.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	if tree.lightBackground {
		t.Fatal("lightBackground = true after a light response, want the fixed dark palette")
	}

	preview := NewPreviewModelConfigured("/abs/file.txt", nil, "", PreviewModelConfig{
		Preferences: Preferences{AppearanceMode: "dark"},
	}, &fakePreviewReader{content: []byte("x")})
	preview.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	if preview.lightBackground {
		t.Fatal("preview lightBackground = true after a light response, want the fixed dark palette")
	}
}

func TestIconBaseSetSwitchesOnlyTheThreeBasicGlyphs(t *testing.T) {
	fake := newFakeFileSystem()
	root := t.TempDir()
	fake.set(root, []filesystem.Entry{
		{Name: "docs", Mode: fs.ModeDir | 0o755},
		{Name: "mystery", Mode: 0},
	})
	tree := NewModelConfigured(root, "", ModelConfig{Preferences: Preferences{IconBaseSet: "material"}}, fake)
	completeInitialLoad(t, tree)

	rows := map[string]string{}
	for index, row := range tree.visibleRows {
		if row.Node == nil || row.Node.Parent() == nil {
			continue
		}
		rows[row.Node.Name()] = tree.renderRowWidth(index, row, 40)
	}
	if !strings.Contains(rows["docs"], "󰉋") {
		t.Fatalf("material closed folder row = %q, want the material directory glyph", rows["docs"])
	}
	if !strings.Contains(rows["mystery"], "󰈙") {
		t.Fatalf("material unknown file row = %q, want the material file glyph", rows["mystery"])
	}
}

func TestIconBaseSetDefaultsToFontAwesomeSolid(t *testing.T) {
	fake := newFakeFileSystem()
	root := t.TempDir()
	fake.set(root, nil)
	for _, config := range []ModelConfig{
		{},
		{Preferences: Preferences{IconBaseSet: ""}},
		{Preferences: Preferences{IconBaseSet: "no-such-set"}},
	} {
		tree := NewModelConfigured(root, "", config, fake)
		if tree.iconBaseSet != defaultTreeIconSet {
			t.Fatalf("IconBaseSet %q produced set %v, want the default set", config.Preferences.IconBaseSet, tree.iconBaseSet)
		}
	}
}

func TestTreePreferencesWarningSurfacesAsStartupToast(t *testing.T) {
	fake := newFakeFileSystem()
	root := t.TempDir()
	fake.set(root, nil)
	tree := NewModelConfigured(root, "", ModelConfig{PreferencesWarning: "preferences.json: boom"}, fake)
	commands := initCommandBatch(t, tree.Init())
	var warning preferencesWarningMsg
	found := false
	for _, command := range commands {
		message := command()
		if msg, ok := message.(preferencesWarningMsg); ok {
			warning = msg
			found = true
		}
	}
	if !found {
		t.Fatal("Init() returned no preferencesWarningMsg although the document was rejected")
	}
	tree.Update(warning)
	if tree.toast != "preferences.json: boom" {
		t.Fatalf("toast = %q, want the preferences warning", tree.toast)
	}
}

func TestPreviewTogglePersistsTheChangedValueImmediately(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("aa bb cc dd\nsecond line")}
	var savedWrap *bool
	var savedWhitespace *bool
	model := NewPreviewModelConfigured("/abs/prefs.txt", nil, "", PreviewModelConfig{
		SaveWrap: func(wrap bool) error {
			savedWrap = &wrap
			return nil
		},
		SaveShowWhitespace: func(show bool) error {
			savedWhitespace = &show
			return nil
		},
	}, reader)
	model.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	model.Update(previewLoadResult(t, model.Init()))

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if savedWrap == nil || !*savedWrap {
		t.Fatalf("wrap toggle did not persist wrap=true (saved = %v)", savedWrap)
	}
	if savedWhitespace != nil {
		t.Fatalf("wrap toggle persisted whitespace (saved = %v), want only the changed key", savedWhitespace)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if savedWrap == nil || *savedWrap {
		t.Fatalf("wrap toggle did not persist wrap=false (saved = %v)", savedWrap)
	}

	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if savedWhitespace == nil || !*savedWhitespace {
		t.Fatalf("whitespace toggle did not persist show_whitespace=true (saved = %v)", savedWhitespace)
	}
	if savedWrap == nil || *savedWrap {
		t.Fatalf("whitespace toggle re-persisted wrap (saved = %v), want only the changed key", savedWrap)
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if savedWhitespace == nil || *savedWhitespace {
		t.Fatalf("whitespace toggle did not persist show_whitespace=false (saved = %v)", savedWhitespace)
	}
}

func TestPreviewToggleWithoutSaverKeepsInMemoryOnly(t *testing.T) {
	model := NewPreviewModel("/abs/prefs.txt", nil, "", &fakePreviewReader{content: []byte("x")})
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !model.wrap {
		t.Fatal("wrap toggle without a saver did not apply")
	}
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !model.showWhitespace {
		t.Fatal("whitespace toggle without a saver did not apply")
	}
}

func TestPreviewSaveFailureSurfacesFooterWarning(t *testing.T) {
	model := NewPreviewModelConfigured("/abs/prefs.txt", nil, "", PreviewModelConfig{
		SaveWrap: func(bool) error { return errors.New("read-only state dir") },
	}, &fakePreviewReader{content: []byte("x")})
	model.UpdateKeyPreview(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !strings.Contains(model.warning, "preferences: read-only state dir") {
		t.Fatalf("warning = %q, want the preferences save failure", model.warning)
	}
	if !model.wrap {
		t.Fatal("failed save must keep the toggle applied")
	}
}

func TestPreviewInitialTogglesApplyFromPreferences(t *testing.T) {
	reader := &fakePreviewReader{content: []byte("alpha beta gamma delta\nsecond")}
	model := NewPreviewModelConfigured("/abs/prefs.txt", nil, "", PreviewModelConfig{
		Wrap:           true,
		ShowWhitespace: true,
	}, reader)
	model.Update(tea.WindowSizeMsg{Width: 14, Height: 7})
	model.Update(previewLoadResult(t, model.Init()))

	if !model.wrap {
		t.Fatal("wrap = false, want the saved wrap preference applied")
	}
	if !model.showWhitespace {
		t.Fatal("showWhitespace = false, want the saved whitespace preference applied")
	}
	if len(model.displayLines) <= 2 {
		t.Fatalf("displayLines = %d rows, want wrapped rows from the saved wrap preference", len(model.displayLines))
	}
	rendered := ""
	for _, line := range model.displayLines {
		if strings.Contains(line.text, " ") {
			rendered = model.renderContent(line, model.contentWidth())
			break
		}
	}
	if !strings.Contains(rendered, previewWhitespaceGlyph) {
		t.Fatalf("rendered content = %q, want %q from the saved whitespace preference", rendered, previewWhitespaceGlyph)
	}
}

func TestPreviewPreferencesWarningSurfacesAsStartupToast(t *testing.T) {
	preview := NewPreviewModelConfigured("/abs/file.txt", nil, "", PreviewModelConfig{
		PreferencesWarning: "preferences.json: boom",
	}, &fakePreviewReader{content: []byte("x")})
	commands := initCommandBatch(t, preview.Init())
	for _, command := range commands {
		if msg, ok := command().(preferencesWarningMsg); ok {
			preview.Update(msg)
			if preview.toast != "preferences.json: boom" {
				t.Fatalf("toast = %q, want the preferences warning", preview.toast)
			}
			return
		}
	}
	t.Fatal("Init() returned no preferencesWarningMsg although the document was rejected")
}
