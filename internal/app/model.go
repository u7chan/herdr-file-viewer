package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// Model is the small Bubble Tea shell used until the browser Sub-Issues add
// tree state and asynchronous filesystem commands.
type Model struct {
	root    string
	warning string
	width   int
	height  int
}

// NewModel constructs the foundation shell without performing I/O.
func NewModel(root, warning string) Model {
	return Model{root: root, warning: warning}
}

// Init has no startup command: root resolution already happened in the
// composition root and this foundation deliberately does not poll.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update applies terminal messages and handles the foundation quit contract.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case tea.InterruptMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width = nonNegative(msg.Width)
		m.height = nonNegative(msg.Height)
	}
	return m, nil
}

// View renders only the already-resolved startup state. Bubble Tea v2 owns
// terminal mode transitions and restores them when the program exits.
func (m Model) View() tea.View {
	lines := []string{
		titleStyle.Render("Herdr File Viewer"),
		fmt.Sprintf("Root: %s", m.root),
		fmt.Sprintf("Window: %d x %d", m.width, m.height),
		"Press q or Ctrl+C to quit.",
	}
	if m.warning != "" {
		lines = append(lines, warningStyle.Render("Warning: "+m.warning))
	}

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
