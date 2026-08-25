package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("238"))
)

const (
	loadingStatus = "Loading directory..."
	readyStatus   = "Ready"
)

// Model owns UI state and delegates all filesystem work to commands that read
// the browser tree outside Update and View.
type Model struct {
	warning string

	tree        *browser.Tree
	visibleRows []browser.VisibleRow
	pending     map[string]struct{}

	selected int
	offset   int
	width    int
	height   int

	loading bool
	status  string
}

// NewModel constructs the tree without reading the filesystem. A filesystem
// argument is optional so the composition root can use the local filesystem
// while tests can provide a deterministic implementation.
func NewModel(root, warning string, fileSystems ...filesystem.FileSystem) *Model {
	fileSystem := filesystem.FileSystem(filesystem.NewLocal())
	if len(fileSystems) > 0 && fileSystems[0] != nil {
		fileSystem = fileSystems[0]
	}

	m := &Model{
		warning: sanitizeDisplay(warning),
		pending: make(map[string]struct{}),
	}
	m.status = m.readyStatus()

	tree, err := browser.NewTree(root, fileSystem)
	if err != nil {
		m.status = "Error: " + sanitizeDisplay(err.Error())
		return m
	}
	m.tree = tree
	m.refreshVisibleRows()
	return m
}

// Init expands the root and returns the first directory read as a command.
func (m *Model) Init() tea.Cmd {
	if m == nil || m.tree == nil {
		return nil
	}

	request, ok := m.tree.Expand(m.tree.Root())
	m.refreshVisibleRows()
	if !ok {
		return nil
	}
	return m.startLoad(request)
}

// Update applies messages and is the only place where load results mutate the
// browser tree. Key navigation only uses the cached visible rows.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}

	switch msg := msg.(type) {
	case browser.LoadResult:
		m.applyLoadResult(msg)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "space", "\u3000":
			return m, m.copySelection()
		case "up":
			m.moveSelection(-1)
		case "down":
			m.moveSelection(1)
		case "right":
			return m, m.expandSelection()
		case "left":
			m.collapseOrMoveToParent()
		case "enter":
			// Enter intentionally has no action in this read-only tree.
		}
	case tea.MouseClickMsg:
		return m, m.handleMouseClick(msg)
	case tea.InterruptMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width = nonNegative(msg.Width)
		m.height = nonNegative(msg.Height)
		m.keepSelectionVisible()
	}
	return m, nil
}

// View renders cached UI state only. In particular, it never asks the tree to
// rebuild its visible rows and never performs filesystem I/O.
func (m *Model) View() tea.View {
	if m == nil {
		view := tea.NewView("")
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		return view
	}

	headerHeight, treeHeight, statusHeight := layoutHeights(m.height)
	lines := make([]string, 0, headerHeight+treeHeight+statusHeight)
	if headerHeight > 0 {
		lines = append(lines, m.renderLine(titleStyle.Render("Herdr File Viewer")))
	}
	if treeHeight > 0 {
		lines = append(lines, m.renderTree(treeHeight)...)
	}
	if statusHeight > 0 {
		lines = append(lines, m.renderLine(m.status))
	}

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m *Model) copySelection() tea.Cmd {
	node := m.selectedNode()
	if node == nil {
		return nil
	}

	path := node.Path()
	m.status = "copied: " + sanitizeDisplay(path)
	return tea.SetClipboard(path)
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}

	index, ok := m.rowIndexAtY(msg.Y)
	if !ok {
		return nil
	}

	m.selected = index
	node := m.selectedNode()
	if node == nil || node.Parent() == nil || !node.IsDirectory() {
		return nil
	}

	if node.Expanded() {
		if m.tree.Collapse(node) {
			m.refreshVisibleRows()
		}
		return nil
	}

	request, expanded := m.tree.Expand(node)
	m.refreshVisibleRows()
	if !expanded {
		return nil
	}
	return m.startLoad(request)
}

func (m *Model) rowIndexAtY(y int) (int, bool) {
	if y < 0 {
		return 0, false
	}

	headerHeight, treeHeight, _ := layoutHeights(m.height)
	if treeHeight <= 0 || y < headerHeight || y >= headerHeight+treeHeight {
		return 0, false
	}

	index := m.offset + y - headerHeight
	if index < 0 || index >= len(m.visibleRows) {
		return 0, false
	}
	return index, true
}

func (m *Model) startLoad(request browser.LoadRequest) tea.Cmd {
	m.pending[request.Path] = struct{}{}
	m.loading = true
	m.status = loadingStatus
	return loadDirectory(m.tree, request)
}

func loadDirectory(tree *browser.Tree, request browser.LoadRequest) tea.Cmd {
	return func() tea.Msg {
		return tree.Read(request)
	}
}

func (m *Model) applyLoadResult(result browser.LoadResult) {
	if m.tree == nil || !m.tree.ApplyLoad(result) {
		return
	}

	delete(m.pending, result.Path)
	m.loading = len(m.pending) > 0
	if result.Err != nil {
		m.status = "Error: " + sanitizeDisplay(fmt.Sprintf("%s: %v", result.Path, result.Err))
	} else if m.loading {
		m.status = loadingStatus
	} else {
		m.status = m.readyStatus()
	}
	m.refreshVisibleRows()
}

func (m *Model) expandSelection() tea.Cmd {
	node := m.selectedNode()
	if node == nil || !node.IsDirectory() {
		return nil
	}

	request, ok := m.tree.Expand(node)
	m.refreshVisibleRows()
	if !ok {
		return nil
	}
	return m.startLoad(request)
}

func (m *Model) collapseOrMoveToParent() {
	node := m.selectedNode()
	if node == nil {
		return
	}
	if node.IsDirectory() && node.Expanded() {
		if m.tree.Collapse(node) {
			m.refreshVisibleRows()
		}
		return
	}

	parent := node.Parent()
	if parent == nil {
		return
	}
	if index := m.indexOfNode(parent); index >= 0 {
		m.selected = index
		m.keepSelectionVisible()
	}
}

func (m *Model) moveSelection(delta int) {
	if len(m.visibleRows) == 0 || delta == 0 {
		return
	}

	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.visibleRows) {
		m.selected = len(m.visibleRows) - 1
	}
	m.keepSelectionVisible()
}

func (m *Model) selectedNode() *browser.Node {
	if m.selected < 0 || m.selected >= len(m.visibleRows) {
		return nil
	}
	return m.visibleRows[m.selected].Node
}

func (m *Model) indexOfNode(want *browser.Node) int {
	for index, row := range m.visibleRows {
		if row.Node == want {
			return index
		}
	}
	return -1
}

func (m *Model) refreshVisibleRows() {
	if m.tree == nil || !m.tree.VisibleRowsDirty() {
		return
	}

	rows := m.tree.VisibleRows()
	m.visibleRows = append(m.visibleRows[:0], rows...)
	if len(m.visibleRows) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.visibleRows) {
		m.selected = len(m.visibleRows) - 1
	}
	m.keepSelectionVisible()
}

func (m *Model) keepSelectionVisible() {
	if len(m.visibleRows) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}

	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.visibleRows) {
		m.selected = len(m.visibleRows) - 1
	}

	_, treeHeight, _ := layoutHeights(m.height)
	if treeHeight <= 0 {
		m.offset = 0
		return
	}
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+treeHeight {
		m.offset = m.selected - treeHeight + 1
	}
	maxOffset := len(m.visibleRows) - treeHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

func (m *Model) renderTree(treeHeight int) []string {
	if treeHeight <= 0 || len(m.visibleRows) == 0 {
		return nil
	}

	start := m.offset
	if start < 0 {
		start = 0
	}
	if start >= len(m.visibleRows) {
		start = len(m.visibleRows) - 1
	}
	end := start + treeHeight
	if end > len(m.visibleRows) {
		end = len(m.visibleRows)
	}

	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		lines = append(lines, m.renderRow(index, m.visibleRows[index]))
	}
	return lines
}

func (m *Model) renderRow(index int, row browser.VisibleRow) string {
	if m.width <= 0 || row.Node == nil {
		return ""
	}

	indent := strings.Repeat("  ", row.Depth)
	name := row.Node.Name()
	if row.Node.Parent() == nil {
		name = row.Node.Path()
	}
	name = sanitizeDisplay(name)
	if row.Node.IsDirectory() {
		chevron := "▸"
		if row.Node.Expanded() {
			chevron = "▾"
		}
		name = indent + chevron + " " + name
	} else {
		name = indent + "  " + name
	}

	style := lipgloss.NewStyle().Inline(true).MaxWidth(m.width)
	if index == m.selected {
		style = selectedStyle.Inline(true).MaxWidth(m.width)
	}
	return style.Width(m.width).Render(name)
}

func (m *Model) renderLine(line string) string {
	if m.width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Width(m.width).Render(line)
}

func (m *Model) readyStatus() string {
	if m.warning != "" {
		return "Warning: " + m.warning
	}
	return readyStatus
}

func layoutHeights(height int) (header, tree, status int) {
	height = nonNegative(height)
	if height == 0 {
		return 0, 0, 0
	}
	header = 1
	if height == 1 {
		return header, 0, 0
	}
	status = 1
	tree = height - header - status
	return header, tree, status
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
