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
		{name: "error envelope on stderr", stderr: `{"error":{"code":"plugin_pane_not_found","message":"entrypoint missing"},"id":"cli:plugin"}`, err: errors.New("exit status 1"), want: "plugin pane not found"},
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

func TestOpenPaneAcceptsBareOKResponseForPopupPlacements(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:plugin","result":{"type":"ok"}}`}
	client := &CLIPaneClient{runner: runner}

	paneID, err := client.OpenPane(OpenPaneRequest{
		Plugin:     "u7chan.file-viewer",
		Entrypoint: "help",
		Focus:      true,
		Env:        []string{"HERDR_HELP_CONTEXT=tree"},
	})
	if err != nil {
		t.Fatalf("OpenPane() error = %v, want nil for bare ok popup response", err)
	}
	if paneID != "" {
		t.Fatalf("OpenPane() pane ID = %q, want empty for popup", paneID)
	}
}

func TestOpenPaneOmitsPlacementWhenManifestDeclaresItAndOmitsDirectionAndTargetForOverlays(t *testing.T) {
	runner := &fakeRunner{stdout: `{"result":{"plugin_pane":{"pane":{"pane_id":"wY:h1"}}}}`}
	client := &CLIPaneClient{runner: runner}

	paneID, err := client.OpenPane(OpenPaneRequest{
		Plugin:     "u7chan.file-viewer",
		Entrypoint: "help",
		Focus:      true,
		Env:        []string{"HERDR_HELP_CONTEXT=tree"},
	})
	if err != nil || paneID != "wY:h1" {
		t.Fatalf("OpenPane() = %q, %v; want popup pane id without error", paneID, err)
	}
	want := []string{"plugin", "pane", "open",
		"--plugin", "u7chan.file-viewer",
		"--entrypoint", "help",
		"--focus",
		"--env", "HERDR_HELP_CONTEXT=tree",
	}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], want) {
		t.Fatalf("OpenPane() args = %v, want %v (popup declared in manifest, no --placement; popup targets the active pane, so no --target-pane)", runner.args, want)
	}
}

func TestOpenPaneIncludesTargetFlagOnlyWhenATargetIsGiven(t *testing.T) {
	runner := &fakeRunner{stdout: `{"result":{"plugin_pane":{"pane":{"pane_id":"wY:p9Z"}}}}`}
	client := &CLIPaneClient{runner: runner}

	if _, err := client.OpenPane(OpenPaneRequest{
		Plugin: "u7chan.file-viewer", Entrypoint: "preview", Placement: "split",
		TargetPane: "wY:p3K", Direction: "right",
	}); err != nil {
		t.Fatalf("OpenPane() error = %v", err)
	}
	want := []string{"plugin", "pane", "open",
		"--plugin", "u7chan.file-viewer",
		"--entrypoint", "preview",
		"--placement", "split",
		"--target-pane", "wY:p3K",
		"--direction", "right",
		"--no-focus",
	}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], want) {
		t.Fatalf("OpenPane() args = %v, want %v (split keeps its required target)", runner.args, want)
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

func TestListWorkspacesParsesWorkspaceIDs(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[
		{"workspace_id":"wA","label":"alpha","number":1},
		{"workspace_id":"wB","label":"beta","number":2}
	]}}`}
	client := &CLIPaneClient{runner: runner}

	workspaces, err := client.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if len(workspaces) != 2 || workspaces[0].WorkspaceID != "wA" || workspaces[1].Label != "beta" {
		t.Fatalf("ListWorkspaces() = %#v, want both workspaces", workspaces)
	}
	if !reflect.DeepEqual(runner.args[0], []string{"workspace", "list"}) {
		t.Fatalf("ListWorkspaces() args = %v, want plain workspace list", runner.args)
	}
}

func TestProcessInfoParsesForegroundProcesses(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:pane:process_info","result":{"process_info":{
		"pane_id":"wY:p1",
		"foreground_processes":[
			{"pid":1,"name":"bash","argv":["/bin/bash"]},
			{"pid":2,"name":"git","argv":["/usr/bin/git","status"]}
		]
	},"type":"pane_process_info"}}`}
	client := &CLIPaneClient{runner: runner}

	info, err := client.ProcessInfo("wY:p1")
	if err != nil {
		t.Fatalf("ProcessInfo() error = %v", err)
	}
	want := PaneProcessInfo{
		PaneID: "wY:p1",
		ForegroundProcesses: []PaneProcess{
			{Name: "bash", Argv: []string{"/bin/bash"}},
			{Name: "git", Argv: []string{"/usr/bin/git", "status"}},
		},
	}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("ProcessInfo() = %#v, want %#v", info, want)
	}
	if !reflect.DeepEqual(runner.args[0], []string{"pane", "process-info", "--pane", "wY:p1"}) {
		t.Fatalf("ProcessInfo() args = %v, want explicit --pane targeting", runner.args)
	}
}

func TestProcessInfoHandlesMissingForegroundList(t *testing.T) {
	client := &CLIPaneClient{runner: &fakeRunner{stdout: `{"result":{"process_info":{"pane_id":"wY:p1"}}}`}}
	info, err := client.ProcessInfo("wY:p1")
	if err != nil {
		t.Fatalf("ProcessInfo() error = %v", err)
	}
	if len(info.ForegroundProcesses) != 0 {
		t.Fatalf("ProcessInfo() = %#v, want no foreground processes", info)
	}
}

func TestRunCommandPassesTheCommandAsOneArgument(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:pane:run","result":{"ok":true}}`}
	client := &CLIPaneClient{runner: runner}

	command := "exec env HERDR_PLUGIN_ENTRYPOINT_ID=files '/path with space' /bin/viewer"
	if err := client.RunCommand("wY:p9Z", command); err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	want := []string{"pane", "run", "wY:p9Z", command}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], want) {
		t.Fatalf("RunCommand() args = %v, want %v (command stays one argument)", runner.args, want)
	}
}

func TestClosePreviewPaneUsesPluginCloseForOwnedPanes(t *testing.T) {
	client := &CLIPaneClient{runner: &fakeRunner{stdout: `{"result":{"pane_id":"wY:p9Z"}}`}}
	if err := client.ClosePreviewPane("wY:p9Z"); err != nil {
		t.Fatalf("ClosePreviewPane() error = %v", err)
	}
	runner := client.runner.(*fakeRunner)
	want := []string{"plugin", "pane", "close", "wY:p9Z"}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], want) {
		t.Fatalf("ClosePreviewPane() args = %v, want plugin close for owned panes", runner.args)
	}
}

func TestClosePreviewPaneFallsBackToPlainPaneCloseForRestoredPanes(t *testing.T) {
	runner := &seqRunner{calls: []seqCall{
		{stderr: `{"error":{"code":"plugin_pane_not_found","message":"plugin pane not found"},"id":"cli:plugin"}`, err: errors.New("exit status 1")},
		{stdout: `{"result":{"ok":true}}`},
	}}
	client := &CLIPaneClient{runner: runner}

	if err := client.ClosePreviewPane("wY:p9Z"); err != nil {
		t.Fatalf("ClosePreviewPane() error = %v, want the plain close fallback", err)
	}
	if len(runner.args) != 2 {
		t.Fatalf("ClosePreviewPane() calls = %v, want plugin close then plain close", runner.args)
	}
	if !reflect.DeepEqual(runner.args[1], []string{"pane", "close", "wY:p9Z"}) {
		t.Fatalf("ClosePreviewPane() fallback args = %v, want plain pane close", runner.args[1])
	}
}

func TestClosePreviewPaneToleratesAlreadyMissingPaneOnFallback(t *testing.T) {
	runner := &seqRunner{calls: []seqCall{
		{stderr: `{"error":{"code":"plugin_pane_not_found","message":"plugin pane not found"},"id":"cli:plugin"}`, err: errors.New("exit status 1")},
		{stderr: `{"error":{"code":"pane_not_found","message":"gone"},"id":"cli:pane"}`, err: errors.New("exit status 1")},
	}}
	client := &CLIPaneClient{runner: runner}
	if err := client.ClosePreviewPane("wY:p9Z"); err != nil {
		t.Fatalf("ClosePreviewPane() error = %v, want the missing-pane fallback to succeed", err)
	}
	if len(runner.args) != 2 {
		t.Fatalf("ClosePreviewPane() calls = %v, want plugin close then plain close", runner.args)
	}
}

// seqRunner answers each invocation with the next canned response, so one
// fake can model CLI sequences such as plugin-close then fallback-close.
type seqCall struct {
	stdout string
	stderr string
	err    error
}

type seqRunner struct {
	calls []seqCall
	args  [][]string
}

func (r *seqRunner) Run(args ...string) (string, string, error) {
	r.args = append(r.args, append([]string(nil), args...))
	index := len(r.args) - 1
	if index < len(r.calls) {
		call := r.calls[index]
		return call.stdout, call.stderr, call.err
	}
	return "", "", errors.New("unexpected extra CLI call")
}

func TestGetPaneParsesLabelAndSavedCWD(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:pane:get","result":{"pane":{
		"pane_id":"wY:p1","workspace_id":"wY","tab_id":"wY:t1",
		"label":"Files","cwd":"/home/u7dev/work space"
	},"type":"pane_info"}}`}
	client := &CLIPaneClient{runner: runner}

	info, found, err := client.GetPane("wY:p1")
	if err != nil || !found {
		t.Fatalf("GetPane() = %#v, %v, %v; want found pane", info, found, err)
	}
	if info.Label != "Files" || info.CWD != "/home/u7dev/work space" || info.WorkspaceID != "wY" || info.TabID != "wY:t1" {
		t.Fatalf("GetPane() info = %#v, want label, cwd, workspace, and tab", info)
	}
}

func TestListPanesParsesLabelsAndSavedCWDs(t *testing.T) {
	runner := &fakeRunner{stdout: `{"id":"cli:pane:list","result":{"panes":[
		{"pane_id":"wY:p1","workspace_id":"wY","tab_id":"wY:t1","label":"Files","cwd":"/a"},
		{"pane_id":"wY:p2","workspace_id":"wY","tab_id":"wY:t1","label":"Preview","cwd":"/b","tokens":{"preview":"/b/file.md"}}
	],"type":"pane_list"}}`}
	client := &CLIPaneClient{runner: runner}

	panes, err := client.ListPanes("wY")
	if err != nil {
		t.Fatalf("ListPanes() error = %v", err)
	}
	if panes[0].Label != "Files" || panes[0].CWD != "/a" {
		t.Fatalf("ListPanes()[0] = %#v, want Files label and cwd", panes[0])
	}
	if panes[1].Label != "Preview" || panes[1].Tokens["preview"] != "/b/file.md" {
		t.Fatalf("ListPanes()[1] = %#v, want Preview label and token", panes[1])
	}
}

func TestIsStartupEventMatchesPluginEventEnv(t *testing.T) {
	t.Setenv(PluginEventEnv, "startup")
	if !IsStartupEvent() {
		t.Fatal("IsStartupEvent() = false with HERDR_PLUGIN_EVENT=startup")
	}
	t.Setenv(PluginEventEnv, "worktree.created")
	if IsStartupEvent() {
		t.Fatal("IsStartupEvent() = true with a non-startup event")
	}
}
