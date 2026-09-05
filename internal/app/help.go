package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// helpTreeContext and helpPreviewContext identify the caller of the
	// help popup through HelpOpenRequest.Context.
	helpTreeContext    = "tree"
	helpPreviewContext = "preview"
)

// HelpOpenRequest describes one Help popup launch.
type HelpOpenRequest struct {
	// Context identifies the caller: helpTreeContext or helpPreviewContext.
	Context string
}

// HelpClient is the Herdr popup capability the tree and preview models
// need, kept as an interface so the composition root can inject the
// subprocess implementation and tests can supply deterministic doubles.
type HelpClient interface {
	// OpenHelp opens the help entrypoint as a focused popup and returns
	// the new pane ID. Popup panes always target the active pane.
	OpenHelp(request HelpOpenRequest) (paneID string, err error)
}

// HelpConfig wires a model to the help popup capability. A nil Client
// keeps h a warning-only no-op, which is how the composition root expresses
// a missing Herdr pane context.
type HelpConfig struct {
	Client HelpClient
}

// helpResultMsg is the outcome of one help popup launch. The opened pane
// takes the keyboard focus itself, so a successful launch needs no further
// bookkeeping in the caller.
type helpResultMsg struct {
	err string
}

// helpEntry is one key-reference row of the help popup.
type helpEntry struct {
	keys  string
	label string
}

// helpTreeRows and helpPreviewRows list the fixed key reference rows per
// caller context. The tree reference is split from the configured
// default-action rows, which helpRowsFor inserts after the preview row
// only when the corresponding action is non-empty; keys carry the shortcut
// emphasis and label the muted description, mirroring the footer rendering.
var helpTreeRows = []helpEntry{
	{keys: "Up / Down, j / k", label: "move"},
	{keys: "Ctrl+u / Ctrl+d, Ctrl+b / Ctrl+f, PageUp / PageDown", label: "half-page / page"},
	{keys: "Home / End", label: "first / last row"},
	{keys: "Right / Left", label: "expand / collapse / parent"},
	{keys: "C / Backspace", label: "root move"},
	{keys: "Enter", label: "preview file"},
	{keys: "/, n, N, Esc", label: "find"},
	{keys: "r", label: "reload"},
	{keys: "space", label: "copy path"},
	{keys: "mouse", label: "click / wheel / scrollbar drag"},
	{keys: "q / Ctrl+C", label: "quit"},
}

// helpPreviewRows is the fixed preview reference; default actions are a
// tree-only key, so no conditional rows exist for the preview context.
var helpPreviewRows = []helpEntry{
	{keys: "Up / Down, j / k", label: "vertical scroll"},
	{keys: "Ctrl+u / Ctrl+d, Ctrl+b / Ctrl+f, PageUp / PageDown", label: "half-page / page"},
	{keys: "Home / End", label: "first / last row"},
	{keys: "Left / Right", label: "horizontal scroll"},
	{keys: "w", label: "wrap"},
	{keys: "s", label: "spaces"},
	{keys: "r", label: "reload"},
	{keys: "space", label: "copy selection"},
	{keys: "mouse", label: "drag select / wheel / scrollbar"},
	{keys: "q / Ctrl+C", label: "close"},
}

// helpRowsFor returns the reference rows for one caller context. The tree
// reference gains one Ctrl+Enter row per configured default action,
// positioned right after the preview row; an unset action contributes no
// row at all.
func helpRowsFor(context string, actions DefaultActions) []helpEntry {
	if context != helpTreeContext {
		return helpPreviewRows
	}
	rows := make([]helpEntry, 0, len(helpTreeRows)+2)
	for _, row := range helpTreeRows {
		rows = append(rows, row)
		if row.keys != "Enter" {
			continue
		}
		if actions.File != "" {
			rows = append(rows, helpEntry{keys: "Ctrl+Enter", label: "run file default action"})
		}
		if actions.Folder != "" {
			rows = append(rows, helpEntry{keys: "Ctrl+Enter", label: "run folder default action"})
		}
	}
	return rows
}

// HelpModel is the read-only Herdr popup help pane. It renders the key
// reference of its caller context and closes itself on h, Esc, or q so the
// popup never leaks keys back to the tree or preview underneath.
type HelpModel struct {
	entries []helpEntry
	width   int
	height  int
}

// NewHelpModel constructs the help popup for one caller context. The
// actions argument carries the resolved default-action commands and
// contributes the Ctrl+Enter rows to the tree reference; an omitted or
// empty value renders the fixed reference only.
func NewHelpModel(context string, actions ...DefaultActions) *HelpModel {
	if context != helpTreeContext && context != helpPreviewContext {
		context = helpTreeContext
	}
	var configured DefaultActions
	if len(actions) > 0 {
		configured = actions[0]
	}
	return &HelpModel{entries: helpRowsFor(context, configured)}
}

// Init has no startup work: the popup renders from its constructor state.
func (m *HelpModel) Init() tea.Cmd {
	return nil
}

// Update applies resizes and the close keys. Every other key is ignored so
// the popup cannot drive the pane underneath.
func (m *HelpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = nonNegative(msg.Width)
		m.height = nonNegative(msg.Height)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "h", "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// View renders the fixed reference rows for the caller context, truncating
// each row to the pane width. The pane frame itself carries the title (the
// popup bar), so no in-content header is drawn here.
func (m *HelpModel) View() tea.View {
	if m == nil {
		view := tea.NewView("")
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		return view
	}

	lines := make([]string, 0, len(m.entries))
	for _, entry := range m.entries {
		lines = append(lines, m.renderHelpRow(entry))
	}
	if m.height > 0 && len(lines) > m.height {
		lines = lines[:m.height]
	}

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m *HelpModel) renderHelpRow(entry helpEntry) string {
	line := " " + renderShortcut(entry.keys, entry.label)
	return renderStyledLineAt(line, lipgloss.NewStyle(), m.width)
}
