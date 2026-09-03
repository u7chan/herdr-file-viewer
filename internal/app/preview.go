package app

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/uax29/v2/graphemes"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

const (
	// previewMaxBytes caps the previewed text at 2 MiB; larger files show a
	// truncated marker after their head.
	previewMaxBytes = 2 << 20
	// previewSniffBytes is the head length used to classify extensionless
	// content before the text pipeline is allowed to run.
	previewSniffBytes = 8 * 1024
	// previewTabWidth is the fixed cell width used for tab expansion.
	previewTabWidth = 4

	previewLoadingStatus             = "Loading preview..."
	previewUntitled                  = "Preview"
	previewHelpCopyKey               = "space"
	previewHelpCopyLabel             = "copy"
	previewHelpHelpKey               = "h"
	previewHelpHelpLabel             = "help"
	previewHelpCloseKey              = "q"
	previewHelpCloseLabel            = "close"
	previewNoSelectionStatus         = "No selection"
	previewUnsupportedPrefix         = "Unsupported preview: "
	previewGutterDividerGlyph        = "│"
	previewHorizontalTrackGlyph      = "─"
	previewHorizontalThumbGlyph      = "━"
	previewSelectionBackground       = "240"
	previewSelectionBackgroundLight  = "252"
	previewWhitespaceGlyph           = "⋅"
	previewWhitespaceForeground      = 241
	previewWhitespaceForegroundLight = 250
)

var previewTruncatedMarker = fmt.Sprintf("… truncated (%d MiB limit)", previewMaxBytes>>20)

// previewToastDuration is a variable so tests can shorten the display time
// without waiting for the real timer.
var previewToastDuration = 3 * time.Second

var (
	previewSelectionStyleDark   = lipgloss.NewStyle().Background(lipgloss.Color(previewSelectionBackground))
	previewSelectionStyleLight  = lipgloss.NewStyle().Background(lipgloss.Color(previewSelectionBackgroundLight))
	previewWhitespaceStyleDark  = lipgloss.NewStyle().Inline(true).Foreground(lipgloss.ANSIColor(previewWhitespaceForeground))
	previewWhitespaceStyleLight = lipgloss.NewStyle().Inline(true).Foreground(lipgloss.ANSIColor(previewWhitespaceForegroundLight))
)

func previewSelectionStyle(lightBackground bool) lipgloss.Style {
	if lightBackground {
		return previewSelectionStyleLight
	}
	return previewSelectionStyleDark
}

func previewWhitespaceStyle(lightBackground bool) lipgloss.Style {
	if lightBackground {
		return previewWhitespaceStyleLight
	}
	return previewWhitespaceStyleDark
}

// previewCategory classifies a preview target for the unsupported label.
type previewCategory string

const (
	previewCategoryText   previewCategory = "text"
	previewCategoryImage  previewCategory = "image"
	previewCategoryVideo  previewCategory = "video"
	previewCategoryAudio  previewCategory = "audio"
	previewCategoryBinary previewCategory = "binary"
)

// previewCategoryByExtension labels known media and binary extensions before
// any content sniffing. Anything not listed is decided by sniffing; known
// text-ish extensions (svg, json, markdown, ...) simply are not listed.
var previewCategoryByExtension = map[string]previewCategory{
	// Images
	".png": previewCategoryImage, ".jpg": previewCategoryImage, ".jpeg": previewCategoryImage,
	".gif": previewCategoryImage, ".webp": previewCategoryImage, ".bmp": previewCategoryImage,
	".ico": previewCategoryImage, ".tif": previewCategoryImage, ".tiff": previewCategoryImage,
	// Video
	".mp4": previewCategoryVideo, ".mkv": previewCategoryVideo, ".avi": previewCategoryVideo,
	".mov": previewCategoryVideo, ".webm": previewCategoryVideo, ".m4v": previewCategoryVideo,
	".mpg": previewCategoryVideo, ".mpeg": previewCategoryVideo, ".wmv": previewCategoryVideo,
	// Audio
	".mp3": previewCategoryAudio, ".wav": previewCategoryAudio, ".flac": previewCategoryAudio,
	".ogg": previewCategoryAudio, ".m4a": previewCategoryAudio, ".aac": previewCategoryAudio,
	".opus": previewCategoryAudio, ".wma": previewCategoryAudio,
	// Binary containers and executables
	".zip": previewCategoryBinary, ".gz": previewCategoryBinary, ".tgz": previewCategoryBinary,
	".tar": previewCategoryBinary, ".xz": previewCategoryBinary, ".bz2": previewCategoryBinary,
	".7z": previewCategoryBinary, ".rar": previewCategoryBinary, ".jar": previewCategoryBinary,
	".exe": previewCategoryBinary, ".so": previewCategoryBinary, ".dll": previewCategoryBinary,
	".pdf": previewCategoryBinary, ".class": previewCategoryBinary, ".pyc": previewCategoryBinary,
	".o": previewCategoryBinary, ".a": previewCategoryBinary, ".wasm": previewCategoryBinary,
	".iso": previewCategoryBinary, ".img": previewCategoryBinary, ".bin": previewCategoryBinary,
}

// previewLine is one rendered line. number is the 1-based original line
// number; wrapped continuation lines and appended markers carry 0 so their
// gutter stays blank. origin and col map display lines back to m.lines, while
// spans keep syntax ranges in original-line cell coordinates.
type previewLine struct {
	text   string
	number int
	muted  bool
	origin int
	col    int
	spans  []previewSyntaxSpan
}

// previewLoadMsg is the result of the preview file read command.
type previewLoadMsg struct {
	file      string
	category  previewCategory
	lines     []previewLine
	truncated bool
	// reload distinguishes a manual reload completion from the initial load.
	reload bool
	err    string
}

// previewTagMsg reports a finished metadata report; tagging is a best-effort
// re-discovery hint and its outcome is intentionally not surfaced.
type previewTagMsg struct{}

type previewDragMode int

const (
	previewDragNone previewDragMode = iota
	previewDragVScroll
	previewDragHScroll
	previewDragSelect
)

// PreviewModel is the read-only text preview pane. It mirrors the tree's
// layout and scrollbar behavior and renders a line-number gutter.
type PreviewModel struct {
	file   string
	paneID string

	client PreviewClient
	reader filesystem.FileReader

	loading bool
	status  string
	warning string

	category        previewCategory
	lines           []previewLine
	lineCount       int
	displayLines    []previewLine
	maxContentWidth int
	truncated       bool

	wrap           bool
	showWhitespace bool
	offset         int
	xoffset        int
	width          int
	height         int

	// The zero value keeps the existing dark palette until detection succeeds.
	lightBackground bool

	dragMode    previewDragMode
	dragVOffset int
	dragHOffset int
	selection   previewSelection

	toast    string
	toastSeq int

	helpConfig  HelpConfig
	helpPending bool
}

// NewPreviewModel constructs the preview without reading the file. The
// reader is optional for tests; the local filesystem is the default. A
// missing or empty file path puts the model into a warning state that waits
// for q.
func NewPreviewModel(file string, client PreviewClient, paneID string, readers ...filesystem.FileReader) *PreviewModel {
	return NewPreviewModelWithConfig(file, client, paneID, HelpConfig{}, readers...)
}

// NewPreviewModelWithConfig additionally wires the help overlay capability
// behind the h key.
func NewPreviewModelWithConfig(file string, client PreviewClient, paneID string, help HelpConfig, readers ...filesystem.FileReader) *PreviewModel {
	reader := filesystem.FileReader(filesystem.NewLocal())
	if len(readers) > 0 && readers[0] != nil {
		reader = readers[0]
	}

	m := &PreviewModel{
		file:       file,
		paneID:     paneID,
		client:     client,
		reader:     reader,
		helpConfig: help,
	}
	if file == "" {
		m.warning = "preview file is unset (HERDR_PREVIEW_FILE)"
		m.status = m.readyStatus()
	} else {
		m.loading = true
		m.status = previewLoadingStatus
	}
	return m
}

// Init starts the metadata tag and the file load as commands. Reads and
// classification happen outside View and Update.
func (m *PreviewModel) Init() tea.Cmd {
	commands := []tea.Cmd{tea.RequestBackgroundColor}
	if m == nil {
		return tea.Batch(commands...)
	}
	if m.client != nil && m.paneID != "" && m.file != "" {
		commands = append(commands, m.tagCommand())
	}
	if m.reader != nil && m.file != "" {
		commands = append(commands, m.loadCommand())
	}
	return tea.Batch(commands...)
}

func (m *PreviewModel) tagCommand() tea.Cmd {
	client, paneID, file := m.client, m.paneID, m.file
	return func() tea.Msg {
		// The token is a re-discovery hint for the tree; a failed report is
		// intentionally silent because the tree then opens a fresh pane.
		_ = client.TagPreview(paneID, file)
		return previewTagMsg{}
	}
}

func (m *PreviewModel) loadCommand() tea.Cmd {
	return m.loadCommandFor(false)
}

func (m *PreviewModel) reloadCommand() tea.Cmd {
	if m.file == "" || m.reader == nil {
		return nil
	}
	m.loading = true
	m.status = previewLoadingStatus
	return m.loadCommandFor(true)
}

func (m *PreviewModel) loadCommandFor(reload bool) tea.Cmd {
	reader, path := m.reader, m.file
	return func() tea.Msg {
		content, truncated, err := reader.ReadFileHead(path, previewMaxBytes)
		if err != nil {
			return previewLoadMsg{file: path, reload: reload, err: sanitizeDisplay(err.Error())}
		}
		category := previewCategoryFor(path, content)
		lines := previewTextLines(content)
		if category == previewCategoryText {
			lines = highlightPreviewLines(path, lines)
		}
		return previewLoadMsg{
			file:      path,
			category:  category,
			lines:     lines,
			truncated: truncated,
			reload:    reload,
		}
	}
}

// Update applies messages, keys, mouse events, and resizes. Reading and
// classification only ever arrive through previewLoadMsg.
func (m *PreviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.lightBackground = !msg.IsDark()
	case previewLoadMsg:
		return m, m.applyPreviewLoad(msg)
	case helpResultMsg:
		m.helpPending = false
		if msg.err != "" {
			m.warning = addWarning(m.warning, "Help: "+msg.err)
			if !m.loading {
				m.status = m.readyStatus()
			}
		}
	case previewToastTimeoutMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
	case previewTagMsg:
		// Tagging has no UI effect.
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "h":
			return m, m.requestHelp()
		case "r":
			return m, m.reloadCommand()
		case "up", "k":
			m.moveVertical(-1)
		case "down", "j":
			m.moveVertical(1)
		case "ctrl+u":
			m.moveVertical(-m.halfPageMove())
		case "ctrl+d":
			m.moveVertical(m.halfPageMove())
		case "ctrl+b", "pgup":
			m.moveVertical(-m.pageMove())
		case "ctrl+f", "pgdown":
			m.moveVertical(m.pageMove())
		case "home":
			m.offset = 0
		case "end":
			m.offset = m.maxVerticalOffset()
		case "left":
			if !m.wrap {
				m.moveHorizontal(-1)
			}
		case "right":
			if !m.wrap {
				m.moveHorizontal(1)
			}
		case "w":
			m.toggleWrap()
		case "s":
			m.showWhitespace = !m.showWhitespace
		case "space", "\u3000":
			return m, m.copySelection()
		}
	case tea.MouseClickMsg:
		m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		m.handleMouseRelease(msg)
	case tea.MouseWheelMsg:
		m.handleMouseWheel(msg)
	case tea.InterruptMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width = nonNegative(msg.Width)
		m.height = nonNegative(msg.Height)
		m.dragMode = previewDragNone
		m.clearSelection()
		m.rebuildDisplayLines()
	}
	return m, nil
}

// View renders cached state only.
func (m *PreviewModel) View() tea.View {
	if m == nil {
		view := tea.NewView("")
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		return view
	}

	headerHeight, bodyHeight, hbarHeight, footerHeight := previewLayoutHeights(m.height, m.showHorizontalScrollbar())
	lines := make([]string, 0, headerHeight+headerDividerHeight(m.height)+bodyHeight+hbarHeight+footerDividerHeight(m.height)+footerHeight)
	if headerHeight > 0 {
		lines = append(lines, m.renderTitle())
	}
	if headerDividerHeight(m.height) > 0 {
		lines = append(lines, m.renderDivider())
	}
	if bodyHeight > 0 {
		lines = append(lines, m.renderBody(bodyHeight)...)
	}
	if hbarHeight > 0 {
		lines = append(lines, m.renderHorizontalScrollbar())
	}
	if footerDividerHeight(m.height) > 0 {
		lines = append(lines, m.renderDivider())
	}
	if footerHeight > 0 {
		lines = append(lines, m.renderFooter())
	}

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m *PreviewModel) applyPreviewLoad(msg previewLoadMsg) tea.Cmd {
	m.loading = false
	if msg.err != "" {
		if m.lines == nil {
			m.displayLines = nil
			m.maxContentWidth = 0
			m.dragMode = previewDragNone
			m.clearSelection()
			m.lineCount = 0
			m.category = ""
			m.truncated = false
		}
		m.warning = addWarning(m.warning, msg.err)
		m.status = m.readyStatus()
		return nil
	}
	m.displayLines = nil
	m.maxContentWidth = 0
	m.dragMode = previewDragNone
	m.clearSelection()
	m.warning = ""
	m.file = msg.file
	m.category = msg.category
	m.truncated = msg.truncated
	m.lines = msg.lines
	m.lineCount = len(msg.lines)
	m.maxContentWidth = maxContentWidthFor(msg.lines)
	m.rebuildDisplayLines()
	m.status = m.readyStatus()
	if msg.reload {
		return m.showToast(reloadToastText)
	}
	return nil
}

// rebuildDisplayLines recomputes the wrapped view after a load, a wrap
// toggle, or a resize, and clamps both scroll offsets to the new rows.
func (m *PreviewModel) rebuildDisplayLines() {
	width := m.contentWidth()
	m.displayLines = buildDisplayLines(m.lines, m.wrap, width)
	if m.truncated && m.category == previewCategoryText {
		m.displayLines = append(m.displayLines, previewLine{text: previewTruncatedMarker, muted: true, origin: -1})
	}
	m.clampVerticalOffset()
	m.clampHorizontalOffset()
}

func (m *PreviewModel) clampVerticalOffset() {
	maxOffset := m.maxVerticalOffset()
	if m.offset < 0 || m.offset > maxOffset {
		m.offset = clampInt(m.offset, 0, maxOffset)
	}
}

func (m *PreviewModel) clampHorizontalOffset() {
	if m.wrap {
		m.xoffset = 0
		return
	}
	m.xoffset = clampInt(m.xoffset, 0, m.maxHorizontalOffset())
}

func (m *PreviewModel) maxVerticalOffset() int {
	body := m.bodyHeight()
	total := len(m.displayLines)
	if body <= 0 {
		return 0
	}
	return max(0, total-body)
}

func (m *PreviewModel) maxHorizontalOffset() int {
	return max(0, m.maxContentWidth-m.contentWidth())
}

func (m *PreviewModel) moveVertical(delta int) {
	if delta == 0 {
		return
	}
	m.offset = clampInt(m.offset+delta, 0, m.maxVerticalOffset())
}

func (m *PreviewModel) halfPageMove() int {
	body := m.bodyHeight()
	if body < 2 {
		return 1
	}
	return body / 2
}

func (m *PreviewModel) pageMove() int {
	body := m.bodyHeight()
	if body < 2 {
		return 1
	}
	return body - 1
}

func (m *PreviewModel) moveHorizontal(delta int) {
	if m.wrap || delta == 0 {
		return
	}
	m.xoffset = clampInt(m.xoffset+delta, 0, m.maxHorizontalOffset())
}

func (m *PreviewModel) toggleWrap() {
	m.wrap = !m.wrap
	m.dragMode = previewDragNone
	m.clearSelection()
	if m.wrap {
		m.xoffset = 0
	}
	m.rebuildDisplayLines()
}

// previewToastTimeoutMsg hides the preview footer toast after its display
// time. It is a separate type from the tree's toastTimeoutMsg so each model
// only consumes its own timers.
type previewToastTimeoutMsg struct {
	seq int
}

// showToast shows text in the footer for a fixed display time. Each show
// invalidates the previous timer, so rapid copies keep the toast visible
// for their own full duration.
func (m *PreviewModel) showToast(text string) tea.Cmd {
	m.toast = text
	m.toastSeq++
	seq := m.toastSeq
	return tea.Tick(previewToastDuration, func(time.Time) tea.Msg {
		return previewToastTimeoutMsg{seq: seq}
	})
}

// requestHelp opens the preview help overlay. A missing Herdr adapter or a
// failed launch keeps the preview state untouched and surfaces a footer
// warning; repeated presses while a launch is in flight are ignored so one
// overlay is never created twice.
func (m *PreviewModel) requestHelp() tea.Cmd {
	if m.helpConfig.Client == nil || m.paneID == "" {
		m.warning = addWarning(m.warning, "Help unavailable: no Herdr context")
		m.status = m.readyStatus()
		return nil
	}
	if m.helpPending {
		return nil
	}
	m.helpPending = true
	client := m.helpConfig.Client
	targetPane := m.paneID
	return func() tea.Msg {
		_, err := client.OpenHelp(HelpOpenRequest{Context: helpPreviewContext, TargetPane: targetPane})
		if err != nil {
			return helpResultMsg{err: sanitizeDisplay(err.Error())}
		}
		return helpResultMsg{}
	}
}

// copySelection extracts the selection into a clipboard command. An empty
// selection only shows a toast; the highlight is kept either way so a copy
// that did happen stays visible and can be re-issued with space.
func (m *PreviewModel) copySelection() tea.Cmd {
	text := extractSelection(m.lines, m.selection)
	if text == "" {
		return m.showToast(previewNoSelectionStatus)
	}
	return tea.Batch(
		m.showToast(previewCopyStatus(text, m.selection)),
		tea.SetClipboard(text),
	)
}

// previewCopyStatus formats the copy status. N is the rune count of the
// extracted text; multi-line selections append their line count.
func previewCopyStatus(text string, selection previewSelection) string {
	start, end, ok := selection.selectionRange()
	if !ok {
		return ""
	}
	count := utf8.RuneCountInString(text)
	if start.line == end.line {
		return fmt.Sprintf("Copied %d chars", count)
	}
	return fmt.Sprintf("Copied %d chars (%d lines)", count, end.line-start.line+1)
}

// showHorizontalScrollbar reports whether the bottom scrollbar row is
// reserved. Wrap mode never scrolls horizontally.
func (m *PreviewModel) showHorizontalScrollbar() bool {
	return !m.wrap && m.maxContentWidth > m.contentWidth()
}

// previewLayoutHeights splits the pane into header, body, horizontal
// scrollbar row, and footer. Dividers follow the tree's thresholds.
func previewLayoutHeights(height int, showHBar bool) (header, body, hbar, footer int) {
	height = nonNegative(height)
	if height == 0 {
		return 0, 0, 0, 0
	}
	header = 1
	if height == 1 {
		return header, 0, 0, 0
	}
	footer = 1
	inner := height - header - headerDividerHeight(height) - footerDividerHeight(height) - footer
	if showHBar && inner >= 2 {
		return header, inner - 1, 1, footer
	}
	return header, inner, 0, footer
}

func (m *PreviewModel) bodyStartY() int {
	return 1 + headerDividerHeight(m.height)
}

func (m *PreviewModel) bodyHeight() int {
	_, body, _, _ := previewLayoutHeights(m.height, m.showHorizontalScrollbar())
	return body
}

func (m *PreviewModel) horizontalBarY() int {
	return m.bodyStartY() + m.bodyHeight()
}

func (m *PreviewModel) isBodyY(y int) bool {
	body := m.bodyHeight()
	return body > 0 && y >= m.bodyStartY() && y < m.bodyStartY()+body
}

func (m *PreviewModel) isVerticalScrollbarCell(x, y int) bool {
	return m.width > 0 && x == m.width-1 && m.isBodyY(y)
}

func (m *PreviewModel) isHorizontalScrollbarCell(x, y int) bool {
	_, _, hbar, _ := previewLayoutHeights(m.height, m.showHorizontalScrollbar())
	return hbar > 0 && y == m.horizontalBarY() && x >= 0 && x < m.width
}

func (m *PreviewModel) handleMouseClick(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	if m.isVerticalScrollbarCell(msg.X, msg.Y) {
		m.dragMode = previewDragNone
		m.beginVerticalDrag(msg.Y)
		return
	}
	if m.isHorizontalScrollbarCell(msg.X, msg.Y) {
		m.dragMode = previewDragNone
		m.beginHorizontalDrag(msg.X)
		return
	}
	if m.category != previewCategoryText || !m.isBodyY(msg.Y) {
		return
	}
	position, ok := m.previewPositionAt(msg.X, msg.Y, false)
	if !ok {
		return
	}
	m.selection = previewSelection{anchor: position, focus: position}
	m.dragMode = previewDragSelect
}

func (m *PreviewModel) handleMouseMotion(msg tea.MouseMotionMsg) {
	switch m.dragMode {
	case previewDragVScroll:
		m.dragVerticalTo(msg.Y)
	case previewDragHScroll:
		m.dragHorizontalTo(msg.X)
	case previewDragSelect:
		if msg.Button != tea.MouseLeft {
			return
		}
		// TODO: Add automatic viewport scrolling while a selection drag is outside the body.
		if m.category != previewCategoryText {
			return
		}
		if position, ok := m.previewPositionAt(msg.X, msg.Y, true); ok {
			m.selection.focus = position
		}
	}
}

func (m *PreviewModel) handleMouseRelease(msg tea.MouseReleaseMsg) {
	if m.dragMode == previewDragSelect && msg.Button != tea.MouseLeft {
		return
	}
	if m.dragMode != previewDragNone {
		m.dragMode = previewDragNone
	}
}

func (m *PreviewModel) handleMouseWheel(msg tea.MouseWheelMsg) {
	if !m.isBodyY(msg.Y) {
		return
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		m.moveVertical(-mouseWheelScrollLines)
	case tea.MouseWheelDown:
		m.moveVertical(mouseWheelScrollLines)
	}
}

func (m *PreviewModel) beginVerticalDrag(y int) {
	metrics := newScrollbarMetrics(m.bodyHeight(), len(m.displayLines), m.offset)
	if metrics.maxThumbStart() == 0 {
		return
	}
	localY := y - m.bodyStartY()
	if localY < 0 || localY >= m.bodyHeight() {
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
	m.dragMode = previewDragVScroll
	m.dragVOffset = grabOffset
}

func (m *PreviewModel) dragVerticalTo(y int) {
	metrics := newScrollbarMetrics(m.bodyHeight(), len(m.displayLines), m.offset)
	if metrics.maxThumbStart() == 0 {
		m.dragMode = previewDragNone
		return
	}
	localY := y - m.bodyStartY() - m.dragVOffset
	if localY < 0 {
		localY = 0
	}
	if max := metrics.maxThumbStart(); localY > max {
		localY = max
	}
	m.offset = metrics.offsetForThumbStart(localY)
}

// horizontalMetrics builds the bottom-bar metrics. The bar spans the full
// pane width while the offset window is the content width; expressing total
// as width+maxOffset keeps both mappings consistent with scrollbarMetrics.
func (m *PreviewModel) horizontalMetrics() scrollbarMetrics {
	total := m.width + m.maxHorizontalOffset()
	return newScrollbarMetrics(m.width, total, m.xoffset)
}

func (m *PreviewModel) beginHorizontalDrag(x int) {
	if m.wrap || m.width <= 0 {
		return
	}
	metrics := m.horizontalMetrics()
	if metrics.maxThumbStart() == 0 {
		return
	}
	localX := x
	if localX < 0 || localX >= m.width {
		return
	}
	grabOffset := metrics.thumbSize / 2
	if localX >= metrics.thumbStart && localX < metrics.thumbStart+metrics.thumbSize {
		grabOffset = localX - metrics.thumbStart
	}
	thumbStart := localX - grabOffset
	if thumbStart < 0 {
		thumbStart = 0
	}
	if max := metrics.maxThumbStart(); thumbStart > max {
		thumbStart = max
	}
	m.xoffset = metrics.offsetForThumbStart(thumbStart)
	m.dragMode = previewDragHScroll
	m.dragHOffset = grabOffset
}

func (m *PreviewModel) dragHorizontalTo(x int) {
	metrics := m.horizontalMetrics()
	if metrics.maxThumbStart() == 0 {
		m.dragMode = previewDragNone
		return
	}
	localX := x - m.dragHOffset
	if localX < 0 {
		localX = 0
	}
	if max := metrics.maxThumbStart(); localX > max {
		localX = max
	}
	m.xoffset = metrics.offsetForThumbStart(localX)
}

func (m *PreviewModel) renderDivider() string {
	return renderStyledLineAt(strings.Repeat(dividerGlyph, m.width), dividerStyle, m.width)
}

func (m *PreviewModel) renderTitle() string {
	title := previewUntitled
	if m.file != "" {
		title = sanitizeDisplay(m.file)
	}
	return renderStyledLineAt(truncateRootPath(title, m.width), titleStyle, m.width)
}

func (m *PreviewModel) unsupportedLabel() string {
	if m.category == previewCategoryText || m.category == "" {
		return ""
	}
	return previewUnsupportedPrefix + string(m.category)
}

func (m *PreviewModel) renderBody(height int) []string {
	rows := make([]string, height)
	if height <= 0 {
		return rows
	}

	if label := m.unsupportedLabel(); label != "" {
		rows[0] = renderStyledLineAt(label, dividerStyle, m.width)
		return rows
	}

	contentWidth := m.contentWidth()
	metrics := newScrollbarMetrics(height, len(m.displayLines), m.offset)
	for row := 0; row < height; row++ {
		var line previewLine
		if index := metrics.offset + row; index >= 0 && index < len(m.displayLines) {
			line = m.displayLines[index]
		}
		rows[row] = m.renderBodyRow(row, line, contentWidth, metrics)
	}
	return rows
}

func (m *PreviewModel) renderBodyRow(row int, line previewLine, contentWidth int, metrics scrollbarMetrics) string {
	scrollbar := renderScrollbarCellWidth(m.width, row, metrics)
	leftPad := strings.Repeat(" ", m.contentLeftPadding())
	if contentWidth <= 0 {
		return leftPad + scrollbar
	}
	separator := " "
	if m.lineCount > 0 {
		separator = dividerStyle.Inline(true).Render(previewGutterDividerGlyph) + " "
	}
	return leftPad + m.renderGutter(line) + separator + m.renderContent(line, contentWidth) + scrollbar
}

func (m *PreviewModel) renderGutter(line previewLine) string {
	if line.number > 0 {
		return fmt.Sprintf("%*d", m.gutterWidth(), line.number)
	}
	return strings.Repeat(" ", m.gutterWidth())
}

func previewWhitespaceText(value string) string {
	if value == "" {
		return ""
	}
	if strings.Trim(value, " ") == "" {
		return strings.Repeat(previewWhitespaceGlyph, utf8.RuneCountInString(value))
	}

	var rendered strings.Builder
	iter := graphemes.FromString(value)
	for iter.Next() {
		piece := iter.Value()
		if piece == " " && lipgloss.Width(piece) == 1 {
			rendered.WriteString(previewWhitespaceGlyph)
		} else {
			rendered.WriteString(piece)
		}
	}
	return rendered.String()
}

func (m *PreviewModel) renderContent(line previewLine, width int) string {
	viewOffset := 0
	if !m.wrap {
		viewOffset = m.xoffset
	}
	spans := previewVisibleSpans(previewSelectionSpans(line, m.selection, m.showWhitespace && !line.muted), viewOffset, width)
	selectionStyle := previewSelectionStyle(m.lightBackground)
	var rendered strings.Builder
	renderedWidth := 0
	for _, span := range spans {
		style := previewSyntaxStyle(span.token, m.lightBackground)
		text := span.text
		if span.space {
			style = previewWhitespaceStyle(m.lightBackground)
			text = previewWhitespaceText(span.text)
		}
		if line.muted {
			style = dividerStyle.Inline(true)
		}
		if span.selected {
			style = style.Inherit(selectionStyle)
		}
		rendered.WriteString(style.Render(text))
		renderedWidth += lipgloss.Width(text)
	}
	if padding := width - renderedWidth; padding > 0 {
		rendered.WriteString(lipgloss.NewStyle().Inline(true).Render(strings.Repeat(" ", padding)))
	}
	return rendered.String()
}

func (m *PreviewModel) renderHorizontalScrollbar() string {
	if m.width <= 0 {
		return ""
	}
	metrics := m.horizontalMetrics()
	var bar strings.Builder
	for cell := 0; cell < m.width; cell++ {
		if metrics.isThumbRow(cell) {
			bar.WriteString(scrollbarThumbStyle.Inline(true).Render(previewHorizontalThumbGlyph))
		} else {
			bar.WriteString(scrollbarTrackStyle.Inline(true).Render(previewHorizontalTrackGlyph))
		}
	}
	return bar.String()
}

func (m *PreviewModel) renderFooter() string {
	if m.toast != "" {
		return renderStyledLineAt(strings.Repeat(" ", m.contentLeftPadding())+m.toast, toastStyle, m.width)
	}
	if m.warning != "" {
		return m.renderLine(m.readyStatus())
	}
	if m.status != m.readyStatus() {
		return m.renderLine(m.status)
	}
	help := renderShortcut(previewHelpCopyKey, previewHelpCopyLabel) + helpGroupSeparator +
		renderShortcut(previewHelpHelpKey, previewHelpHelpLabel) + helpGroupSeparator +
		renderShortcut(previewHelpCloseKey, previewHelpCloseLabel)
	return renderStyledLineAt(strings.Repeat(" ", m.contentLeftPadding())+help, lipgloss.NewStyle(), m.width)
}

func (m *PreviewModel) renderLine(line string) string {
	line = strings.Repeat(" ", m.contentLeftPadding()) + sanitizeDisplay(line)
	return renderStyledLineAt(line, lipgloss.NewStyle(), m.width)
}

func (m *PreviewModel) contentLeftPadding() int {
	if m.width <= 1 {
		return 0
	}
	return contentLeftPadding
}

// contentWidth is the width available for line content after the left
// padding, the gutter, its separator, and the vertical scrollbar column.
func (m *PreviewModel) contentWidth() int {
	return max(0, m.width-m.contentLeftPadding()-m.gutterWidth()-2-1)
}

func (m *PreviewModel) contentStartX() int {
	return m.contentLeftPadding() + m.gutterWidth() + 2
}

func (m *PreviewModel) previewPositionAt(x, y int, clampY bool) (previewPosition, bool) {
	return previewPositionForMouse(
		m.displayLines,
		m.offset,
		m.bodyStartY(),
		m.bodyHeight(),
		x,
		y,
		m.contentStartX(),
		m.contentWidth(),
		m.xoffset,
		clampY,
	)
}

func (m *PreviewModel) clearSelection() {
	m.selection = previewSelection{}
}

// gutterWidth is the maximum line-number width with a fixed two-digit
// minimum, so the gutter keeps the content aligned until the line count
// reaches three digits.
func (m *PreviewModel) gutterWidth() int {
	return max(2, len(strconv.Itoa(max(1, m.lineCount))))
}

func (m *PreviewModel) readyStatus() string {
	if m.warning != "" {
		return "Warning: " + m.warning
	}
	return readyStatus
}

// previewCategoryFor classifies a preview target: known media/binary
// extensions win immediately; unknown or extensionless content is sniffed
// for NUL bytes or invalid UTF-8 and falls back to text otherwise.
func previewCategoryFor(path string, content []byte) previewCategory {
	if category, ok := previewCategoryByExtension[strings.ToLower(filepath.Ext(path))]; ok {
		return category
	}
	if sniffBinaryContent(content) {
		return previewCategoryBinary
	}
	return previewCategoryText
}

func sniffBinaryContent(content []byte) bool {
	head := content
	if len(head) > previewSniffBytes {
		// The byte cut can split one multi-byte rune; drop the trailing
		// partial rune so valid text crossing the boundary is not
		// misclassified as binary. At most three bytes are removed.
		head = trimIncompleteRuneTail(head[:previewSniffBytes])
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	return !utf8.Valid(head)
}

// trimIncompleteRuneTail removes the trailing rune split by a byte-boundary
// cut. Sequences already invalid inside the head are left untouched so
// utf8.Valid can still reject them.
func trimIncompleteRuneTail(head []byte) []byte {
	end := len(head)
	start := end
	for start > 0 && head[start-1] >= 0x80 && head[start-1] <= 0xBF {
		start--
	}
	if start == 0 {
		return head // continuation bytes without a lead are invalid regardless of the cut
	}
	lead := head[start-1]
	size := utf8RuneSize(lead)
	if size == 0 || end-start >= size-1 {
		return head // invalid, complete, or overlong tail: utf8.Valid decides
	}
	if !validRunePrefix(lead, head[start:end]) {
		return head // out-of-range continuation is corruption, not a cut
	}
	return head[:start-1]
}

// utf8RuneSize returns the encoded size of a UTF-8 lead byte, or 0 for
// bytes that can never start a valid rune (0xC0, 0xC1, 0xF5-0xFF).
func utf8RuneSize(lead byte) int {
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		return 2
	case lead >= 0xE0 && lead <= 0xEF:
		return 3
	case lead >= 0xF0 && lead <= 0xF4:
		return 4
	default:
		return 0
	}
}

// validRunePrefix reports whether lead and a strict prefix of its
// continuation bytes can still form a valid rune once the missing bytes
// follow the boundary. Only the first continuation has a lead-specific
// range; the remaining byte positions accept the full 0x80-0xBF range.
func validRunePrefix(lead byte, continuation []byte) bool {
	if len(continuation) == 0 {
		return true
	}
	first := continuation[0]
	switch lead {
	case 0xE0:
		return first >= 0xA0
	case 0xED:
		return first <= 0x9F
	case 0xF0:
		return first >= 0x90
	case 0xF4:
		return first <= 0x8F
	default:
		return true
	}
}

// previewTextLines normalizes the content into numbered display lines:
// CRLF is normalized to LF, tabs expand to fixed-width spaces, and every
// line passes the terminal-safe sanitize boundary.
func previewTextLines(content []byte) []previewLine {
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	rawLines := strings.Split(normalized, "\n")
	lines := make([]previewLine, 0, len(rawLines))
	for index, raw := range rawLines {
		expanded := strings.ReplaceAll(raw, "\t", strings.Repeat(" ", previewTabWidth))
		lines = append(lines, previewLine{text: sanitizeDisplay(expanded), number: index + 1, origin: index})
	}
	return lines
}

func maxContentWidthFor(lines []previewLine) int {
	width := 0
	for _, line := range lines {
		if value := lipgloss.Width(line.text); value > width {
			width = value
		}
	}
	return width
}

// buildDisplayLines maps original lines to rendered lines. Without wrap the
// mapping is identity; with wrap every line is hard-wrapped at width and
// continuation segments lose their line number so the gutter stays blank.
func buildDisplayLines(lines []previewLine, wrap bool, width int) []previewLine {
	if !wrap {
		display := make([]previewLine, 0, len(lines))
		for index, line := range lines {
			line.origin = index
			line.col = 0
			display = append(display, line)
		}
		return display
	}
	display := make([]previewLine, 0, len(lines))
	for origin, line := range lines {
		segments := wrapToWidth(line.text, width)
		column := 0
		for index, segment := range segments {
			number := 0
			if index == 0 {
				number = line.number
			}
			display = append(display, previewLine{
				text:   segment,
				number: number,
				muted:  line.muted,
				origin: origin,
				col:    column,
				spans:  line.spans,
			})
			column += lipgloss.Width(segment)
		}
	}
	return display
}

// wrapToWidth hard-wraps one line into segments of at most width cells.
// Graphemes wider than the width are kept whole and trimmed at render time.
func wrapToWidth(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	rest := line
	var segments []string
	for lipgloss.Width(rest) > width {
		head := cutHeadToWidth(rest, width)
		if head == "" {
			break
		}
		segments = append(segments, head)
		rest = rest[len(head):]
	}
	if rest != "" || len(segments) == 0 {
		segments = append(segments, rest)
	}
	return segments
}

// cutHeadToWidth keeps the leading graphemes that fit width cells, dropping
// any grapheme that straddles the cut boundary.
func cutHeadToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	head := ansi.Truncate(value, width, "")
	for lipgloss.Width(head) > width {
		head = dropLastGrapheme(head)
	}
	return head
}

func dropLastGrapheme(value string) string {
	iter := graphemes.FromString(value)
	var last string
	for iter.Next() {
		last = iter.Value()
	}
	if last == "" {
		return value
	}
	return value[:len(value)-len(last)]
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
