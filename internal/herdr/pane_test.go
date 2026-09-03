package herdr

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// fakeRunner records every invocation and answers with canned output. It is
// the herdr-side test double for the subprocess boundary.
type fakeRunner struct {
	args   [][]string
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(args ...string) (string, string, error) {
	f.args = append(f.args, append([]string(nil), args...))
	return f.stdout, f.stderr, f.err
}

func TestResolvePaneBinaryPrefersHerdrBinPath(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == BinPathEnv {
			return "/custom/herdr", true
		}
		return "", false
	}
	if got := resolvePaneBinary(lookup); got != "/custom/herdr" {
		t.Fatalf("resolvePaneBinary() = %q, want HERDR_BIN_PATH", got)
	}
}

func TestResolvePaneBinaryFallsBackToPathLookup(t *testing.T) {
	for _, values := range []map[string]string{
		{},
		{BinPathEnv: "  "},
		{BinPathEnv: ""},
	} {
		lookup := func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		}
		if got := resolvePaneBinary(lookup); got != "herdr" {
			t.Fatalf("resolvePaneBinary(%v) = %q, want PATH fallback", values, got)
		}
	}
}

func TestOpenPaneBuildsArgumentsAndParsesPaneID(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:plugin","result":{"plugin_pane":{"entrypoint":"preview","pane":{"pane_id":"wY:p9Z"},"plugin_id":"u7chan.file-viewer"},"type":"plugin_pane_opened"}}`}
	client := &CLIPaneClient{runner: runner}

	paneID, err := client.OpenPane(OpenPaneRequest{
		Plugin:     "u7chan.file-viewer",
		Entrypoint: "preview",
		Placement:  "split",
		TargetPane: "wY:p3K",
		Direction:  "right",
		Env:        []string{"HERDR_PREVIEW_FILE=/abs/file.md"},
	})
	if err != nil {
		t.Fatalf("OpenPane() error = %v", err)
	}
	if paneID != "wY:p9Z" {
		t.Fatalf("OpenPane() pane ID = %q, want wY:p9Z", paneID)
	}

	want := []string{"plugin", "pane", "open",
		"--plugin", "u7chan.file-viewer",
		"--entrypoint", "preview",
		"--placement", "split",
		"--target-pane", "wY:p3K",
		"--direction", "right",
		"--no-focus",
		"--env", "HERDR_PREVIEW_FILE=/abs/file.md",
	}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], want) {
		t.Fatalf("OpenPane() args = %v, want %v", runner.args, want)
	}
}

func TestOpenPaneReportsMissingPaneIDAndErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		stdout string
		stderr string
		err    error
		want   string
	}{
		{name: "empty pane id", stdout: `{"result":{"plugin_pane":{"pane":{}}}}`, want: "no pane_id"},
		{name: "error envelope on stderr", stderr: `{"error":{"code":"plugin_pane_not_found","message":"entrypoint missing"},"id":"cli:plugin"}`, err: errors.New("exit status 1"), want: "plugin_pane_not_found"},
		{name: "CLI missing", err: errors.New("executable not found"), want: "herdr CLI"},
		{name: "unparseable response", stdout: "not json", want: "parse herdr CLI output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &CLIPaneClient{runner: &fakeRunner{stdout: test.stdout, stderr: test.stderr, err: test.err}}
			_, err := client.OpenPane(OpenPaneRequest{
				Plugin: "p", Entrypoint: "e", Placement: "split", TargetPane: "t", Direction: "right",
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("OpenPane() error = %v, want mention of %q", err, test.want)
			}
		})
	}
}

func TestOpenPaneUsesFocusFlagAndOmitsDirectionForOverlays(t *testing.T) {
	runner := &fakeRunner{stdout: `{"result":{"plugin_pane":{"pane":{"pane_id":"wY:h1"}}}}`}
	client := &CLIPaneClient{runner: runner}

	paneID, err := client.OpenPane(OpenPaneRequest{
		Plugin:     "u7chan.file-viewer",
		Entrypoint: "help",
		Placement:  "overlay",
		TargetPane: "wY:p3K",
		Focus:      true,
		Env:        []string{"HERDR_HELP_CONTEXT=tree"},
	})
	if err != nil || paneID != "wY:h1" {
		t.Fatalf("OpenPane() = %q, %v; want overlay pane id without error", paneID, err)
	}
	want := []string{"plugin", "pane", "open",
		"--plugin", "u7chan.file-viewer",
		"--entrypoint", "help",
		"--placement", "overlay",
		"--target-pane", "wY:p3K",
		"--focus",
		"--env", "HERDR_HELP_CONTEXT=tree",
	}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], want) {
		t.Fatalf("OpenPane() args = %v, want %v", runner.args, want)
	}
}

func TestGetPaneParsesTokensAndDistinguishesNotFound(t *testing.T) {
	client := &CLIPaneClient{runner: &fakeRunner{stdout: `{"id":"cli:pane:get","result":{"pane":{"pane_id":"wY:p9Z","tokens":{"preview":"/abs/file.md"}},"type":"pane_info"}}`}}
	info, found, err := client.GetPane("wY:p9Z")
	if err != nil || !found {
		t.Fatalf("GetPane() = %#v, %v, %v; want found pane", info, found, err)
	}
	if info.PaneID != "wY:p9Z" || info.Tokens["preview"] != "/abs/file.md" {
		t.Fatalf("GetPane() info = %#v, want pane id and preview token", info)
	}

	missing := &fakeRunner{stderr: `{"error":{"code":"pane_not_found","message":"pane wY:p9Z not found"},"id":"cli:pane:get"}`, err: errors.New("exit status 1")}
	client = &CLIPaneClient{runner: missing}
	info, found, err = client.GetPane("wY:p9Z")
	if err != nil || found {
		t.Fatalf("GetPane(missing) = %#v, %v, %v; want not found without error", info, found, err)
	}
	if !reflect.DeepEqual(missing.args[0], []string{"pane", "get", "wY:p9Z"}) {
		t.Fatalf("GetPane() args = %v, want pane get invocation", missing.args)
	}
}

func TestGetPaneSurfacesNonNotFoundErrors(t *testing.T) {
	client := &CLIPaneClient{runner: &fakeRunner{stderr: `{"error":{"code":"internal_error","message":"socket broke"},"id":"cli:pane:get"}`, err: errors.New("exit status 1")}}
	_, found, err := client.GetPane("wY:p9Z")
	if found || err == nil || !strings.Contains(err.Error(), "socket broke") {
		t.Fatalf("GetPane() found = %v, error = %v; want surfaced error", found, err)
	}
}

func TestClosePaneToleratesAlreadyMissingPane(t *testing.T) {
	client := &CLIPaneClient{runner: &fakeRunner{stderr: `{"error":{"code":"pane_not_found","message":"gone"},"id":"cli:plugin"}`, err: errors.New("exit status 1")}}
	if err := client.ClosePane("wY:p9Z"); err != nil {
		t.Fatalf("ClosePane(missing) error = %v, want nil", err)
	}
}

func TestListPanesScopesToWorkspaceAndParsesTokens(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:pane:list","result":{"panes":[
		{"pane_id":"wY:p1","tokens":{}},
		{"pane_id":"wY:p2","tokens":{"preview":"/a.md"}},
		{"pane_id":"wY:p3"}
	],"type":"pane_list"}}`}
	client := &CLIPaneClient{runner: runner}

	panes, err := client.ListPanes("wY")
	if err != nil {
		t.Fatalf("ListPanes() error = %v", err)
	}
	if len(panes) != 3 || panes[1].PaneID != "wY:p2" || panes[1].Tokens["preview"] != "/a.md" {
		t.Fatalf("ListPanes() = %#v, want three panes with tokens", panes)
	}
	if !reflect.DeepEqual(runner.args[0], []string{"pane", "list", "--workspace", "wY"}) {
		t.Fatalf("ListPanes() args = %v, want workspace-scoped list", runner.args)
	}

	client = &CLIPaneClient{runner: &fakeRunner{stdout: `{"result":{"panes":[]}}`}}
	if _, err := client.ListPanes(""); err != nil {
		t.Fatalf("ListPanes(empty workspace) error = %v", err)
	}
	if !reflect.DeepEqual(client.runner.(*fakeRunner).args[0], []string{"pane", "list"}) {
		t.Fatalf("ListPanes() unscoped args = %v, want plain list", client.runner.(*fakeRunner).args)
	}
}

func TestReportMetadataPlacesPaneIDFirstAndParsesSilentSuccess(t *testing.T) {
	client := &CLIPaneClient{runner: &fakeRunner{}}
	if err := client.ReportMetadata(ReportMetadataRequest{
		PaneID: "wY:p9Z",
		Source: "u7chan.file-viewer",
		Tokens: []string{"preview=/abs/file.md"},
	}); err != nil {
		t.Fatalf("ReportMetadata() error = %v, want silent success", err)
	}
	want := []string{"pane", "report-metadata", "wY:p9Z", "--source", "u7chan.file-viewer", "--token", "preview=/abs/file.md"}
	runner := client.runner.(*fakeRunner)
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], want) {
		t.Fatalf("ReportMetadata() args = %v, want %v", runner.args, want)
	}

	failing := &CLIPaneClient{runner: &fakeRunner{err: fmt.Errorf("exit status 2")}}
	if err := failing.ReportMetadata(ReportMetadataRequest{PaneID: "wY:p9Z", Source: "s"}); err == nil {
		t.Fatal("ReportMetadata() error = nil, want exit status error")
	}
}
