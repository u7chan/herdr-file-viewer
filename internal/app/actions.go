package app

import "strings"

// DefaultActions carries the resolved per-type default-action command
// strings from preferences.json ("actions" section). The zero value and
// empty strings mean the action is unset: Ctrl+Enter stays a silent no-op
// and the Help popup omits its row. Hand-edited strings are evaluated by
// the user's interactive shell, so no validation is applied beyond
// non-empty.
type DefaultActions struct {
	File   string
	Folder string
}

// DefaultActionRunner launches one assembled default-action command
// detached from the TUI. The composition root supplies the implementation
// (bash -lic with a new session); tests supply deterministic doubles.
type DefaultActionRunner interface {
	// Run starts the command without waiting for it to finish. A returned
	// error means the launch itself failed.
	Run(command string) error
}

const (
	// actionPathTokenFile and actionPathTokenFolder are the fixed
	// placeholders of the actions.file / actions.folder command strings.
	// Each is replaced at run time by the selected path with shell quoting
	// applied automatically.
	actionPathTokenFile   = "<filepath>"
	actionPathTokenFolder = "<dirpath>"
)

// defaultActionCommand substitutes every occurrence of the path token in
// the template with shellQuote(path) so paths containing spaces or quotes
// survive one round of shell evaluation intact.
func defaultActionCommand(template, token, path string) string {
	return strings.ReplaceAll(template, token, shellQuote(path))
}

// shellQuote wraps value in single quotes for a POSIX shell, escaping
// embedded single quotes as '\'' so the quoted text always evaluates back
// to the original bytes.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}