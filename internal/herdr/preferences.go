package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// preferencesFileName is the single document holding hand-edited settings
// and the mutable preview toggles the app rewrites. It lives in
// HERDR_PLUGIN_CONFIG_DIR (the user-editable plugin config directory),
// separate from the per-pane preview restore state under
// preview/<namespace>/<pane>.json in HERDR_PLUGIN_STATE_DIR.
const preferencesFileName = "preferences.json"

// Preferences is the resolved preferences document. Absent fields decode
// to zero values before resolution and then map onto the built-in
// defaults, so a missing file or a rejected document resolves to the same
// defaults and no absent-tracking is needed.
type Preferences struct {
	AppearanceMode        string // "auto", "light", or "dark"; defaults to "auto"
	IconBaseSet           string // one of the four basic icon set names; defaults to "font-awesome-solid"
	PreviewWrap           bool
	PreviewShowWhitespace bool
	// ActionFile and ActionFolder are the configured per-type default
	// action command strings; empty means the action is unset. Hand-edited
	// strings are evaluated by the user's interactive shell, so any content
	// is accepted without validation.
	ActionFile   string
	ActionFolder string
}

const (
	defaultAppearanceMode = "auto"
	appearanceModeLight   = "light"
	appearanceModeDark    = "dark"

	defaultIconBaseSet  = "font-awesome-solid"
	iconBaseSetOutline  = "font-awesome-outline"
	iconBaseSetMaterial = "material"
	iconBaseSetCodicon  = "codicon"
)

// preferencesDoc mirrors the JSON document. Unknown keys and sections are
// ignored by encoding/json. String fields are resolved after decoding, so ""
// means the key was absent (or explicitly empty) and maps to the built-in
// default rather than an error.
type preferencesDoc struct {
	Appearance appearanceDoc `json:"appearance"`
	Icons      iconsDoc      `json:"icons"`
	Preview    previewDoc    `json:"preview"`
	Actions    actionsDoc    `json:"actions"`
}

type appearanceDoc struct {
	Mode string `json:"mode"`
}

type iconsDoc struct {
	BaseSet string `json:"base_set"`
}

type previewDoc struct {
	Wrap           bool `json:"wrap"`
	ShowWhitespace bool `json:"show_whitespace"`
}

type actionsDoc struct {
	File   string `json:"file"`
	Folder string `json:"folder"`
}

// PreferencesStore persists the single preferences document under
// HERDR_PLUGIN_CONFIG_DIR. An empty config dir leaves the store detached:
// every operation becomes a no-op returning defaults, so runs outside a
// Herdr context (direct terminal, tests) never touch the filesystem.
type PreferencesStore struct {
	path string
}

// NewPreferencesStore builds the store. An empty or blank config dir leaves
// the store detached.
func NewPreferencesStore(configDir string) *PreferencesStore {
	if strings.TrimSpace(configDir) == "" {
		return &PreferencesStore{}
	}
	return &PreferencesStore{path: filepath.Join(configDir, preferencesFileName)}
}

func (s *PreferencesStore) detached() bool {
	return s.path == ""
}

// Load returns the resolved preferences. A missing file is a first run: the
// default document is written so the hand-editing entry point exists, and
// the defaults are returned without an error. A failed creation or any other
// read, parse, or validation failure yields the defaults with an error
// describing the rejection, so the caller can warn.
func (s *PreferencesStore) Load() (Preferences, error) {
	if s.detached() {
		return preferencesDefaults(), nil
	}
	content, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := s.writeDoc(defaultPreferencesDoc()); err != nil {
				return preferencesDefaults(), err
			}
			return preferencesDefaults(), nil
		}
		return preferencesDefaults(), fmt.Errorf("read %s: %w", preferencesFileName, err)
	}
	var doc preferencesDoc
	if err := json.Unmarshal(content, &doc); err != nil {
		return preferencesDefaults(), fmt.Errorf("%s: %w", preferencesFileName, err)
	}
	resolved, err := doc.resolve()
	if err != nil {
		return preferencesDefaults(), fmt.Errorf("%s: %w", preferencesFileName, err)
	}
	return resolved, nil
}

func preferencesDefaults() Preferences {
	return Preferences{
		AppearanceMode: defaultAppearanceMode,
		IconBaseSet:    defaultIconBaseSet,
	}
}

// defaultPreferencesDoc is the document written on first run. The preview
// toggles are emitted as explicit false values and the action strings as
// explicit empty values so the created file shows every key a user can
// edit; either representation resolves to the same defaults.
func defaultPreferencesDoc() preferencesDoc {
	return preferencesDoc{
		Appearance: appearanceDoc{Mode: defaultAppearanceMode},
		Icons:      iconsDoc{BaseSet: defaultIconBaseSet},
	}
}

func (d preferencesDoc) resolve() (Preferences, error) {
	mode := d.Appearance.Mode
	if mode == "" {
		mode = defaultAppearanceMode
	} else if !validAppearanceMode(mode) {
		return Preferences{}, fmt.Errorf("invalid appearance.mode %q", mode)
	}

	baseSet := d.Icons.BaseSet
	if baseSet == "" {
		baseSet = defaultIconBaseSet
	} else if !validIconBaseSet(baseSet) {
		return Preferences{}, fmt.Errorf("invalid icons.base_set %q", baseSet)
	}

	return Preferences{
		AppearanceMode:        mode,
		IconBaseSet:           baseSet,
		PreviewWrap:           d.Preview.Wrap,
		PreviewShowWhitespace: d.Preview.ShowWhitespace,
		ActionFile:            d.Actions.File,
		ActionFolder:          d.Actions.Folder,
	}, nil
}

func validAppearanceMode(mode string) bool {
	switch mode {
	case defaultAppearanceMode, appearanceModeLight, appearanceModeDark:
		return true
	}
	return false
}

func validIconBaseSet(name string) bool {
	switch name {
	case defaultIconBaseSet, iconBaseSetOutline, iconBaseSetMaterial, iconBaseSetCodicon:
		return true
	}
	return false
}

// SavePreviewWrap persists the wrap toggle. Only the changed key is merged
// into the current document, so hand-edited appearance/icons/actions
// sections and another preview's independent show_whitespace value survive
// the write.
func (s *PreferencesStore) SavePreviewWrap(wrap bool) error {
	return s.updatePreview(func(doc *preferencesDoc) { doc.Preview.Wrap = wrap })
}

// SavePreviewShowWhitespace persists the whitespace toggle, merging only the
// changed key into the current document.
func (s *PreferencesStore) SavePreviewShowWhitespace(show bool) error {
	return s.updatePreview(func(doc *preferencesDoc) { doc.Preview.ShowWhitespace = show })
}

// updatePreview merges one preview key into the current document on disk and
// writes the whole known document back with a simple os.WriteFile; the
// fsync + rename atomicity of PreviewStateStore is deliberately not copied
// because these are settings whose loss is tolerable. Unknown keys in the
// existing file are not carried over (the schema is all the app owns). A
// document that cannot be read or parsed is treated as missing, so a write
// never fails on a previously-broken file and instead replaces it.
func (s *PreferencesStore) updatePreview(apply func(*preferencesDoc)) error {
	if s.detached() {
		return nil
	}
	var doc preferencesDoc
	if content, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(content, &doc)
	}
	apply(&doc)
	return s.writeDoc(doc)
}

// writeDoc marshals and writes the whole known document with a simple
// os.WriteFile. The parent directory is created on demand because a fresh
// Herdr install may not have materialized the plugin config dir yet.
func (s *PreferencesStore) writeDoc(doc preferencesDoc) error {
	content, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", preferencesFileName, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create preferences config dir: %w", err)
	}
	if err := os.WriteFile(s.path, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", preferencesFileName, err)
	}
	return nil
}
