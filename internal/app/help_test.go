package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

// stubHelpClient records every overlay launch and answers with a canned
// result.
type stubHelpClient struct {
	requests []HelpOpenRequest
	err      error
	paneID   string
}

func (s *stubHelpClient) OpenHelp(request HelpOpenRequest) (string, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return "", s.err
	}
	return s.paneID, nil
}

func TestTreeHelpKeyOpensFocusedTreeContextOverlay(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, nil)
	client := &stubHelpClient{paneID: "wY:h1"}
	model := NewModelConfigured(root, "", ModelConfig{
		Help: HelpConfig{Client: client, TargetPane: "wY:p3K"},
	}, fake)
	completeInitialLoad(t, model)

	cmd := model.UpdateKey(helpKey)
	if cmd == nil {
		t.Fatal("h returned nil command")
	}
	message := cmd().(helpResultMsg)
	model.Update(message)
	if message.err != "" {
		t.Fatalf("help result error = %q, want clean launch", message.err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("help launches = %d, want 1", len(client.requests))
	}
	want := HelpOpenRequest{Context: helpTreeContext, TargetPane: "wY:p3K"}
	if client.requests[0] != want {
		t.Fatalf("help request = %#v, want %#v", client.requests[0], want)
	}
	if model.helpPending {
		t.Fatal("helpPending stays set after the launch result")
	}
	if model.warning != "" || model.status != readyStatus {
		t.Fatalf("state after help = warning %q status %q, want unchanged", model.warning, model.status)
	}
}

func TestPreviewHelpKeyOpensPreviewContextOverlay(t *testing.T) {
	client := &stubHelpClient{paneID: "wY:h1"}
	model := NewPreviewModelWithConfig("/abs/file.md", nil, "wY:p9Z", HelpConfig{Client: client})
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})

	_, cmd := model.Update(helpKey)
	if cmd == nil {
		t.Fatal("h returned nil command")
	}
	if message := cmd().(helpResultMsg); message.err != "" {
		t.Fatalf("help result error = %q, want clean launch", message.err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("help launches = %d, want 1", len(client.requests))
	}
	want := HelpOpenRequest{Context: helpPreviewContext, TargetPane: "wY:p9Z"}
	if client.requests[0] != want {
		t.Fatalf("help request = %#v, want %#v with the preview pane as target", client.requests[0], want)
	}
}

func TestHelpLaunchDoesNotDuplicateOnRapidRepeatedKeys(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, nil)
	client := &stubHelpClient{paneID: "wY:h1"}
	model := NewModelConfigured(root, "", ModelConfig{
		Help: HelpConfig{Client: client, TargetPane: "wY:p3K"},
	}, fake)
	completeInitialLoad(t, model)

	first := model.UpdateKey(helpKey)
	if first == nil {
		t.Fatal("first h returned nil command")
	}
	for range 3 {
		if cmd := model.UpdateKey(helpKey); cmd != nil {
			t.Fatalf("h while a launch is in flight returned command %v, want ignored", cmd)
		}
	}
	if len(client.requests) != 0 || !model.helpPending {
		t.Fatalf("in-flight state = requests %d, pending %v; want one pending launch", len(client.requests), model.helpPending)
	}

	model.Update(first().(helpResultMsg))
	if model.helpPending {
		t.Fatal("helpPending stays set after the launch result")
	}
	second := model.UpdateKey(helpKey)
	if second == nil {
		t.Fatal("h after a completed launch returned nil command")
	}
	model.Update(second().(helpResultMsg))
	if len(client.requests) != 2 {
		t.Fatalf("help launches = %d, want 2 (one per h press, none from the repeated presses)", len(client.requests))
	}
}

func TestHelpLaunchFailureKeepsCallerStateAndWarns(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	client := &stubHelpClient{err: errors.New("daemon down\x1b\n")}
	model := NewModelConfigured(root, "", ModelConfig{
		Help: HelpConfig{Client: client, TargetPane: "wY:p3K"},
	}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	selected, rows := model.selected, len(model.visibleRows)

	cmd := model.UpdateKey(helpKey)
	if cmd == nil {
		t.Fatal("h returned nil command")
	}
	model.Update(cmd().(helpResultMsg))
	if model.warning != "Help: daemon down��" {
		t.Fatalf("warning = %q, want sanitized help failure", model.warning)
	}
	if model.status != "Warning: Help: daemon down��" {
		t.Fatalf("status = %q, want the warning visible in the footer", model.status)
	}
	if model.selected != selected || len(model.visibleRows) != rows || model.tree.Root().Path() != root {
		t.Fatalf("failed help changed state: selected %d/%d, rows %d/%d, root %q", model.selected, selected, len(model.visibleRows), rows, model.tree.Root().Path())
	}
	footer := strings.TrimRight(ansi.Strip(strings.Split(model.View().Content, "\n")[5]), " ")
	if !strings.Contains(footer, "Warning: Help: daemon down") {
		t.Fatalf("failed help footer = %q, want warning", footer)
	}
	if model.helpPending {
		t.Fatal("failed launch left helpPending set, want retryable")
	}
}

func TestHelpWithoutHerdrContextWarnsWithoutFallback(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, nil)
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 6})

	if cmd := model.UpdateKey(helpKey); cmd != nil {
		t.Fatalf("h without a help client returned command %v, want warning-only no-op", cmd)
	}
	if !strings.Contains(model.warning, "Help unavailable: no Herdr context") {
		t.Fatalf("warning = %q, want missing-context warning", model.warning)
	}
	if model.tree.Root().Path() != root {
		t.Fatalf("h without context changed the root to %q", model.tree.Root().Path())
	}
	footer := ansi.Strip(model.View().Content)
	if !strings.Contains(footer, "Warning: Help unavailable: no Herdr context") {
		t.Fatalf("footer = %q, want the missing-context warning", footer)
	}

	preview := NewPreviewModel("/abs/file.md", nil, "wY:p9Z")
	if _, cmd := preview.Update(helpKey); cmd != nil {
		t.Fatalf("preview h without a help client returned command %v, want warning-only no-op", cmd)
	}
	if !strings.Contains(preview.warning, "Help unavailable: no Herdr context") {
		t.Fatalf("preview warning = %q, want missing-context warning", preview.warning)
	}
}

func TestHelpWithClientButNoPaneIDIsTreatedAsNoHerdrContext(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, nil)
	client := &stubHelpClient{paneID: "wY:h1"}
	model := NewModelConfigured(root, "", ModelConfig{
		Help: HelpConfig{Client: client}, // no TargetPane: outside a Herdr pane
	}, fake)
	completeInitialLoad(t, model)

	if cmd := model.UpdateKey(helpKey); cmd != nil {
		t.Fatalf("h with an empty target pane returned command %v, want warning-only no-op", cmd)
	}
	if len(client.requests) != 0 {
		t.Fatalf("help launches = %d, want none without a Herdr pane id", len(client.requests))
	}
	if !strings.Contains(model.warning, "Help unavailable: no Herdr context") {
		t.Fatalf("warning = %q, want missing-context warning", model.warning)
	}

	previewClient := &stubHelpClient{paneID: "wY:h1"}
	preview := NewPreviewModelWithConfig("/abs/file.md", nil, "", HelpConfig{Client: previewClient})
	if _, cmd := preview.Update(helpKey); cmd != nil {
		t.Fatalf("preview h without a pane id returned command %v, want warning-only no-op", cmd)
	}
	if len(previewClient.requests) != 0 || !strings.Contains(preview.warning, "Help unavailable: no Herdr context") {
		t.Fatalf("preview without pane id launched %d helps, warning %q; want warning only", len(previewClient.requests), preview.warning)
	}
}

func TestHelpModelRendersContextSpecificTitlesAndOperations(t *testing.T) {
	tree := NewHelpModel(helpTreeContext)
	tree.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	treeView := ansi.Strip(tree.View().Content)
	for _, want := range []string{
		helpTitleTree,
		"C / Backspace",
		"root move",
		"/, n, N, Esc",
		"reload",
		"copy path",
		"q / Ctrl+C",
		"quit",
	} {
		if !strings.Contains(treeView, want) {
			t.Fatalf("tree help view = %q, want %q", treeView, want)
		}
	}
	if strings.Contains(treeView, helpTitlePreview) || strings.Contains(treeView, "horizontal scroll") {
		t.Fatalf("tree help view = %q, leaks preview reference rows", treeView)
	}

	preview := NewHelpModel(helpPreviewContext)
	preview.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	previewView := ansi.Strip(preview.View().Content)
	for _, want := range []string{
		helpTitlePreview,
		"horizontal scroll",
		"wrap",
		"spaces",
		"copy selection",
		"close",
		"q / Ctrl+C",
	} {
		if !strings.Contains(previewView, want) {
			t.Fatalf("preview help view = %q, want %q", previewView, want)
		}
	}
	if strings.Contains(previewView, helpTitleTree) || strings.Contains(previewView, "root move") {
		t.Fatalf("preview help view = %q, leaks tree reference rows", previewView)
	}
}

func TestHelpModelUnknownContextFallsBackToTreeReference(t *testing.T) {
	model := NewHelpModel("bogus")
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, helpTitleTree) || !strings.Contains(view, "root move") {
		t.Fatalf("fallback help view = %q, want the tree reference", view)
	}
}

func TestHelpModelCloseKeysQuitAndOtherKeysAreIgnored(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		helpKey,
		{Code: tea.KeyEsc},
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		model := NewHelpModel(helpTreeContext)
		_, cmd := model.Update(key)
		if cmd == nil {
			t.Fatalf("Update(%q) returned nil command, want quit", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("Update(%q) command = %T, want tea.QuitMsg", key.String(), cmd())
		}
	}

	model := NewHelpModel(helpTreeContext)
	for _, key := range []tea.KeyPressMsg{
		{Code: 'j', Text: "j"},
		{Code: tea.KeyEnter},
		{Code: tea.KeyRight},
		{Code: 'x', Text: "x"},
	} {
		if _, cmd := model.Update(key); cmd != nil {
			t.Fatalf("Update(%q) returned command %v, want ignored", key.String(), cmd)
		}
	}
}

func TestHelpModelRowsStayWithinNarrowWidths(t *testing.T) {
	for _, width := range []int{0, 1, 8, 16, 40} {
		model := NewHelpModel(helpTreeContext)
		model.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		for lineNumber, line := range strings.Split(ansi.Strip(model.View().Content), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d help line %d = %d cells, want <= %d: %q", width, lineNumber, got, width, line)
			}
		}
	}
	for _, width := range []int{4, 12} {
		model := NewHelpModel(helpPreviewContext)
		model.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		for lineNumber, line := range strings.Split(ansi.Strip(model.View().Content), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d preview help line %d = %d cells, want <= %d: %q", width, lineNumber, got, width, line)
			}
		}
	}
}

func TestFindModeConsumesCAndHAndBackspaceBeforeRootMenuAndHelp(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "child")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "child", Mode: fs.ModeDir}})
	fake.set(target, nil)
	chdir := &chdirRecorder{}
	helpClient := &stubHelpClient{paneID: "wY:h1"}
	model := NewModelConfigured(root, "", ModelConfig{
		Help:  HelpConfig{Client: helpClient, TargetPane: "wY:p3K"},
		Chdir: chdir.chdir,
	}, fake)
	completeInitialLoad(t, model)

	model.UpdateKey(tea.KeyPressMsg{Code: '/'})
	model.UpdateKey(shiftC)
	if model.findQuery != "C" {
		t.Fatalf("find query after C = %q, want the typed C", model.findQuery)
	}
	model.UpdateKey(helpKey)
	if model.findQuery != "Ch" {
		t.Fatalf("find query after h = %q, want the typed h", model.findQuery)
	}
	model.UpdateKey(backspaceKey)
	if model.findQuery != "C" {
		t.Fatalf("find query after backspace = %q, want one grapheme shorter", model.findQuery)
	}
	if len(chdir.paths) != 0 || len(helpClient.requests) != 0 {
		t.Fatalf("find keys moved the root (chdir %v) or opened help (requests %d)", chdir.paths, len(helpClient.requests))
	}
	if model.tree.Root().Path() != root || !model.findActive {
		t.Fatalf("find mode state = root %q, active %v; want unchanged active find", model.tree.Root().Path(), model.findActive)
	}
}

func TestReadyFootersShowOnlyTheThreeCoreOperationsAndStayWithinWidth(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)

	for _, width := range []int{6, 11, 24, 80} {
		model.Update(tea.WindowSizeMsg{Width: width, Height: 6})
		content := ansi.Strip(model.View().Content)
		lines := strings.Split(content, "\n")
		footer := strings.TrimRight(lines[5], " ")
		if width >= 40 && footer != " space copy    h help    q quit" {
			t.Fatalf("width %d tree footer = %q, want the three core operations", width, footer)
		}
		if got := lipgloss.Width(lines[5]); got > width {
			t.Fatalf("width %d tree footer width = %d, want <= %d: %q", width, got, width, lines[5])
		}
		if width >= 40 && strings.Contains(footer, "reload") {
			t.Fatalf("width %d tree footer = %q, must not list reload", width, footer)
		}
	}

	reader := &fakePreviewReader{content: []byte("hello\n")}
	preview := NewPreviewModel("/abs/file.md", nil, "wY:p9Z", reader)
	preview.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	preview.Update(loadResultFromPreviewCommand(t, preview.Init()))
	for _, width := range []int{6, 11, 24, 40} {
		preview.Update(tea.WindowSizeMsg{Width: width, Height: 8})
		lines := strings.Split(ansi.Strip(preview.View().Content), "\n")
		footer := strings.TrimRight(lines[len(lines)-1], " ")
		if width >= 40 && footer != " space copy    h help    q close" {
			t.Fatalf("width %d preview footer = %q, want the three core operations", width, footer)
		}
		if got := lipgloss.Width(lines[len(lines)-1]); got > width {
			t.Fatalf("width %d preview footer width = %d, want <= %d: %q", width, got, width, lines[len(lines)-1])
		}
		if width >= 40 && strings.Contains(footer, "wrap") {
			t.Fatalf("width %d preview footer = %q, must not list wrap", width, footer)
		}
	}
}

// loadResultFromPreviewCommand unwraps the preview file load message, which
// stays untagged because previews do not change roots.
func loadResultFromPreviewCommand(t testing.TB, cmd tea.Cmd) previewLoadMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("preview command is nil")
	}
	message := cmd()
	if result, ok := message.(previewLoadMsg); ok {
		return result
	}
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		t.Fatalf("preview command message = %T, want previewLoadMsg or tea.BatchMsg", message)
	}
	for _, command := range batch {
		if result, ok := command().(previewLoadMsg); ok {
			return result
		}
	}
	t.Fatalf("preview batch contains no previewLoadMsg")
	return previewLoadMsg{}
}
