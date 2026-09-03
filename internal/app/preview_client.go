package app

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u7chan/herdr-file-viewer/internal/browser"
)

// PreviewMetadataToken is the pane metadata token name the preview pane
// reports for itself and the tree re-discovers through `pane list`. The
// token value is the previewed file path.
const PreviewMetadataToken = "preview"

// PreviewPane is the pane data the tree model needs to track the preview.
// File holds the PreviewMetadataToken value; it is empty for panes without
// a preview token.
type PreviewPane struct {
	PaneID string
	File   string
}

// PreviewClient is the Herdr pane capability the tree and preview models
// need, kept as an interface so the composition root can inject the
// subprocess implementation and tests can supply deterministic doubles.
type PreviewClient interface {
	// OpenPreview opens the preview entrypoint in a right split beside
	// targetPane without focusing it, and returns the new pane ID.
	OpenPreview(file, targetPane string) (paneID string, err error)
	// ClosePane closes a pane. Closing an already-missing pane succeeds.
	ClosePane(paneID string) error
	// GetPane reports whether the pane still exists.
	GetPane(paneID string) (PreviewPane, bool, error)
	// ListPanes returns the panes of one workspace.
	ListPanes(workspaceID string) ([]PreviewPane, error)
	// TagPreview reports the preview file as a metadata token of paneID.
	TagPreview(paneID, file string) error
}

// PreviewConfig wires the tree model to the preview-pane capability. A zero
// config (or an empty TargetPane) makes preview activation a safe no-op,
// which is the behavior outside a Herdr pane.
type PreviewConfig struct {
	Client      PreviewClient
	TargetPane  string
	WorkspaceID string
}

// previewResultMsg is the outcome of one preview activation command.
// paneID is the pane to track afterwards ("" clears the tracked pane).
type previewResultMsg struct {
	seq    int
	paneID string
	err    string
}

// openPreviewOnActivate launches preview activation for the selected row: it
// resolves the selectable file target and, inside a command, ensures exactly
// one preview pane shows that file. Directories, non-file symlinks, missing
// Herdr context, and configuration absence keep activation a no-op. A
// successful resolution consumes the find underline at the synchronous moment
// the launch is attempted, while lastQuery stays for n/N repeat navigation.
func (m *Model) openPreviewOnActivate() tea.Cmd {
	node := m.selectedNode()
	if node == nil {
		return nil
	}
	file, ok := previewTargetPath(node)
	if !ok || m.previewConfig.Client == nil || m.previewConfig.TargetPane == "" {
		return nil
	}

	// The underline is a pointer to the found target; once the preview launch
	// is attempted the pointer has served its purpose, even on CLI failure.
	m.findHighlightQuery = ""

	m.previewSeq++
	seq := m.previewSeq
	client := m.previewConfig.Client
	workspaceID := m.previewConfig.WorkspaceID
	targetPane := m.previewConfig.TargetPane
	trackedPaneID := m.previewPaneID
	return func() tea.Msg {
		paneID, err := runPreviewSwap(client, workspaceID, trackedPaneID, file, targetPane)
		if err != nil {
			return previewResultMsg{seq: seq, err: sanitizeDisplay(err.Error())}
		}
		return previewResultMsg{seq: seq, paneID: paneID}
	}
}

// previewTargetPath resolves the selected node to a previewable absolute
// file path. Directories are not previewable; a symlink only qualifies when
// its resolved target is a regular file.
func previewTargetPath(node *browser.Node) (string, bool) {
	if node == nil || node.IsDirectory() {
		return "", false
	}
	path := node.Path()
	if node.IsSymlink() {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return "", false
		}
	}
	return path, true
}

// runPreviewSwap guarantees that exactly one preview pane displays file:
// the tracked pane (from a previous open) is checked through GetPane, and
// when it is unknown or dead the workspace list is searched for a pane
// carrying the preview token. A pane already showing the same file is kept
// (no-op); a pane showing another file is closed and reopened; otherwise a
// new pane is opened. The returned pane ID is the one to track afterwards.
func runPreviewSwap(client PreviewClient, workspaceID, trackedPaneID, file, targetPane string) (string, error) {
	if trackedPaneID != "" {
		pane, found, err := client.GetPane(trackedPaneID)
		if err != nil {
			return "", err
		}
		if found {
			if pane.File == file {
				return trackedPaneID, nil
			}
			if err := client.ClosePane(trackedPaneID); err != nil {
				return "", err
			}
			return client.OpenPreview(file, targetPane)
		}
	}

	panes, err := client.ListPanes(workspaceID)
	if err != nil {
		return "", err
	}
	for _, pane := range panes {
		if pane.File == "" {
			continue
		}
		if pane.File == file {
			return pane.PaneID, nil
		}
		if err := client.ClosePane(pane.PaneID); err != nil {
			return "", err
		}
		return client.OpenPreview(file, targetPane)
	}
	return client.OpenPreview(file, targetPane)
}

// addWarning appends one distinct warning to the persistent footer warning,
// so repeated preview failures do not stack up.
func addWarning(existing, next string) string {
	if next == "" {
		return existing
	}
	if existing != "" && strings.Contains(existing, next) {
		return existing
	}
	if existing == "" {
		return next
	}
	return existing + "; " + next
}
