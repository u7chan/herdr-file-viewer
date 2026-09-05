package herdr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writePreferences(t *testing.T, configDir, content string) {
	t.Helper()
	path := filepath.Join(configDir, preferencesFileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func TestPreferencesStoreLoadCreatesDefaultsWhenFileIsMissing(t *testing.T) {
	configDir := t.TempDir()
	store := NewPreferencesStore(configDir)
	prefs, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for a missing file (first run)", err)
	}
	want := Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}
	if prefs != want {
		t.Fatalf("Load() = %#v, want defaults %#v", prefs, want)
	}

	content, err := os.ReadFile(filepath.Join(configDir, preferencesFileName))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v, want the default document created on first run", preferencesFileName, err)
	}
	var doc preferencesDoc
	if err := json.Unmarshal(content, &doc); err != nil {
		t.Fatalf("created %s = %q is not valid JSON: %v", preferencesFileName, content, err)
	}
	if doc.Appearance.Mode != defaultAppearanceMode || doc.Icons.BaseSet != defaultIconBaseSet {
		t.Fatalf("created document = %s, want the default appearance/icon values", content)
	}
}

func TestPreferencesStoreLoadRejectsFailedFirstRunCreation(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	prefs, err := NewPreferencesStore(filepath.Join(blocker, "sub")).Load()
	if err == nil {
		t.Fatal("Load() error = nil, want rejection when the default document cannot be created")
	}
	if prefs != (Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}) {
		t.Fatalf("Load() = %#v, want defaults on failed creation", prefs)
	}
}

func TestPreferencesStoreLoadResolvesEverySection(t *testing.T) {
	configDir := t.TempDir()
	writePreferences(t, configDir, `{
		"appearance": {"mode": "light"},
		"icons": {"base_set": "material"},
		"preview": {"wrap": true, "show_whitespace": true},
		"unknown_section": {"anything": 1}
	}`)
	prefs, err := NewPreferencesStore(configDir).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Preferences{
		AppearanceMode:        "light",
		IconBaseSet:           "material",
		PreviewWrap:           true,
		PreviewShowWhitespace: true,
	}
	if prefs != want {
		t.Fatalf("Load() = %#v, want %#v", prefs, want)
	}
}

func TestPreferencesStoreLoadTreatsEmptyDocumentAsDefaults(t *testing.T) {
	configDir := t.TempDir()
	writePreferences(t, configDir, `{}`)
	prefs, err := NewPreferencesStore(configDir).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}
	if prefs != want {
		t.Fatalf("Load() = %#v, want defaults %#v", prefs, want)
	}
}

func TestPreferencesStoreLoadMapsEmptyStringsToDefaults(t *testing.T) {
	configDir := t.TempDir()
	writePreferences(t, configDir, `{
		"appearance": {"mode": ""},
		"icons": {"base_set": ""},
		"preview": {"wrap": true}
	}`)
	prefs, err := NewPreferencesStore(configDir).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid", PreviewWrap: true}
	if prefs != want {
		t.Fatalf("Load() = %#v, want %#v", prefs, want)
	}
}

func TestPreferencesStoreLoadIgnoresNullSections(t *testing.T) {
	configDir := t.TempDir()
	writePreferences(t, configDir, `{"appearance": null, "icons": null, "preview": null}`)
	prefs, err := NewPreferencesStore(configDir).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}
	if prefs != want {
		t.Fatalf("Load() = %#v, want defaults %#v", prefs, want)
	}
}

func TestPreferencesStoreLoadRejectsSyntaxErrors(t *testing.T) {
	configDir := t.TempDir()
	writePreferences(t, configDir, `{"preview": {"wrap": true`)
	prefs, err := NewPreferencesStore(configDir).Load()
	if err == nil {
		t.Fatal("Load() error = nil, want rejection of invalid JSON")
	}
	if prefs != (Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}) {
		t.Fatalf("Load() = %#v, want defaults on rejection", prefs)
	}
}

func TestPreferencesStoreLoadRejectsWrongTypes(t *testing.T) {
	tests := map[string]string{
		"boolean wrap as string":  `{"preview": {"wrap": "yes"}}`,
		"boolean wrap as number":  `{"preview": {"wrap": 1}}`,
		"mode as number":          `{"appearance": {"mode": 5}}`,
		"appearance as number":    `{"appearance": 5}`,
		"base_set as boolean":     `{"icons": {"base_set": true}}`,
		"show_whitespace as text": `{"preview": {"show_whitespace": "on"}}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			configDir := t.TempDir()
			writePreferences(t, configDir, content)
			prefs, err := NewPreferencesStore(configDir).Load()
			if err == nil {
				t.Fatal("Load() error = nil, want rejection of wrong type")
			}
			if prefs != (Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}) {
				t.Fatalf("Load() = %#v, want defaults on rejection", prefs)
			}
		})
	}
}

func TestPreferencesStoreLoadRejectsUnknownEnumValues(t *testing.T) {
	tests := map[string]string{
		"unknown appearance mode": `{"appearance": {"mode": "sepia"}}`,
		"unknown icon base set":   `{"icons": {"base_set": "emoji"}}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			configDir := t.TempDir()
			writePreferences(t, configDir, content)
			prefs, err := NewPreferencesStore(configDir).Load()
			if err == nil {
				t.Fatal("Load() error = nil, want rejection of unknown enum value")
			}
			if prefs != (Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}) {
				t.Fatalf("Load() = %#v, want defaults on rejection", prefs)
			}
		})
	}
}

func TestPreferencesStoreLoadRejectsUnreadableFile(t *testing.T) {
	configDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(configDir, preferencesFileName), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	prefs, err := NewPreferencesStore(configDir).Load()
	if err == nil {
		t.Fatal("Load() error = nil, want rejection of an unreadable file")
	}
	if prefs != (Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}) {
		t.Fatalf("Load() = %#v, want defaults on rejection", prefs)
	}
}

func TestPreferencesStoreDetachedNeverReadsOrWrites(t *testing.T) {
	store := NewPreferencesStore("")
	prefs, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if prefs != (Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}) {
		t.Fatalf("Load() = %#v, want defaults", prefs)
	}
	if err := store.SavePreviewWrap(true); err != nil {
		t.Fatalf("SavePreviewWrap() error = %v, want nil for a detached store", err)
	}
	if err := store.SavePreviewShowWhitespace(true); err != nil {
		t.Fatalf("SavePreviewShowWhitespace() error = %v, want nil for a detached store", err)
	}
}

func TestPreferencesStoreSavePreviewWrapCreatesTheFileAndMergesOnlyTheChangedKey(t *testing.T) {
	configDir := t.TempDir()
	writePreferences(t, configDir, `{
		"appearance": {"mode": "dark"},
		"icons": {"base_set": "codicon"},
		"preview": {"show_whitespace": true}
	}`)
	store := NewPreferencesStore(configDir)

	if err := store.SavePreviewWrap(true); err != nil {
		t.Fatalf("SavePreviewWrap() error = %v", err)
	}
	prefs, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Preferences{
		AppearanceMode:        "dark",
		IconBaseSet:           "codicon",
		PreviewWrap:           true,
		PreviewShowWhitespace: true,
	}
	if prefs != want {
		t.Fatalf("Load() after save = %#v, want %#v (unchanged keys must survive)", prefs, want)
	}

	if err := store.SavePreviewWrap(false); err != nil {
		t.Fatalf("SavePreviewWrap(false) error = %v", err)
	}
	prefs, err = store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if prefs.PreviewWrap || !prefs.PreviewShowWhitespace || prefs.AppearanceMode != "dark" {
		t.Fatalf("Load() after second save = %#v, want wrap off, whitespace and appearance untouched", prefs)
	}
}

func TestPreferencesStoreSavePreviewWhitespaceCreatesFileWhenMissing(t *testing.T) {
	store := NewPreferencesStore(t.TempDir())
	if err := store.SavePreviewShowWhitespace(true); err != nil {
		t.Fatalf("SavePreviewShowWhitespace() error = %v", err)
	}
	prefs, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Preferences{
		AppearanceMode:        "auto",
		IconBaseSet:           "font-awesome-solid",
		PreviewShowWhitespace: true,
	}
	if prefs != want {
		t.Fatalf("Load() after save = %#v, want %#v", prefs, want)
	}
}

func TestPreferencesStoreSaveReplacesABrokenFileWithAValidDocument(t *testing.T) {
	configDir := t.TempDir()
	writePreferences(t, configDir, `{broken`)
	store := NewPreferencesStore(configDir)
	if err := store.SavePreviewWrap(true); err != nil {
		t.Fatalf("SavePreviewWrap() error = %v", err)
	}
	prefs, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after replacing a broken file error = %v, want a valid document", err)
	}
	if !prefs.PreviewWrap {
		t.Fatalf("Load() after save = %#v, want wrap true", prefs)
	}
}
