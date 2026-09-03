package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// helpTreeContext and helpPreviewContext identify the caller of the
	// help overlay through HelpOpenRequest.Context.
	helpTreeContext    = "tree"
	helpPreviewContext = "preview"

	helpTitleTree    = "File Viewer Help"
	helpTitlePreview = "Preview Help"
)

// HelpOpenRequest describes one Help overlay launch.
type HelpOpenRequest struct {
	// Context identifies the caller: helpTreeContext or helpPreviewContext.
	Context string
}

// HelpClient is the Herdr overlay capability the tree and preview models
// need, kept as an interface so the composition root can inject the
// subprocess implementation and tests can supply deterministic doubles.
type HelpClient interface {
	// OpenHelp opens the help entrypoint as a focused overlay and returns
	// the new pane ID. Overlays always target the active pane.
	OpenHelp(request HelpOpenRequest) (paneID string, err error)
}

// HelpConfig wires a model to the help-overlay capability. A nil Client
// keeps h a warning-only no-op, which is how the composition root expresses
// a missing Herdr pane context.
type HelpConfig struct {
	Client HelpClient
}

// helpResultMsg is the outcome of one help overlay launch. The opened pane
// takes the keyboard focus itself, so a successful launch needs no further
// bookkeeping in the caller.
type helpResultMsg struct {
	err string
}

// helpEntry is one key-reference row of a help overlay.
type helpEntry struct {
	keys  string
	label string
}

// helpContent lists the published operations per caller context. The rows
// are the fixed key reference: keys carry the shortcut emphasis and label
// the muted description, mirroring the footer rendering.
var helpContent = map[string][]helpEntry{
	helpTreeContext: {
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
	},
	helpPreviewContext: {
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
	},
}

// HelpModel is the read-only Herdr overlay help pane. It renders the key
// reference of its caller context and closes itself on h, Esc, or q so the
// overlay never leaks keys back to the tree or preview underneath.
type HelpModel struct {
	context string
	width   int
	height  int
}

// NewHelpModel constructs the help overlay for one caller context. An
// unknown or empty context falls back to the tree reference; the fallback
// keeps the entrypoint usable when the launch environment was stripped.
func NewHelpModel(context string) *HelpModel {
	if context != helpTreeContext && context != helpPreviewContext {
		context = helpTreeContext
	}
	return &HelpModel{context: context}
}

// Init has no startup work: the overlay renders from its constructor state.
func (m *HelpModel) Init() tea.Cmd {
	return nil
}

// Update applies resizes and the close keys. Every other key is ignored so
// the overlay cannot drive the pane underneath.
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

// View renders the title, divider, and the fixed reference rows for the
// caller context, truncating each row to the pane width.
func (m *HelpModel) View() tea.View {
	if m == nil {
		view := tea.NewView("")
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		return view
	}

	lines := []string{renderStyledLineAt(m.helpTitle(), titleStyle, m.width)}
	if headerDividerHeight(m.height) > 0 {
		lines = append(lines, m.renderDivider())
	}
	for _, entry := range helpContent[m.context] {
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

func (m *HelpModel) helpTitle() string {
	if m.context == helpPreviewContext {
		return helpTitlePreview
	}
	return helpTitleTree
}

func (m *HelpModel) renderDivider() string {
	return renderStyledLineAt(strings.Repeat(dividerGlyph, m.width), dividerStyle, m.width)
}

func (m *HelpModel) renderHelpRow(entry helpEntry) string {
	line := " " + renderShortcut(entry.keys, entry.label)
	return renderStyledLineAt(line, lipgloss.NewStyle(), m.width)
}
