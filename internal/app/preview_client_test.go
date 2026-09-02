package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

func TestRunPreviewSwapKeepsOnePreviewForTheFile(t *testing.T) {
	open := stubPreviewClient{openPaneID: "wY:p9Z"}
	tests := []struct {
		name         string
		client       stubPreviewClient
		tracked      string
		file         string
		wantPaneID   string
		wantOpens    int
		wantCloses   []string
		wantGetCalls int
		wantList     int
	}{
		{
			name: "tracked pane shows the same file, keep it",
			client: stubPreviewClient{
				getPane:  PreviewPane{PaneID: "wY:p1", File: "/a.md"},
				getFound: true,
			},
			tracked:      "wY:p1",
			file:         "/a.md",
			wantPaneID:   "wY:p1",
			wantGetCalls: 1,
		},
		{
			name: "tracked pane shows another file, close and reopen",
			client: stubPreviewClient{
				getPane:  PreviewPane{PaneID: "wY:p1", File: "/old.md"},
				getFound: true,
			},
			tracked:      "wY:p1",
			file:         "/new.md",
			wantPaneID:   "wY:p9Z",
			wantOpens:    1,
			wantCloses:   []string{"wY:p1"},
			wantGetCalls: 1,
		},
		{
			name: "tracked pane is gone, rediscover the same file in the list",
			client: stubPreviewClient{
				panes: []PreviewPane{{PaneID: "wY:p2", File: "/a.md"}},
			},
			tracked:      "wY:p1",
			file:         "/a.md",
			wantPaneID:   "wY:p2",
			wantGetCalls: 1,
			wantList:     1,
		},
		{
			name: "tracked pane is gone, rediscovered pane shows another file",
			client: stubPreviewClient{
				panes: []PreviewPane{{PaneID: "wY:p2", File: "/old.md"}},
			},
			tracked:      "wY:p1",
			file:         "/new.md",
			wantPaneID:   "wY:p9Z",
			wantOpens:    1,
			wantCloses:   []string{"wY:p2"},
			wantGetCalls: 1,
			wantList:     1,
		},
		{
			name:       "no tracked pane and no preview token, open fresh",
			client:     stubPreviewClient{},
			file:       "/new.md",
			wantPaneID: "wY:p9Z",
			wantOpens:  1,
			wantList:   1,
		},
		{
			name: "untagged panes in the list are ignored",
			client: stubPreviewClient{
				panes: []PreviewPane{{PaneID: "wY:p2"}, {PaneID: "wY:p3", File: "/a.md"}},
			},
			file:       "/a.md",
			wantPaneID: "wY:p3",
			wantList:   1,
		},
		{
			name: "tracked pane exists without a token, reopen it",
			client: stubPreviewClient{
				getPane:  PreviewPane{PaneID: "wY:p1"},
				getFound: true,
			},
			tracked:      "wY:p1",
			file:         "/a.md",
			wantPaneID:   "wY:p9Z",
			wantOpens:    1,
			wantCloses:   []string{"wY:p1"},
			wantGetCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := test.client
			client.openPaneID = open.openPaneID
			paneID, err := runPreviewSwap(&client, "wY", test.tracked, test.file, "wY:p3K")
			if err != nil {
				t.Fatalf("runPreviewSwap() error = %v", err)
			}
			if paneID != test.wantPaneID {
				t.Fatalf("runPreviewSwap() pane ID = %q, want %q", paneID, test.wantPaneID)
			}
			if len(client.openFiles) != test.wantOpens {
				t.Fatalf("opens = %v, want %d", client.openFiles, test.wantOpens)
			}
			if !reflect.DeepEqual(client.closed, test.wantCloses) {
				t.Fatalf("closed = %v, want %v", client.closed, test.wantCloses)
			}
			if len(client.getCalls) != test.wantGetCalls {
				t.Fatalf("get calls = %v, want %d", client.getCalls, test.wantGetCalls)
			}
			if len(client.listed) != test.wantList {
				t.Fatalf("list calls = %v, want %d", client.listed, test.wantList)
			}
			if test.wantOpens == 1 {
				if client.openFiles[0] != test.file || client.openTargets[0] != "wY:p3K" {
					t.Fatalf("open request = %v -> %v, want %q -> target pane", client.openFiles, client.openTargets, test.file)
				}
			}
		})
	}
}

func TestRunPreviewSwapSurfacesClientFailures(t *testing.T) {
	tests := []struct {
		name   string
		client stubPreviewClient
		want   string
	}{
		{name: "get failure", client: stubPreviewClient{getErr: errors.New("socket broke")}, want: "socket broke"},
		{name: "list failure", client: stubPreviewClient{listErr: errors.New("daemon down")}, want: "daemon down"},
		{name: "close failure", client: stubPreviewClient{getPane: PreviewPane{PaneID: "wY:p1", File: "/x"}, getFound: true, closeErr: errors.New("close failed")}, want: "close failed"},
		{name: "open failure", client: stubPreviewClient{openErr: errors.New("open failed")}, want: "open failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := test.client
			_, err := runPreviewSwap(&client, "wY", "wY:p1", "/a.md", "wY:p3K")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("runPreviewSwap() error = %v, want mention of %q", err, test.want)
			}
		})
	}
}

func TestPreviewTargetPathRequiresAFileTarget(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "directory", Mode: fs.ModeDir},
		{Name: "file", Mode: 0},
		{Name: "link-to-file", Mode: fs.ModeSymlink},
		{Name: "link-to-dir", Mode: fs.ModeSymlink},
		{Name: "dangling", Mode: fs.ModeSymlink},
	})
	// Real targets for the symlink resolution; the tree itself stays on the
	// fake filesystem.
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "file"), filepath.Join(root, "link-to-file")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "directory"), filepath.Join(root, "link-to-dir")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	rows := model.visibleRows

	// Entries sort directories first, then by name: directory | dangling,
	// file, link-to-dir, link-to-file.
	for _, test := range []struct {
		name     string
		row      int
		wantFile string
		wantOK   bool
	}{
		{name: "directory is not previewable", row: 1},
		{name: "dangling symlink is not previewable", row: 2},
		{name: "regular file is previewable", row: 3, wantFile: filepath.Join(root, "file"), wantOK: true},
		{name: "symlink to a directory is not previewable", row: 4},
		{name: "symlink to a file is previewable", row: 5, wantFile: filepath.Join(root, "link-to-file"), wantOK: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, ok := previewTargetPath(rows[test.row].Node)
			if ok != test.wantOK || file != test.wantFile {
				t.Fatalf("previewTargetPath() = %q, %v; want %q, %v", file, ok, test.wantFile, test.wantOK)
			}
		})
	}
}

func TestEnterWithoutHerdrContextIsASafeNoOp(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file", Mode: 0}})
	model := NewModel(root, "", fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("enter without client returned command %v, want nil", cmd)
	}

	model = NewModelWithPreview(root, "", PreviewConfig{Client: &stubPreviewClient{}, WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("enter without target pane returned command %v, want nil", cmd)
	}
}

func TestMouseFileClickOpensPreview(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt", Mode: 0}})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})

	command := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: model.treeStartY() + stickyRootHeight, Button: tea.MouseLeft})
	if command == nil {
		t.Fatal("file click returned nil command")
	}
	message := command()
	result, ok := message.(previewResultMsg)
	if !ok {
		t.Fatalf("file click command message = %T, want previewResultMsg", message)
	}
	model.Update(result)

	if model.selectedNode().Name() != "file.txt" {
		t.Fatalf("selected node = %q, want file.txt", model.selectedNode().Name())
	}
	if got, want := client.openFiles, []string{filePath}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opened files = %v, want %v", got, want)
	}
	if got, want := client.openTargets, []string{"wY:p3K"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("open targets = %v, want %v", got, want)
	}
	if got, want := client.listed, []string{"wY"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("listed workspaces = %v, want %v", got, want)
	}
	if model.previewPaneID != "wY:p9Z" {
		t.Fatalf("tracked pane = %q, want wY:p9Z", model.previewPaneID)
	}
}

func TestMouseFileClickOnSameFileKeepsExistingPreview(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt", Mode: 0}})
	client := &stubPreviewClient{
		openPaneID: "wY:p9Z",
		getPane:    PreviewPane{PaneID: "wY:p9Z", File: filePath},
		getFound:   true,
	}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	click := tea.MouseClickMsg{X: 0, Y: model.treeStartY() + stickyRootHeight, Button: tea.MouseLeft}

	first := model.UpdateMouse(click)
	if first == nil {
		t.Fatal("initial file click returned nil command")
	}
	model.Update(first().(previewResultMsg))
	openCount, closeCount := len(client.openFiles), len(client.closed)
	if openCount != 1 || closeCount != 0 {
		t.Fatalf("initial file click pane operations = opens %v / closes %v, want one open", client.openFiles, client.closed)
	}

	second := model.UpdateMouse(click)
	if second == nil {
		t.Fatal("same-file click returned nil command")
	}
	model.Update(second().(previewResultMsg))
	if len(client.openFiles) != openCount || len(client.closed) != closeCount {
		t.Fatalf("same-file click changed pane operations: opens %v / closes %v", client.openFiles, client.closed)
	}
	if got, want := client.getCalls, []string{"wY:p9Z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("same-file click get calls = %v, want %v", got, want)
	}
	if got, want := client.listed, []string{"wY"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("same-file click listed workspaces = %v, want %v", got, want)
	}
}

func TestMouseFileClickOnDifferentFileClosesAndReopensPreview(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "a.txt")
	bPath := filepath.Join(root, "b.txt")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "a.txt", Mode: 0}, {Name: "b.txt", Mode: 0}})
	client := &stubPreviewClient{
		openPaneID: "wY:p9Z",
		getPane:    PreviewPane{PaneID: "wY:p9Z", File: aPath},
		getFound:   true,
	}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 8})

	first := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: model.treeStartY() + stickyRootHeight, Button: tea.MouseLeft})
	if first == nil {
		t.Fatal("first file click returned nil command")
	}
	model.Update(first().(previewResultMsg))
	second := model.UpdateMouse(tea.MouseClickMsg{X: 0, Y: model.treeStartY() + stickyRootHeight + 1, Button: tea.MouseLeft})
	if second == nil {
		t.Fatal("second file click returned nil command")
	}
	model.Update(second().(previewResultMsg))

	if got, want := client.openFiles, []string{aPath, bPath}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opened files = %v, want %v", got, want)
	}
	if got, want := client.closed, []string{"wY:p9Z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("closed panes = %v, want %v", got, want)
	}
}

func TestEnterOpensPreviewAndKeepsTreeState(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt", Mode: 0}})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	status := model.status
	selected := model.selected

	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned nil command")
	}
	model.Update(cmd().(previewResultMsg))

	if len(client.openFiles) != 1 || client.openFiles[0] != filePath || client.openTargets[0] != "wY:p3K" {
		t.Fatalf("open requests = %v -> %v, want file via target pane", client.openFiles, client.openTargets)
	}
	if client.listed[0] != "wY" {
		t.Fatalf("list workspace = %v, want wY", client.listed)
	}
	if model.previewPaneID != "wY:p9Z" {
		t.Fatalf("tracked pane = %q, want wY:p9Z", model.previewPaneID)
	}
	if model.status != status || model.selected != selected {
		t.Fatalf("tree state changed: status %q/%q selected %d/%d", model.status, status, model.selected, selected)
	}
}

func TestEnterOnDirectoryDoesNotOpenPreview(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "directory", Mode: fs.ModeDir}})
	fake.set(filepath.Join(root, "directory"), nil)
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("enter on directory returned command %v, want nil", cmd)
	}
	if len(client.openFiles) != 0 {
		t.Fatalf("directory enter opened %v, want none", client.openFiles)
	}
}

func TestEnterSameFileKeepsExistingPreview(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt", Mode: 0}})
	client := &stubPreviewClient{
		openPaneID: "wY:p9Z",
		getPane:    PreviewPane{PaneID: "wY:p9Z", File: filepath.Join(root, "file.txt")},
		getFound:   true,
	}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	first := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	model.Update(first().(previewResultMsg))
	if len(client.openFiles) != 1 {
		t.Fatalf("initial open count = %d, want 1", len(client.openFiles))
	}
	client.getCalls = nil

	second := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if second == nil {
		t.Fatal("enter on the same file returned nil command")
	}
	model.Update(second().(previewResultMsg))
	if model.previewPaneID != "wY:p9Z" {
		t.Fatalf("tracked pane = %q, want unchanged", model.previewPaneID)
	}
	if len(client.openFiles) != 1 || len(client.closed) != 0 {
		t.Fatalf("same-file enter opened %v / closed %v, want no-op", client.openFiles, client.closed)
	}
}

func TestEnterDifferentFileClosesAndReopensPreview(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "a.txt", Mode: 0}, {Name: "b.txt", Mode: 0}})
	client := &stubPreviewClient{
		openPaneID: "wY:p9Z",
		getPane:    PreviewPane{PaneID: "wY:p9Z", File: filepath.Join(root, "a.txt")},
		getFound:   true,
	}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.previewPaneID = "wY:p9Z"
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned nil command")
	}
	model.Update(cmd().(previewResultMsg))
	if model.previewPaneID != "wY:p9Z" {
		t.Fatalf("tracked pane = %q, want new pane", model.previewPaneID)
	}
	if len(client.closed) != 1 || client.closed[0] != "wY:p9Z" {
		t.Fatalf("closed = %v, want the old pane", client.closed)
	}
	if len(client.openFiles) != 1 || client.openFiles[0] != filepath.Join(root, "b.txt") {
		t.Fatalf("opens = %v, want the new file", client.openFiles)
	}
}

func TestEnterRediscoveryAdoptsPreviewFromList(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt", Mode: 0}})
	previous := PreviewPane{PaneID: "wY:pOld", File: filepath.Join(root, "file.txt")}
	client := &stubPreviewClient{
		openPaneID: "wY:p9Z",
		panes:      []PreviewPane{previous},
	}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned nil command")
	}
	model.Update(cmd().(previewResultMsg))
	if model.previewPaneID != "wY:pOld" {
		t.Fatalf("tracked pane = %q, want rediscovered preview", model.previewPaneID)
	}
	if len(client.openFiles) != 0 {
		t.Fatalf("rediscovery opened %v, want no-op", client.openFiles)
	}
}

func TestEnterFailureWarnsAndKeepsTheTreeWorking(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt", Mode: 0}})
	client := &stubPreviewClient{openErr: errors.New("herdr CLI unavailable\x1b")}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned nil command")
	}
	model.Update(cmd().(previewResultMsg))
	if !strings.HasPrefix(model.warning, "Preview: ") || strings.ContainsAny(model.status, "\x1b") {
		t.Fatalf("warning = %q, want sanitized preview warning", model.warning)
	}
	if model.loading {
		t.Fatal("preview failure left the tree loading")
	}
	// The tree keeps navigating after the failure.
	model.UpdateKey(tea.KeyPressMsg{Code: 'j'}) // no-op at boundary
	if model.status != readyStatus && model.status != "Warning: "+model.warning {
		t.Fatalf("tree status = %q, want continued operation", model.status)
	}
}

func TestEnterStaleResultsDoNotOverwriteNewerState(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "file.txt", Mode: 0}})
	client := &stubPreviewClient{
		openPaneID: "wY:p9Z",
		getPane:    PreviewPane{PaneID: "wY:p9Z", File: filepath.Join(root, "file.txt")},
		getFound:   true,
	}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	first := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	second := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	firstMessage := first().(previewResultMsg)
	secondMessage := second().(previewResultMsg)

	// The older result arrives after the newer one was issued.
	model.Update(secondMessage)
	model.Update(firstMessage)
	if model.previewPaneID != secondMessage.paneID {
		t.Fatalf("tracked pane = %q, want the newer result %q", model.previewPaneID, secondMessage.paneID)
	}
}

func TestPreviewTargetPathRejectsNilNode(t *testing.T) {
	if _, ok := previewTargetPath(nil); ok {
		t.Fatal("previewTargetPath(nil) = ok, want false")
	}
}

func TestPreviewActivationOpensSymlinkToFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	link := filepath.Join(root, "link.txt")
	if err := os.WriteFile(target, []byte("content"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "target.txt", Mode: 0},
		{Name: "link.txt", Mode: fs.ModeSymlink},
	})
	client := &stubPreviewClient{openPaneID: "wY:p9Z"}
	model := NewModelWithPreview(root, "", PreviewConfig{Client: client, TargetPane: "wY:p3K", WorkspaceID: "wY"}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // link.txt sorts before target.txt

	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on symlink returned nil command")
	}
	model.Update(cmd().(previewResultMsg))
	if len(client.openFiles) != 1 || client.openFiles[0] != link {
		t.Fatalf("symlink enter opened %v, want %q", client.openFiles, link)
	}
}
