package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/uax29/v2/graphemes"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

// toastDisplayDuration is a variable so tests can shorten the display time
// without waiting for the real timer.
var toastDisplayDuration = 3 * time.Second

var (
	titleStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).Align(lipgloss.Center)
	selectedStyleDark   = lipgloss.NewStyle().Background(lipgloss.Color(selectedRowBackgroundDark))
	selectedStyleLight  = lipgloss.NewStyle().Background(lipgloss.Color(selectedRowBackgroundLight))
	toastStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	dividerStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollbarTrackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollbarThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	helpKeyStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	helpLabelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func selectedStyle(lightBackground bool) lipgloss.Style {
	if lightBackground {
		return selectedStyleLight
	}
	return selectedStyleDark
}

const (
	selectedRowBackgroundDark  = "238"
	selectedRowBackgroundLight = "254"
	loadingStatus              = "Loading directory..."
	readyStatus                = "Ready"
	helpCopyKey                = "space"
	helpCopyLabel              = "copy"
	helpReloadKey              = "r"
	helpReloadLabel            = "reload"
	helpQuitKey                = "q"
	helpQuitLabel              = "quit"
	helpGroupSeparator         = "    "
	contentLeftPadding         = 1
	ellipsis                   = "…"
	dividerGlyph               = "─"
	scrollbarTrackGlyph        = "│"
	scrollbarThumbGlyph        = "┃"
	mouseWheelScrollLines      = 3
	stickyRootHeight           = 1
	reloadToastText            = "Reloaded"
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

	// The zero value keeps the existing dark palette until detection succeeds.
	lightBackground bool

	loading bool
	status  string

	draggingScrollbar   bool
	dragScrollbarOffset int

	gitStatusRequested bool
	restorePath        string

	toast         string
	toastSeq      int
	reloadCount   int
	reloadErrored bool

	previewConfig PreviewConfig
	previewPaneID string
	previewSeq    int
}

// NewModel constructs the tree without reading the filesystem. A filesystem
// argument is optional so the composition root can use the local filesystem
// while tests can provide a deterministic implementation.
func NewModel(root, warning string, fileSystems ...filesystem.FileSystem) *Model {
	return NewModelWithPreview(root, warning, PreviewConfig{}, fileSystems...)
}

// NewModelWithPreview additionally wires the Enter-preview capability. The
// zero PreviewConfig keeps Enter a no-op, matching the plain NewModel.
func NewModelWithPreview(root, warning string, preview PreviewConfig, fileSystems ...filesystem.FileSystem) *Model {
	fileSystem := filesystem.FileSystem(filesystem.NewLocal())
	if len(fileSystems) > 0 && fileSystems[0] != nil {
		fileSystem = fileSystems[0]
	}

	m := &Model{
		warning:       sanitizeDisplay(warning),
		pending:       make(map[string]struct{}),
		previewConfig: preview,
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

// Init requests the terminal background, expands the root, and returns the
// first directory read as a command.
func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{tea.RequestBackgroundColor}
	if m == nil || m.tree == nil {
		return tea.Batch(commands...)
	}

	request, ok := m.tree.Expand(m.tree.Root())
	m.refreshVisibleRows()
	if !ok {
		return tea.Batch(commands...)
	}
	m.pending[request.Path] = struct{}{}
	m.loading = true
	m.status = loadingStatus
	if !m.gitStatusRequested && request.Node == m.tree.Root() {
		m.gitStatusRequested = true
		commands = append(commands, loadInitialDirectory(m.tree, request))
		return tea.Batch(commands...)
	}
	commands = append(commands, loadDirectory(m.tree, request))
	return tea.Batch(commands...)
}

// Update applies messages and is the only place where load results mutate the
// browser tree. Key navigation only uses the cached visible rows.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.lightBackground = !msg.IsDark()
	case browser.LoadResult:
		return m, m.applyLoadResult(msg)
	case toastTimeoutMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "space", "\u3000":
			return m, m.copySelection()
		case "r":
			return m, m.reloadTree(true)
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
			return m, m.openPreviewOnEnter()
		}
	case previewResultMsg:
		if msg.seq != m.previewSeq {
			break
		}
		m.previewPaneID = msg.paneID
		if msg.err != "" {
			m.warning = addWarning(m.warning, "Preview: "+msg.err)
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
	case tea.FocusMsg:
		// Refreshing on focus regain keeps status letters and ignore colors
		// current for editors that report terminal focus; terminals without
		// focus reporting fall back to the r key.
		return m, m.reloadTree(false)
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
		view.ReportFocus = true
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
		lines = append(lines, m.renderFooter())
	}

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	// Without focus reporting (DECSET 1004) the terminal never emits focus
	// events, and the focus-return refresh in Update would be dead code.
	view.ReportFocus = true
	return view
}

func (m *Model) copySelection() tea.Cmd {
	node := m.selectedNode()
	if node == nil {
		return nil
	}

	path := node.Path()
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

// reloadTree re-scans every loaded directory and refreshes the Git snapshot.
// Cached rows stay visible until the results land, then the selection is
// re-anchored to its pre-reload path, falling back to the last visible row
// when that path no longer exists. showToast controls the completion toast so
// focus-return refreshes stay quiet.
func (m *Model) reloadTree(showToast bool) tea.Cmd {
	if m == nil || m.tree == nil {
		return nil
	}

	requests := m.tree.Reload()
	if len(requests) == 0 {
		return nil
	}

	if node := m.selectedNode(); node != nil {
		m.restorePath = node.Path()
	}
	m.reloadErrored = false
	if showToast {
		m.reloadCount = len(requests)
	} else {
		m.reloadCount = 0
	}
	commands := make([]tea.Cmd, 0, len(requests))
	for _, request := range requests {
		m.pending[request.Path] = struct{}{}
		commands = append(commands, loadReloadDirectory(m.tree, request))
	}
	m.loading = true
	m.status = loadingStatus
	return tea.Batch(commands...)
}

func loadDirectory(tree *browser.Tree, request browser.LoadRequest) tea.Cmd {
	return func() tea.Msg {
		return tree.Read(request)
	}
}

func loadReloadDirectory(tree *browser.Tree, request browser.LoadRequest) tea.Cmd {
	return func() tea.Msg {
		return tree.ReadReload(request)
	}
}

func loadInitialDirectory(tree *browser.Tree, request browser.LoadRequest) tea.Cmd {
	return func() tea.Msg {
		return tree.ReadInitial(request)
	}
}

func (m *Model) applyLoadResult(result browser.LoadResult) tea.Cmd {
	applied := false
	if m.tree != nil {
		applied = m.tree.ApplyLoad(result)
	}
	delete(m.pending, result.Path)
	m.loading = len(m.pending) > 0
	if applied {
		if result.Err != nil {
			m.reloadErrored = true
			m.status = "Error: " + sanitizeDisplay(fmt.Sprintf("%s: %v", result.Path, result.Err))
		} else if m.loading {
			m.status = loadingStatus
		} else if !m.reloadErrored {
			m.status = m.readyStatus()
		}
		m.refreshVisibleRows()
	}
	if m.loading {
		return nil
	}

	m.restoreSelectionAfterReload()
	succeeded := !m.reloadErrored
	m.reloadErrored = false
	return m.reloadToastCommand(succeeded)
}

// reloadToastCommand shows the footer toast only when every directory of the
// reload batch succeeded. Errors are left to the footer status line, where
// they stay visible; a toast would only flash them away.
func (m *Model) reloadToastCommand(succeeded bool) tea.Cmd {
	if m.reloadCount == 0 || !succeeded {
		m.reloadCount = 0
		return nil
	}
	m.reloadCount = 0
	return m.showToast(reloadToastText)
}

// toastTimeoutMsg hides the footer toast after its display time.
type toastTimeoutMsg struct {
	seq int
}

// showToast shows text in the footer for a fixed display time. Each show
// invalidates the previous timer, so rapid reloads keep the toast visible
// for their own full duration.
func (m *Model) showToast(text string) tea.Cmd {
	m.toast = text
	m.toastSeq++
	seq := m.toastSeq
	return tea.Tick(toastDisplayDuration, func(time.Time) tea.Msg {
		return toastTimeoutMsg{seq: seq}
	})
}

// restoreSelectionAfterReload re-anchors the selection once a reload has
// finished. The pre-reload path is restored when it still exists; otherwise
// the selection settles on the last visible row.
func (m *Model) restoreSelectionAfterReload() {
	if m.restorePath == "" {
		return
	}
	path := m.restorePath
	m.restorePath = ""
	for index, row := range m.visibleRows {
		if row.Node != nil && row.Node.Path() == path {
			m.selected = index
			m.keepSelectionVisible()
			return
		}
	}
	if len(m.visibleRows) > 0 {
		m.selected = len(m.visibleRows) - 1
		m.keepSelectionVisible()
	}
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
	leftPadding := strings.Repeat(" ", m.contentLeftPadding())
	lines := make([]string, treeHeight)
	blankScrollbarCell := " "
	if m.width <= 0 {
		blankScrollbarCell = ""
	}
	if len(m.visibleRows) > 0 {
		lines[0] = leftPadding + m.renderRowWidth(0, m.visibleRows[0], contentWidth) + blankScrollbarCell
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
		} else if m.letterColumnReserved() {
			// Empty padding rows keep the reserved gap+letter cells so the
			// scrollbar stays in the same column.
			line += "  "
		}
		lines[rowIndex] = leftPadding + line + m.renderScrollbarCell(rowIndex-stickyRootHeight, metrics)
	}
	return lines
}

func (m *Model) renderRowWidth(index int, row browser.VisibleRow, width int) string {
	if width <= 0 || row.Node == nil {
		return ""
	}

	icons := iconsFor(defaultTreeIconSet)
	indent := strings.Repeat("  ", max(0, row.Depth-1))
	isRoot := row.Node.Parent() == nil
	name := row.Node.Name()
	if isRoot {
		name = row.Node.Path()
	}
	name = sanitizeDisplay(name)
	status := browser.GitStatusNone
	ignored := false
	if m.tree != nil {
		status = m.tree.GitStatusForPath(row.Path)
		// A status wins over ignore coloring; in practice they do not collide
		// because git check-ignore never reports tracked files.
		ignored = status == browser.GitStatusNone && m.tree.IsIgnored(row.Path)
	}
	var prefix string
	var icon string
	if isRoot {
		// The root row is the fixed current-path display: it cannot be collapsed
		// through UI toggles, so a chevron would falsely imply expandability.
		icon = rootTreeIcon
		name = " " + truncateRootPath(name, max(0, width-lipgloss.Width(icon)-1))
	} else {
		icon = iconForNode(row.Node, icons)
		if row.Node.IsDirectory() {
			chevron := collapsedTreeIcon
			if row.Node.Expanded() {
				chevron = expandedTreeIcon
			}
			prefix = indent + chevron + " "
		} else {
			prefix = indent + "  "
		}
		name = " " + name
	}
	return m.renderTreeRow(index, prefix, icon, name, status, ignored, width)
}

// renderTreeRow paints a tree line as separate spans so the icon keeps its
// palette color while the name follows the Git status and selection styles.
// Spans are styled individually because an inner reset inside a single styled
// span would drop the surrounding colors for the rest of the line. An ignored
// row greys out the icon and name; the trailing letter column is rendered
// whenever the repository reserves it, even for statusless rows.
func (m *Model) renderTreeRow(index int, prefix, icon, name string, status browser.GitStatus, ignored bool, width int) string {
	selectionStyle := selectedStyle(m.lightBackground)
	rowStyle := lipgloss.NewStyle().Inline(true)
	if index == m.selected {
		rowStyle = selectionStyle.Inline(true)
	}
	nameStyle := lipgloss.NewStyle().Inline(true)
	iconSpan := iconStyle(icon)
	if ignored {
		nameStyle = ignoredRowStyle
		iconSpan = ignoredRowStyle
	} else if status != browser.GitStatusNone {
		nameStyle = gitStatusStyle(status)
	}
	if index == m.selected {
		selectionBackground := selectionStyle.GetBackground()
		iconSpan = iconSpan.Background(selectionBackground)
		nameStyle = nameStyle.Background(selectionBackground)
	}

	prefix = truncateToWidth(prefix, width)
	prefixWidth := lipgloss.Width(prefix)
	budget := width - prefixWidth
	if lipgloss.Width(icon) > budget {
		// The prefix alone fills the row, so the icon and name cannot fit.
		icon = ""
		name = ""
	}
	iconWidth := lipgloss.Width(icon)
	name = truncateToWidth(name, max(0, budget-iconWidth))
	nameWidth := lipgloss.Width(name)

	rendered := rowStyle.Render(prefix) + iconSpan.Render(icon) + nameStyle.Render(name)
	if padding := width - prefixWidth - iconWidth - nameWidth; padding > 0 {
		rendered += rowStyle.Render(strings.Repeat(" ", padding))
	}

	letterSpan := gitStatusStyle(status)
	if letter := gitStatusLetter(status); letter != "" || m.letterColumnReserved() {
		if letter == "" {
			letter = " "
			letterSpan = lipgloss.NewStyle().Inline(true)
		}
		if index == m.selected {
			letterSpan = letterSpan.Background(selectionStyle.GetBackground())
		}
		rendered += rowStyle.Render(" ") + letterSpan.Render(letter)
	}
	return rendered
}

// letterColumnReserved reports whether the gap+letter column is kept for
// every row, keeping the name truncate width stable across the tree.
func (m *Model) letterColumnReserved() bool {
	return m.tree != nil && m.tree.GitReady()
}

func (m *Model) renderScrollbarCell(row int, metrics scrollbarMetrics) string {
	return renderScrollbarCellWidth(m.width, row, metrics)
}

func renderScrollbarCellWidth(width, row int, metrics scrollbarMetrics) string {
	if width <= 0 {
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
	width := m.width - 1 - m.contentLeftPadding()
	if m.letterColumnReserved() {
		// Gap + letter stay inside the scrollbar cell.
		width -= 2
	}
	if width <= 0 {
		return 0
	}
	return width
}

func (m *Model) renderLine(line string) string {
	line = strings.Repeat(" ", m.contentLeftPadding()) + sanitizeDisplay(line)
	return m.renderStyledLine(line, lipgloss.NewStyle())
}

func (m *Model) contentLeftPadding() int {
	if m.width <= 1 {
		return 0
	}
	return contentLeftPadding
}

func (m *Model) renderFooter() string {
	if m.toast != "" {
		return m.renderStyledLine(strings.Repeat(" ", m.contentLeftPadding())+m.toast, toastStyle)
	}
	if m.status != readyStatus {
		return m.renderLine(m.status)
	}

	help := renderShortcut(helpCopyKey, helpCopyLabel) + helpGroupSeparator +
		renderShortcut(helpReloadKey, helpReloadLabel) + helpGroupSeparator +
		renderShortcut(helpQuitKey, helpQuitLabel)
	help = strings.Repeat(" ", m.contentLeftPadding()) + help
	return m.renderStyledLine(help, lipgloss.NewStyle())
}

func renderShortcut(key, label string) string {
	return helpKeyStyle.Inline(true).Render(key) + " " + helpLabelStyle.Inline(true).Render(label)
}

func (m *Model) renderStyledLine(line string, style lipgloss.Style) string {
	return renderStyledLineAt(line, style, m.width)
}

func renderStyledLineAt(line string, style lipgloss.Style, width int) string {
	if width <= 0 {
		return ""
	}
	return style.Inline(true).Width(width).Render(truncateToWidth(line, width))
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
	if nonNegative(height) >= 5 {
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
