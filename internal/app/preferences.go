package app

// preferencesWarningMsg triggers the startup toast for a rejected
// preferences.json. The text is sanitized when the config is applied, so
// Update can render it directly.
type preferencesWarningMsg struct {
	text string
}

// Preferences carries the resolved startup preferences for one process. The
// composition root fills it from preferences.json (internal/herdr); the zero
// value matches every built-in default, so models constructed without
// preferences behave exactly as before. Only resolved values arrive here:
// the store has already applied defaults and rejected invalid documents, so
// the models just fall back to the same defaults for empty or unknown
// values.
type Preferences struct {
	// AppearanceMode is the resolved appearance.mode: "auto", "light", or
	// "dark". auto keeps OSC 11 detection with the dark fallback; light and
	// dark fix the palette regardless of the terminal response. Empty and
	// unknown values fall back to auto.
	AppearanceMode string
	// IconBaseSet is the resolved icons.base_set name. It switches only the
	// closed folder, open folder, and unknown-file glyphs. Empty and unknown
	// values fall back to the built-in font-awesome-solid set.
	IconBaseSet string
}

const (
	appearanceModeLight = "light"
	appearanceModeDark  = "dark"
)

// resolveAppearance maps the resolved appearance.mode onto the palette
// behavior. auto is adaptive: the dark palette rules until an OSC 11
// response says otherwise. light and dark fix the palette deterministically,
// which also keeps ANSI-256-only terminals stable.
func resolveAppearance(mode string) (lightBackground, fixed bool) {
	switch mode {
	case appearanceModeLight:
		return true, true
	case appearanceModeDark:
		return false, true
	default:
		return false, false
	}
}
