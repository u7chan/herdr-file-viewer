package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/uax29/v2/graphemes"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

var (
	titleStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).Align(lipgloss.Center)
	selectedStyle       = lipgloss.NewStyle().Background(lipgloss.Color("238"))
	dividerStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollbarTrackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollbarThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
)

const (
	loadingStatus         = "Loading directory..."
	readyStatus           = "Ready"
	ellipsis              = "…"
	dividerGlyph          = "─"
	scrollbarTrackGlyph   = "│"
	scrollbarThumbGlyph   = "┃"
	mouseWheelScrollLines = 3
	stickyRootHeight      = 1
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

	draggingScrollbar   bool
	dragScrollbarOffset int
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
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "ctrl+u":
			m.moveSelection(-m.halfPageSize())
		case "ctrl+d":
			m.moveSelection(m.halfPageSize())
		case "ctrl+b", "pgup":
			m.moveSelection(-m.pageSize())
		case "ctrl+f", "pgdown":
			m.moveSelection(m.pageSize())
		case "home":
			m.selectBoundary(false)
		case "end":
			m.selectBoundary(true)
		case "right":
			return m, m.expandSelection()
		case "left":
			m.collapseOrMoveToParent()
		case "enter":
			// Enter intentionally has no action in this read-only tree.
		}
	case tea.MouseClickMsg:
		return m, m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		m.handleMouseRelease()
	case tea.MouseWheelMsg:
		m.handleMouseWheel(msg)
	case tea.InterruptMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width = nonNegative(msg.Width)
		m.height = nonNegative(msg.Height)
		m.draggingScrollbar = false
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

	headerHeight, treeHeight, footerHeight := layoutHeights(m.height)
	topDividerHeight := headerDividerHeight(m.height)
	bottomDividerHeight := footerDividerHeight(m.height)
	lines := make([]string, 0, headerHeight+topDividerHeight+treeHeight+bottomDividerHeight+footerHeight)
	if headerHeight > 0 {
		lines = append(lines, m.renderStyledLine("Herdr File Viewer", titleStyle))
	}
	if topDividerHeight > 0 {
		lines = append(lines, m.renderDivider())
	}
	if treeHeight > 0 {
		lines = append(lines, m.renderTree(treeHeight)...)
	}
	if bottomDividerHeight > 0 {
		lines = append(lines, m.renderDivider())
	}
	if footerHeight > 0 {
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
	if m.isScrollbarCell(msg.X, msg.Y) {
		m.beginScrollbarDrag(msg.Y)
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

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) {
	if !m.isTreeY(msg.Y) {
		return
	}

	switch msg.Button {
	case tea.MouseWheelUp:
		m.scrollBy(-mouseWheelScrollLines)
	case tea.MouseWheelDown:
		m.scrollBy(mouseWheelScrollLines)
	}
}

func (m *Model) handleMouseMotion(msg tea.MouseMotionMsg) {
	if !m.draggingScrollbar {
		return
	}

	scrollHeight := m.scrollableViewportHeight()
	metrics := newScrollbarMetrics(scrollHeight, m.scrollableRowCount(), m.offset)
	if metrics.maxThumbStart() == 0 {
		m.draggingScrollbar = false
		return
	}

	startY := m.scrollableStartY()
	localY := msg.Y - startY - m.dragScrollbarOffset
	if localY < 0 {
		localY = 0
	}
	if max := metrics.maxThumbStart(); localY > max {
		localY = max
	}
	m.offset = metrics.offsetForThumbStart(localY)
	m.clampSelectionToViewport()
}

func (m *Model) handleMouseRelease() {
	m.draggingScrollbar = false
}

func (m *Model) beginScrollbarDrag(y int) {
	scrollHeight := m.scrollableViewportHeight()
	metrics := newScrollbarMetrics(scrollHeight, m.scrollableRowCount(), m.offset)
	if metrics.maxThumbStart() == 0 {
		return
	}

	localY := y - m.scrollableStartY()
	if localY < 0 || localY >= scrollHeight {
		return
	}

	grabOffset := metrics.thumbSize / 2
	if localY >= metrics.thumbStart && localY < metrics.thumbStart+metrics.thumbSize {
		grabOffset = localY - metrics.thumbStart
	}
	thumbStart := localY - grabOffset
	if thumbStart < 0 {
		thumbStart = 0
	}
	if max := metrics.maxThumbStart(); thumbStart > max {
		thumbStart = max
	}
	m.offset = metrics.offsetForThumbStart(thumbStart)
	m.clampSelectionToViewport()
	m.draggingScrollbar = true
	m.dragScrollbarOffset = grabOffset
}

func (m *Model) isScrollbarCell(x, y int) bool {
	return m.width > 0 && x == m.width-1 && m.isScrollableTreeY(y)
}

func (m *Model) isTreeY(y int) bool {
	_, treeHeight, _ := layoutHeights(m.height)
	startY := m.treeStartY()
	return treeHeight > 0 && y >= startY && y < startY+treeHeight
}

func (m *Model) treeStartY() int {
	headerHeight, _, _ := layoutHeights(m.height)
	return headerHeight + headerDividerHeight(m.height)
}

func (m *Model) scrollableStartY() int {
	return m.treeStartY() + stickyRootHeight
}

func (m *Model) isScrollableTreeY(y int) bool {
	scrollHeight := m.scrollableViewportHeight()
	startY := m.scrollableStartY()
	return scrollHeight > 0 && y >= startY && y < startY+scrollHeight
}

func (m *Model) scrollableViewportHeight() int {
	_, treeHeight, _ := layoutHeights(m.height)
	if treeHeight <= stickyRootHeight || len(m.visibleRows) == 0 {
		return 0
	}
	return treeHeight - stickyRootHeight
}

func (m *Model) scrollableRowCount() int {
	if len(m.visibleRows) <= stickyRootHeight {
		return 0
	}
	return len(m.visibleRows) - stickyRootHeight
}

func (m *Model) rowIndexAtY(y int) (int, bool) {
	if y < 0 {
		return 0, false
	}

	_, treeHeight, _ := layoutHeights(m.height)
	startY := m.treeStartY()
	if treeHeight <= 0 || y < startY || y >= startY+treeHeight {
		return 0, false
	}

	localY := y - startY
	if localY == 0 && len(m.visibleRows) > 0 {
		return 0, true
	}
	if localY < stickyRootHeight || localY >= stickyRootHeight+m.scrollableViewportHeight() {
		return 0, false
	}

	index := stickyRootHeight + m.offset + localY - stickyRootHeight
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
	if node.IsDirectory() && node.Expanded() && node.Parent() != nil {
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

func (m *Model) selectBoundary(last bool) {
	if len(m.visibleRows) == 0 {
		return
	}
	if last {
		m.selected = len(m.visibleRows) - 1
	} else {
		m.selected = 0
	}
	m.keepSelectionVisible()
}

func (m *Model) halfPageSize() int {
	scrollHeight := m.scrollableViewportHeight()
	if scrollHeight < 2 {
		return 1
	}
	return scrollHeight / 2
}

func (m *Model) pageSize() int {
	scrollHeight := m.scrollableViewportHeight()
	if scrollHeight < 2 {
		return 1
	}
	return scrollHeight - 1
}

func (m *Model) scrollBy(delta int) {
	if len(m.visibleRows) == 0 || delta == 0 {
		return
	}

	scrollHeight := m.scrollableViewportHeight()
	if scrollHeight <= 0 {
		return
	}
	m.clampSelectionToViewport()
	previous := m.offset
	m.offset = m.clampOffset(m.offset+delta, scrollHeight)
	actualDelta := m.offset - previous
	if actualDelta != 0 && m.selected != 0 {
		m.selected += actualDelta
		m.clampSelectionToViewport()
	}
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

	scrollHeight := m.scrollableViewportHeight()
	if scrollHeight <= 0 {
		m.offset = 0
		return
	}
	m.offset = m.clampOffset(m.offset, scrollHeight)
	if m.selected == 0 {
		return
	}
	selectedOffset := m.selected - stickyRootHeight
	if selectedOffset < m.offset {
		m.offset = selectedOffset
	}
	if selectedOffset >= m.offset+scrollHeight {
		m.offset = selectedOffset - scrollHeight + 1
	}
	m.offset = m.clampOffset(m.offset, scrollHeight)
}

func (m *Model) clampSelectionToViewport() {
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
	scrollHeight := m.scrollableViewportHeight()
	if scrollHeight <= 0 {
		return
	}
	m.offset = m.clampOffset(m.offset, scrollHeight)
	if m.selected == 0 {
		return
	}
	selectedOffset := m.selected - stickyRootHeight
	if selectedOffset < m.offset {
		m.selected = m.offset + stickyRootHeight
	}
	if selectedOffset >= m.offset+scrollHeight {
		m.selected = m.offset + scrollHeight - 1 + stickyRootHeight
	}
	if m.selected >= len(m.visibleRows) {
		m.selected = len(m.visibleRows) - 1
	}
}

func (m *Model) clampOffset(offset, scrollHeight int) int {
	rowCount := m.scrollableRowCount()
	if scrollHeight <= 0 || rowCount <= scrollHeight {
		return 0
	}
	maxOffset := rowCount - scrollHeight
	if offset < 0 {
		return 0
	}
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

func (m *Model) renderTree(treeHeight int) []string {
	if treeHeight <= 0 {
		return nil
	}

	contentWidth := m.treeContentWidth()
	lines := make([]string, treeHeight)
	blankScrollbarCell := " "
	if m.width <= 0 {
		blankScrollbarCell = ""
	}
	if len(m.visibleRows) > 0 {
		lines[0] = m.renderRowWidth(0, m.visibleRows[0], contentWidth) + blankScrollbarCell
	}

	scrollHeight := treeHeight - stickyRootHeight
	if scrollHeight <= 0 {
		return lines
	}
	metrics := newScrollbarMetrics(scrollHeight, m.scrollableRowCount(), m.offset)
	for rowIndex := stickyRootHeight; rowIndex < len(lines); rowIndex++ {
		index := stickyRootHeight + metrics.offset + rowIndex - stickyRootHeight
		line := strings.Repeat(" ", contentWidth)
		if index >= stickyRootHeight && index < len(m.visibleRows) {
			line = m.renderRowWidth(index, m.visibleRows[index], contentWidth)
		}
		lines[rowIndex] = line + m.renderScrollbarCell(rowIndex-stickyRootHeight, metrics)
	}
	return lines
}

func (m *Model) renderRowWidth(index int, row browser.VisibleRow, width int) string {
	if width <= 0 || row.Node == nil {
		return ""
	}

	icons := iconsFor(defaultTreeIconSet)
	indent := strings.Repeat("  ", row.Depth)
	isRoot := row.Node.Parent() == nil
	name := row.Node.Name()
	if isRoot {
		name = row.Node.Path()
	}
	name = sanitizeDisplay(name)
	if isRoot {
		rootPrefix := rootTreeIcon + " "
		name = rootPrefix + truncateRootPath(name, width-lipgloss.Width(rootPrefix))
	} else {
		icon := iconForNode(row.Node, icons)
		if row.Node.IsDirectory() {
			chevron := collapsedTreeIcon
			if row.Node.Expanded() {
				chevron = expandedTreeIcon
			}
			name = indent + chevron + " " + icon + " " + name
		} else {
			name = indent + "  " + icon + " " + name
		}
	}
	// The root row is the fixed current-path display: it cannot be collapsed
	// through UI toggles, so a chevron would falsely imply expandability.

	style := lipgloss.NewStyle().Inline(true)
	if index == m.selected {
		style = selectedStyle.Inline(true)
	}
	return style.Width(width).Render(truncateToWidth(name, width))
}

func (m *Model) renderScrollbarCell(row int, metrics scrollbarMetrics) string {
	if m.width <= 0 {
		return ""
	}

	glyph := scrollbarTrackGlyph
	style := scrollbarTrackStyle
	if metrics.isThumbRow(row) {
		glyph = scrollbarThumbGlyph
		style = scrollbarThumbStyle
	}
	return style.Inline(true).Render(glyph)
}

func (m *Model) renderDivider() string {
	return m.renderStyledLine(strings.Repeat(dividerGlyph, m.width), dividerStyle)
}

func (m *Model) treeContentWidth() int {
	if m.width <= 1 {
		return 0
	}
	return m.width - 1
}

func (m *Model) renderLine(line string) string {
	return m.renderStyledLine(sanitizeDisplay(line), lipgloss.NewStyle())
}

func (m *Model) renderStyledLine(line string, style lipgloss.Style) string {
	if m.width <= 0 {
		return ""
	}
	return style.Inline(true).Width(m.width).Render(truncateToWidth(line, m.width))
}

func truncateToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, ellipsis)
}

func truncateRootPath(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}

	pathPrefix := ellipsis + string(filepath.Separator)
	if width <= lipgloss.Width(pathPrefix) {
		return truncateToWidth(pathPrefix, width)
	}

	suffix := truncateTailToWidth(value, width-lipgloss.Width(pathPrefix))
	suffix = strings.TrimPrefix(suffix, string(filepath.Separator))
	return pathPrefix + suffix
}

func truncateTailToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	valueWidth := lipgloss.Width(value)
	if valueWidth <= width {
		return value
	}

	// TruncateLeft keeps the grapheme that straddles the cut boundary whole,
	// so the tail can exceed the budget by one wide cluster. Drop leading
	// graphemes until the tail fits the budget.
	tail := ansi.TruncateLeft(value, valueWidth-width, "")
	for lipgloss.Width(tail) > width {
		tail = dropFirstGrapheme(tail)
	}
	return tail
}

func dropFirstGrapheme(value string) string {
	iter := graphemes.FromString(value)
	if iter.Next() {
		return value[len(iter.Value()):]
	}
	return value
}

func (m *Model) readyStatus() string {
	if m.warning != "" {
		return "Warning: " + m.warning
	}
	return readyStatus
}

func layoutHeights(height int) (header, tree, footer int) {
	height = nonNegative(height)
	if height == 0 {
		return 0, 0, 0
	}
	header = 1
	if height == 1 {
		return header, 0, 0
	}
	footer = 1
	tree = height - header - headerDividerHeight(height) - footerDividerHeight(height) - footer
	return header, tree, footer
}

func headerDividerHeight(height int) int {
	if nonNegative(height) >= 4 {
		return 1
	}
	return 0
}

func footerDividerHeight(height int) int {
	if nonNegative(height) >= 3 {
		return 1
	}
	return 0
}

type scrollbarMetrics struct {
	viewport   int
	total      int
	offset     int
	thumbStart int
	thumbSize  int
}

func newScrollbarMetrics(viewport, total, offset int) scrollbarMetrics {
	metrics := scrollbarMetrics{
		viewport: viewport,
		total:    total,
		offset:   offset,
	}
	if viewport <= 0 || total <= 0 {
		return metrics
	}
	if metrics.offset < 0 {
		metrics.offset = 0
	}
	maxOffset := metrics.maxOffset()
	if metrics.offset > maxOffset {
		metrics.offset = maxOffset
	}
	if total <= viewport {
		metrics.thumbSize = viewport
		return metrics
	}

	thumbSize := int64(viewport) * int64(viewport) / int64(total)
	if thumbSize < 1 {
		thumbSize = 1
	}
	if viewport > 1 && thumbSize >= int64(viewport) {
		// Keep at least one track cell for every overflowing viewport.
		thumbSize = int64(viewport - 1)
	}
	metrics.thumbSize = int(thumbSize)
	maxThumbStart := metrics.maxThumbStart()
	if maxThumbStart > 0 && maxOffset > 0 {
		metrics.thumbStart = int(int64(metrics.offset) * int64(maxThumbStart) / int64(maxOffset))
	}
	return metrics
}

func (s scrollbarMetrics) maxOffset() int {
	if s.total <= s.viewport {
		return 0
	}
	return s.total - s.viewport
}

func (s scrollbarMetrics) maxThumbStart() int {
	if s.viewport <= 0 || s.thumbSize >= s.viewport {
		return 0
	}
	return s.viewport - s.thumbSize
}

func (s scrollbarMetrics) isThumbRow(row int) bool {
	return row >= s.thumbStart && row < s.thumbStart+s.thumbSize
}

func (s scrollbarMetrics) offsetForThumbStart(thumbStart int) int {
	maxThumbStart := s.maxThumbStart()
	if maxThumbStart <= 0 {
		return 0
	}
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart > maxThumbStart {
		thumbStart = maxThumbStart
	}
	return int(int64(thumbStart) * int64(s.maxOffset()) / int64(maxThumbStart))
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
