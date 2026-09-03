package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/u7chan/herdr-file-viewer/internal/app"
	"github.com/u7chan/herdr-file-viewer/internal/herdr"
)

// stubPaneClient is the herdr-side test double for the composition root
// adapter tests.
type stubPaneClient struct {
	openRequest herdr.OpenPaneRequest
	openID      string
	openErr     error

	closeErr error
	closed   []string

	getInfo  herdr.PaneInfo
	getFound bool
	getErr   error

	listInfo []herdr.PaneInfo
	listErr  error

	reportRequest herdr.ReportMetadataRequest
	reportErr     error
}

func (s *stubPaneClient) OpenPane(request herdr.OpenPaneRequest) (string, error) {
	s.openRequest = request
	return s.openID, s.openErr
}

func (s *stubPaneClient) ClosePane(paneID string) error {
	s.closed = append(s.closed, paneID)
	return s.closeErr
}

func (s *stubPaneClient) GetPane(paneID string) (herdr.PaneInfo, bool, error) {
	return s.getInfo, s.getFound, s.getErr
}

func (s *stubPaneClient) ListPanes(workspaceID string) ([]herdr.PaneInfo, error) {
	return s.listInfo, s.listErr
}

func (s *stubPaneClient) ReportMetadata(request herdr.ReportMetadataRequest) error {
	s.reportRequest = request
	return s.reportErr
}

func TestPreviewClientAdapterOpenPreviewFixesPluginIdentityAndEnv(t *testing.T) {
	stub := &stubPaneClient{openID: "wY:p9Z"}
	client := paneClientAdapter{client: stub}

	paneID, err := client.OpenPreview("/abs/file.md", "wY:p3K")
	if err != nil || paneID != "wY:p9Z" {
		t.Fatalf("OpenPreview() = %q, %v; want pane id without error", paneID, err)
	}
	want := herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.PreviewEntrypointID,
		Placement:  "split",
		TargetPane: "wY:p3K",
		Direction:  "right",
		Env:        []string{herdr.PreviewFileEnv + "=/abs/file.md"},
	}
	if !reflect.DeepEqual(stub.openRequest, want) {
		t.Fatalf("OpenPane() request = %#v, want %#v", stub.openRequest, want)
	}

	stub.openErr = errors.New("daemon down")
	if _, err := client.OpenPreview("/a", "wY:p3K"); err == nil {
		t.Fatal("OpenPreview() error = nil, want propagated error")
	}
}

func TestPreviewClientAdapterGetPaneExtractsPreviewToken(t *testing.T) {
	stub := &stubPaneClient{
		getInfo:  herdr.PaneInfo{PaneID: "wY:p9Z", Tokens: map[string]string{app.PreviewMetadataToken: "/abs/file.md"}},
		getFound: true,
	}
	client := paneClientAdapter{client: stub}

	pane, found, err := client.GetPane("wY:p9Z")
	if err != nil || !found || pane.PaneID != "wY:p9Z" || pane.File != "/abs/file.md" {
		t.Fatalf("GetPane() = %#v, %v, %v; want token-extracted pane", pane, found, err)
	}

	untagged := &stubPaneClient{getInfo: herdr.PaneInfo{PaneID: "wY:p9Z"}, getFound: true}
	pane, found, err = paneClientAdapter{client: untagged}.GetPane("wY:p9Z")
	if err != nil || !found || pane.File != "" {
		t.Fatalf("GetPane(untagged) = %#v, %v, %v; want empty file", pane, found, err)
	}

	missing := &stubPaneClient{getFound: false}
	pane, found, err = paneClientAdapter{client: missing}.GetPane("wY:p9Z")
	if found || err != nil || pane != (app.PreviewPane{}) {
		t.Fatalf("GetPane(missing) = %#v, %v, %v; want not found without error", pane, found, err)
	}
}

func TestPreviewClientAdapterListPanesMapsTokens(t *testing.T) {
	stub := &stubPaneClient{listInfo: []herdr.PaneInfo{
		{PaneID: "wY:p1"},
		{PaneID: "wY:p2", Tokens: map[string]string{app.PreviewMetadataToken: "/a.md"}},
	}}
	panes, err := paneClientAdapter{client: stub}.ListPanes("wY")
	if err != nil {
		t.Fatalf("ListPanes() error = %v", err)
	}
	if len(panes) != 2 || panes[1].PaneID != "wY:p2" || panes[1].File != "/a.md" || panes[0].File != "" {
		t.Fatalf("ListPanes() = %#v, want both panes with extracted token", panes)
	}
}

func TestPreviewClientAdapterTagPreviewReportsMetadata(t *testing.T) {
	stub := &stubPaneClient{}
	err := paneClientAdapter{client: stub}.TagPreview("wY:p9Z", "/abs/file.md")
	if err != nil {
		t.Fatalf("TagPreview() error = %v", err)
	}
	want := herdr.ReportMetadataRequest{
		PaneID: "wY:p9Z",
		Source: pluginID,
		Tokens: []string{app.PreviewMetadataToken + "=/abs/file.md"},
	}
	if !reflect.DeepEqual(stub.reportRequest, want) {
		t.Fatalf("ReportMetadata() request = %#v, want %#v", stub.reportRequest, want)
	}
}

func TestHelpClientAdapterOpenHelpFixesPluginOverlayFocusAndContext(t *testing.T) {
	stub := &stubPaneClient{openID: "wY:h1"}
	paneID, err := (helpClientAdapter{client: stub}).OpenHelp(app.HelpOpenRequest{Context: "preview", TargetPane: "wY:p9Z"})
	if err != nil || paneID != "wY:h1" {
		t.Fatalf("OpenHelp() = %q, %v; want pane id without error", paneID, err)
	}
	want := herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.HelpEntrypointID,
		Placement:  "overlay",
		TargetPane: "wY:p9Z",
		Focus:      true,
		Env:        []string{herdr.HelpContextEnv + "=preview"},
	}
	if !reflect.DeepEqual(stub.openRequest, want) {
		t.Fatalf("OpenPane() request = %#v, want %#v", stub.openRequest, want)
	}

	stub.openErr = errors.New("daemon down")
	if _, err := (helpClientAdapter{client: stub}).OpenHelp(app.HelpOpenRequest{Context: "tree", TargetPane: "wY:p3K"}); err == nil {
		t.Fatal("OpenHelp() error = nil, want propagated error")
	}
}

func TestHelpClientAdapterOpenHelpKeepsEveryCallerContextDistinct(t *testing.T) {
	stub := &stubPaneClient{openID: "wY:h1"}
	adapter := helpClientAdapter{client: stub}

	if _, err := adapter.OpenHelp(app.HelpOpenRequest{Context: "tree", TargetPane: "wY:p3K"}); err != nil {
		t.Fatalf("OpenHelp(tree) error = %v", err)
	}
	treeWant := herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.HelpEntrypointID,
		Placement:  "overlay",
		TargetPane: "wY:p3K",
		Focus:      true,
		Env:        []string{herdr.HelpContextEnv + "=tree"},
	}
	if !reflect.DeepEqual(stub.openRequest, treeWant) {
		t.Fatalf("OpenPane(tree) request = %#v, want %#v", stub.openRequest, treeWant)
	}
}

func TestPreviewClientAdapterClosePanePassesThrough(t *testing.T) {
	stub := &stubPaneClient{}
	if err := (paneClientAdapter{client: stub}).ClosePane("wY:p9Z"); err != nil {
		t.Fatalf("ClosePane() error = %v", err)
	}
	if !reflect.DeepEqual(stub.closed, []string{"wY:p9Z"}) {
		t.Fatalf("closed = %v, want the pane id", stub.closed)
	}
}

func TestPreviewClientAdapterClosePanePropagatesFailure(t *testing.T) {
	stub := &stubPaneClient{closeErr: errors.New("close failed")}
	if err := (paneClientAdapter{client: stub}).ClosePane("wY:p9Z"); err == nil {
		t.Fatal("ClosePane() error = nil, want propagated error")
	}
}
