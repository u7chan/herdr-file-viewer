package herdr

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeRestoreClient is the RestoreClient test double: it serves canned
// workspace/pane listings and per-pane process-info snapshots that can
// evolve between calls, and it records every pane run command.
type fakeRestoreClient struct {
	workspaces []Workspace
	panesByID  map[string]PaneInfo

	listWorkspacesErr error
	listPanesErr      error

	processInfo map[string]PaneProcessInfo
	processErr  map[string]error
	processSeq  map[string]int

	runCommands []string
	runPaneIDs  []string
	runErr      error
}

func (f *fakeRestoreClient) ListWorkspaces() ([]Workspace, error) {
	return f.workspaces, f.listWorkspacesErr
}

func (f *fakeRestoreClient) ListPanes(workspaceID string) ([]PaneInfo, error) {
	if f.listPanesErr != nil {
		return nil, f.listPanesErr
	}
	panes := make([]PaneInfo, 0)
	for _, pane := range f.panesByID {
		if pane.WorkspaceID == workspaceID {
			panes = append(panes, pane)
		}
	}
	sort.Slice(panes, func(i, j int) bool { return panes[i].PaneID < panes[j].PaneID })
	return panes, nil
}

func (f *fakeRestoreClient) ProcessInfo(paneID string) (PaneProcessInfo, error) {
	f.processSeq[paneID]++
	index := f.processSeq[paneID] - 1
	if err := f.processErr[paneID]; err != nil {
		return PaneProcessInfo{}, err
	}
	info := f.processInfo[paneID]
	if index > 0 {
		if info, ok := f.processInfo[paneID+"@"+strconv.Itoa(index)]; ok {
			return info, nil
		}
	}
	return info, nil
}

func (f *fakeRestoreClient) RunCommand(paneID, command string) error {
	f.runPaneIDs = append(f.runPaneIDs, paneID)
	f.runCommands = append(f.runCommands, command)
	return f.runErr
}

func (f *fakeRestoreClient) processCalls(paneID string) int {
	return f.processSeq[paneID]
}

// fakeWriteLog collects the restore log lines.
type fakeWriteLog struct {
	lines []string
}

func (l *fakeWriteLog) Write(p []byte) (int, error) {
	l.lines = append(l.lines, strings.TrimSpace(string(p)))
	return len(p), nil
}

// testRestorer builds a Restorer with deterministic timing: the sleep is a
// recording no-op, so pending rounds advance without real time.
func testRestorer(client RestoreClient, state *PreviewStateStore, log *fakeWriteLog, poll, timeout time.Duration) (*Restorer, *[]time.Duration) {
	slept := make([]time.Duration, 0)
	writer := io.Writer(io.Discard)
	if log != nil {
		writer = log
	}
	restorer := NewRestorer(RestoreConfig{
		Client:  client,
		State:   state,
		Log:     writer,
		Sleep:   func(d time.Duration) { slept = append(slept, d) },
		Poll:    poll,
		Timeout: timeout,
		Env: RestoreLaunchEnv{
			PluginRoot:      "/home/u7dev/workspace/herdr-file-viewer",
			PluginConfigDir: "/cfg/path",
			PluginStateDir:  "/state/path",
			SocketPath:      "/tmp/herdr-test.sock",
			BinPath:         "/usr/bin/herdr",
			Executable:      "/home/u7dev/workspace/herdr-file-viewer/bin/herdr-file-viewer",
		},
	})
	return restorer, &slept
}

func bashPane() PaneProcessInfo {
	return PaneProcessInfo{ForegroundProcesses: []PaneProcess{{Name: "bash", Argv: []string{"/bin/bash"}}}}
}

func viewerPane() PaneProcessInfo {
	return PaneProcessInfo{ForegroundProcesses: []PaneProcess{{Name: "herdr-file-view", Argv: []string{"/home/u7dev/bin/herdr-file-viewer"}}}}
}

func TestRestoreEnumeratesEveryWorkspaceAndRestoresFilesAndPreview(t *testing.T) {
	stateDir := t.TempDir()
	store := NewPreviewStateStore(stateDir, "/tmp/herdr-test.sock")
	if err := store.Save("wB:p2", "/pre view/file=1.md"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA", Label: "one"}, {WorkspaceID: "wB", Label: "two"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "", CWD: "/root"},
			"wA:p2": {PaneID: "wA:p2", WorkspaceID: "wA", Label: "Files", CWD: "/work space"},
			"wA:p3": {PaneID: "wA:p3", WorkspaceID: "wA", Label: "Shell", CWD: "/other"},
			"wB:p1": {PaneID: "wB:p1", WorkspaceID: "wB", Label: "Files", CWD: "/root2"},
			"wB:p2": {PaneID: "wB:p2", WorkspaceID: "wB", Label: "Preview", CWD: "/root2"},
		},
		processInfo: map[string]PaneProcessInfo{
			"wA:p2": bashPane(),
			"wB:p1": bashPane(),
			"wB:p2": bashPane(),
		},
		processSeq: map[string]int{},
	}
	log := &fakeWriteLog{}
	restorer, _ := testRestorer(client, store, log, time.Millisecond, time.Millisecond)

	reports := restorer.Run()

	byPane := map[string]RestoreOutcome{}
	for _, report := range reports {
		byPane[report.PaneID] = report.Outcome
	}
	if byPane["wA:p2"] != RestoreRestored || byPane["wB:p1"] != RestoreRestored || byPane["wB:p2"] != RestoreRestored {
		t.Fatalf("outcomes = %v, want both Files panes and the Preview pane restored", byPane)
	}
	if len(reports) != 3 {
		t.Fatalf("reports = %v, want exactly the three candidates", reports)
	}
	if len(client.runCommands) != 3 {
		t.Fatalf("run commands = %d, want 3: %v", len(client.runCommands), client.runCommands)
	}
	if client.processCalls("wA:p1") != 0 || client.processCalls("wA:p3") != 0 {
		t.Fatalf("non-candidate panes were classified: wA:p1=%d wA:p3=%d", client.processCalls("wA:p1"), client.processCalls("wA:p3"))
	}
	if !strings.Contains(client.runCommands[2], "HERDR_PLUGIN_ENTRYPOINT_ID=preview") {
		t.Fatalf("preview command = %q, want the preview entrypoint", client.runCommands[2])
	}

	filesCommand := client.runCommands[0]
	if !strings.Contains(filesCommand, "HERDR_PLUGIN_CONTEXT_JSON=") {
		t.Fatalf("files command = %q, want a pane-specific context", filesCommand)
	}
	for _, line := range log.lines {
		if !strings.HasPrefix(line, "startup restore: pane=") {
			t.Fatalf("log line = %q, want restore report prefix", line)
		}
	}
}

func TestRestoreFilesContextIsBuiltPerPaneFromItsOwnCwd(t *testing.T) {
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Files", CWD: "/first/root"},
			"wA:p2": {PaneID: "wA:p2", WorkspaceID: "wA", Label: "Files", CWD: "/second/root"},
		},
		processInfo: map[string]PaneProcessInfo{"wA:p1": bashPane(), "wA:p2": bashPane()},
		processSeq:  map[string]int{},
	}
	restorer, _ := testRestorer(client, NewPreviewStateStore(t.TempDir(), "/sock"), nil, time.Millisecond, time.Millisecond)

	restorer.Run()

	if len(client.runCommands) != 2 {
		t.Fatalf("run commands = %d, want 2", len(client.runCommands))
	}
	if !strings.Contains(client.runCommands[0], `{"focused_pane_cwd":"/first/root"}`) {
		t.Fatalf("first command = %q, want the first pane's own cwd", client.runCommands[0])
	}
	if !strings.Contains(client.runCommands[1], `{"focused_pane_cwd":"/second/root"}`) {
		t.Fatalf("second command = %q, want the second pane's own cwd", client.runCommands[1])
	}
	if strings.Contains(client.runCommands[0], "/second/root") {
		t.Fatalf("first command leaks the other pane's cwd: %q", client.runCommands[0])
	}
}

func TestRestoreClassifiesPendingAlreadyAndRunningCommands(t *testing.T) {
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Files", CWD: "/a"},
			"wA:p2": {PaneID: "wA:p2", WorkspaceID: "wA", Label: "Files", CWD: "/b"},
			"wA:p3": {PaneID: "wA:p3", WorkspaceID: "wA", Label: "Files", CWD: "/c"},
		},
		processInfo: map[string]PaneProcessInfo{
			"wA:p1": bashPane(),
			"wA:p2": viewerPane(),
			"wA:p3": {ForegroundProcesses: []PaneProcess{{Name: "vim", Argv: []string{"/usr/bin/vim", "/f"}}}},
		},
		processSeq: map[string]int{},
	}
	log := &fakeWriteLog{}
	restorer, _ := testRestorer(client, NewPreviewStateStore(t.TempDir(), "/sock"), log, time.Millisecond, time.Millisecond)

	reports := restorer.Run()

	byPane := map[string]RestoreReport{}
	for _, report := range reports {
		byPane[report.PaneID] = report
	}
	if byPane["wA:p1"].Outcome != RestoreRestored {
		t.Fatalf("idle bash pane outcome = %v, want restored", byPane["wA:p1"].Outcome)
	}
	if byPane["wA:p2"].Outcome != RestoreAlready {
		t.Fatalf("running viewer outcome = %v, want already", byPane["wA:p2"].Outcome)
	}
	if byPane["wA:p3"].Outcome != RestoreTimeout {
		t.Fatalf("running command outcome = %v, want timeout (pane untouched)", byPane["wA:p3"].Outcome)
	}
	if len(client.runCommands) != 1 || client.processCalls("wA:p3") != 2 {
		t.Fatalf("pane with running command was modified or mispolled: commands=%v calls=%d", client.runCommands, client.processCalls("wA:p3"))
	}
}

func TestRestorePendingBecomesReadyAcrossPollRounds(t *testing.T) {
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Files", CWD: "/a"},
		},
		processInfo: map[string]PaneProcessInfo{
			// The shell is not yet the foreground process for the first two
			// snapshots; it settles before the poll window expires.
			"wA:p1":   {},
			"wA:p1@1": {},
			"wA:p1@2": bashPane(),
		},
		processSeq: map[string]int{},
	}
	log := &fakeWriteLog{}
	restorer, slept := testRestorer(client, NewPreviewStateStore(t.TempDir(), "/sock"), log, 10*time.Millisecond, 25*time.Millisecond)

	reports := restorer.Run()

	if len(reports) != 1 || reports[0].Outcome != RestoreRestored {
		t.Fatalf("reports = %v, want the pending pane restored", reports)
	}
	if client.processCalls("wA:p1") != 3 {
		t.Fatalf("process-info calls = %d, want initial + two poll rounds", client.processCalls("wA:p1"))
	}
	if !reflect.DeepEqual(*slept, []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}) {
		t.Fatalf("sleeps = %v, want one poll interval per round", *slept)
	}
	if len(client.runCommands) != 1 {
		t.Fatalf("run commands = %v, want the late-ready pane restored once", client.runCommands)
	}
}

func TestRestoreTimeoutLeavesPendingPaneUntouched(t *testing.T) {
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Files", CWD: "/a"},
		},
		processInfo: map[string]PaneProcessInfo{"wA:p1": {}},
		processSeq:  map[string]int{},
	}
	log := &fakeWriteLog{}
	restorer, slept := testRestorer(client, NewPreviewStateStore(t.TempDir(), "/sock"), log, 10*time.Millisecond, 20*time.Millisecond)

	reports := restorer.Run()

	if len(reports) != 1 || reports[0].Outcome != RestoreTimeout || !strings.Contains(reports[0].Reason, "not ready within") {
		t.Fatalf("reports = %v, want a timeout with reason", reports)
	}
	// The timeout is a user-visible outcome and must reach the hook log.
	found := false
	for _, line := range log.lines {
		if strings.Contains(line, "result=timeout") && strings.Contains(line, "wA:p1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("log = %v, want a timeout line for wA:p1", log.lines)
	}
	if len(client.runCommands) != 0 {
		t.Fatalf("run commands = %v, want the timed-out pane untouched", client.runCommands)
	}
	if len(*slept) != 2 {
		t.Fatalf("sleeps = %v, want two poll rounds", *slept)
	}
}

func TestRestoreProcessInfoFailureSkipsPaneWithoutRetry(t *testing.T) {
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Files", CWD: "/a"},
		},
		processErr: map[string]error{"wA:p1": errors.New("socket broke")},
		processSeq: map[string]int{},
	}
	log := &fakeWriteLog{}
	restorer, _ := testRestorer(client, NewPreviewStateStore(t.TempDir(), "/sock"), log, 10*time.Millisecond, 30*time.Millisecond)

	reports := restorer.Run()

	if len(reports) != 1 || reports[0].Outcome != RestoreSkipped || !strings.Contains(reports[0].Reason, "process-info failed") {
		t.Fatalf("reports = %v, want skipped with the process-info failure reason", reports)
	}
	if client.processCalls("wA:p1") != 1 {
		t.Fatalf("process-info calls = %d, want exactly one (no retry after failure)", client.processCalls("wA:p1"))
	}
}

func TestRestoreWorkspaceListFailureStopsThePass(t *testing.T) {
	client := &fakeRestoreClient{listWorkspacesErr: errors.New("daemon down")}
	log := &fakeWriteLog{}
	restorer, _ := testRestorer(client, NewPreviewStateStore(t.TempDir(), "/sock"), log, time.Millisecond, time.Millisecond)

	reports := restorer.Run()

	if len(reports) != 0 {
		t.Fatalf("reports = %v, want none after a workspace list failure", reports)
	}
	if len(log.lines) != 1 || !strings.Contains(log.lines[0], "workspace list failed") {
		t.Fatalf("log = %v, want the workspace list failure reason", log.lines)
	}
}

func TestRestoreSkipsStaleCleanupWhenPaneListingIsPartial(t *testing.T) {
	store := NewPreviewStateStore(t.TempDir(), "/sock")
	if err := store.Save("wB:p9", "/gone/file.md"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	client := &fakeRestoreClient{
		workspaces:   []Workspace{{WorkspaceID: "wA"}, {WorkspaceID: "wB"}},
		panesByID:    map[string]PaneInfo{},
		listPanesErr: errors.New("workspace wB unreachable"),
		processSeq:   map[string]int{},
	}
	restorer, _ := testRestorer(client, store, &fakeWriteLog{}, time.Millisecond, time.Millisecond)

	restorer.Run()

	if _, found, err := store.Load("wB:p9"); err != nil || !found {
		t.Fatalf("state after partial listing = found %v, err %v; want kept (cleanup must see the full pane picture)", found, err)
	}
}

func TestRestoreRemovesStaleStateOfMissingPanesOnly(t *testing.T) {
	store := NewPreviewStateStore(t.TempDir(), "/sock")
	if err := store.Save("wA:p1", "/keep.md"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Save("wA:p2", "/gone.md"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			// wA:p2 exists but was relabeled; only the truly missing pane is
			// cleaned.
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Preview"},
			"wA:p2": {PaneID: "wA:p2", WorkspaceID: "wA", Label: "renamed"},
		},
		processSeq: map[string]int{},
	}
	log := &fakeWriteLog{}
	restorer, _ := testRestorer(client, store, log, time.Millisecond, time.Millisecond)

	restorer.Run()

	if _, found, err := store.Load("wA:p1"); err != nil || !found {
		t.Fatalf("existing pane state = found %v, err %v; want kept", found, err)
	}
	if _, found, err := store.Load("wA:p2"); err != nil || !found {
		t.Fatalf("relabeled pane state = found %v, err %v; want kept", found, err)
	}
}

func TestRestorePreviewSkipsMissingAndCorruptState(t *testing.T) {
	stateDir := t.TempDir()
	store := NewPreviewStateStore(stateDir, "/sock")
	corruptPath := filepath.Join(stateDir, "preview", previewStateNamespace("/sock"), "wA:p2.json")
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Preview"},
			"wA:p2": {PaneID: "wA:p2", WorkspaceID: "wA", Label: "Preview"},
		},
		processInfo: map[string]PaneProcessInfo{"wA:p1": bashPane(), "wA:p2": bashPane()},
		processSeq:  map[string]int{},
	}
	log := &fakeWriteLog{}
	restorer, _ := testRestorer(client, store, log, time.Millisecond, time.Millisecond)

	reports := restorer.Run()

	if len(reports) != 2 {
		t.Fatalf("reports = %v, want both preview candidates handled", reports)
	}
	byPane := map[string]RestoreReport{}
	for _, report := range reports {
		byPane[report.PaneID] = report
	}
	if byPane["wA:p1"].Outcome != RestoreSkipped || !strings.Contains(byPane["wA:p1"].Reason, "no preview state") {
		t.Fatalf("missing-state preview = %v, want skipped without an empty preview", byPane["wA:p1"])
	}
	if byPane["wA:p2"].Outcome != RestoreSkipped || !strings.Contains(byPane["wA:p2"].Reason, "corrupt") {
		t.Fatalf("corrupt-state preview = %v, want skipped with the corrupt reason", byPane["wA:p2"])
	}
	if len(client.runCommands) != 0 {
		t.Fatalf("run commands = %v, want no preview restored without usable state", client.runCommands)
	}
}

func TestShellQuoteRoundTripsThroughTheRestoredShell(t *testing.T) {
	for _, value := range []string{
		"plain",
		"/work space/root",
		"/quote'inside/path",
		"/eq=uals/path",
		"/mixed space'quote=equal/path",
		`{"focused_pane_cwd":"/work space"}`,
		"~not-expanded",
		"/tab\tvalue",
		"/dollar$and\\backslash",
		"",
	} {
		t.Run(value, func(t *testing.T) {
			quoted := shellQuote(value)
			out, err := exec.Command("/bin/sh", "-c", "printf %s "+quoted).Output()
			if err != nil {
				t.Fatalf("shell eval of %q failed: %v", quoted, err)
			}
			if string(out) != value {
				t.Fatalf("shellQuote(%q) = %q evaluated to %q", value, quoted, out)
			}
		})
	}
}

func TestShellQuoteKeepsSafeWordsBare(t *testing.T) {
	for _, value := range []string{"wY:p1a", "/a=b/c", "/x-y.z", "herdr-file-viewer", "HERDR_ENV=1"} {
		if got := shellQuote(value); got != value {
			t.Fatalf("shellQuote(%q) = %q, want the bare word", value, got)
		}
	}
}

func TestRestoreCommandSurvivesUnsafePathsAsOnePaneRunArgument(t *testing.T) {
	restorer, _ := testRestorer(&fakeRestoreClient{}, NewPreviewStateStore(t.TempDir(), "/sock"), nil, time.Millisecond, time.Millisecond)
	pane := PaneInfo{PaneID: "wA:p1", WorkspaceID: "wA", Label: "Files", CWD: "/root with space'quote=equal"}

	command := restorer.buildRestoreCommand(pane, filesEntrypointID, "")

	if !strings.HasPrefix(command, "exec env ") {
		t.Fatalf("command = %q, want the exec env prefix", command)
	}
	if len(strings.Fields(command)) < 2 {
		t.Fatalf("command = %q, want more than one shell word", command)
	}
	// The trailing binary is shell-quoted; evaluate it with the shell the
	// pane run submission goes through.
	binary := restorer.viewerBinary()
	quotedBinary := shellQuote(binary)
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+quotedBinary).Output()
	if err != nil || string(out) != binary {
		t.Fatalf("binary %q evaluated to %q, err %v", quotedBinary, out, err)
	}
	// Every env value must survive shell parsing; extract the assignment
	// list (safe binary keeps the suffix removable) and eval it the way the
	// restored shell would.
	if strings.ContainsAny("/home/u7dev/workspace/herdr-file-viewer/bin/herdr-file-viewer", " \t'\"") {
		t.Fatal("test binary path must be shell-safe for the extraction below")
	}
	assignments := strings.TrimSuffix(strings.TrimPrefix(command, "exec env "), " "+binary)
	out, err = exec.Command("/bin/sh", "-c",
		`eval "$1"; printf '%s\n' "$HERDR_PLUGIN_CONTEXT_JSON" "$HERDR_PLUGIN_ENTRYPOINT_ID" "$HERDR_PANE_ID"`,
		"sh", assignments).Output()
	if err != nil {
		t.Fatalf("eval of %q failed: %v", assignments, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	wantJSON := `{"focused_pane_cwd":"/root with space'quote=equal"}`
	if len(lines) != 3 || lines[0] != wantJSON || lines[1] != "files" || lines[2] != "wA:p1" {
		t.Fatalf("eval = %q, want cwd %q, entrypoint files, pane wA:p1", lines, wantJSON)
	}
}

func TestRestorePreviewCommandCarriesTheSavedFile(t *testing.T) {
	restorer, _ := testRestorer(&fakeRestoreClient{}, NewPreviewStateStore(t.TempDir(), "/sock"), nil, time.Millisecond, time.Millisecond)
	pane := PaneInfo{PaneID: "wB:p2", WorkspaceID: "wB", Label: "Preview"}

	command := restorer.buildRestoreCommand(pane, PreviewEntrypointID, "/pre view/file'=x.md")

	if !strings.Contains(command, "HERDR_PLUGIN_ENTRYPOINT_ID=preview") {
		t.Fatalf("command = %q, want the preview entrypoint", command)
	}
	if !strings.Contains(command, "HERDR_PREVIEW_FILE=") {
		t.Fatalf("command = %q, want the preview file env", command)
	}
	if strings.Contains(command, "HERDR_PLUGIN_CONTEXT_JSON=") {
		t.Fatalf("command = %q, want no context env for previews", command)
	}
	// The env assignments must eval to the original file value.
	binary := restorer.viewerBinary()
	assignments := strings.TrimSuffix(strings.TrimPrefix(command, "exec env "), " "+binary)
	out, err := exec.Command("/bin/sh", "-c",
		`eval "$1"; printf '%s' "$HERDR_PREVIEW_FILE"`, "sh", assignments).Output()
	if err != nil || string(out) != "/pre view/file'=x.md" {
		t.Fatalf("preview file eval = %q, err %v; want the untouched path", out, err)
	}
}

func TestRestoreReporterReturnsOutcomesForPendingTimeoutFirst(t *testing.T) {
	client := &fakeRestoreClient{
		workspaces: []Workspace{{WorkspaceID: "wA"}},
		panesByID: map[string]PaneInfo{
			"wA:p1": {PaneID: "wA:p1", WorkspaceID: "wA", Label: "Files", CWD: "/a"},
			"wA:p2": {PaneID: "wA:p2", WorkspaceID: "wA", Label: "Files", CWD: "/b"},
		},
		processInfo: map[string]PaneProcessInfo{"wA:p1": bashPane(), "wA:p2": {}},
		processSeq:  map[string]int{},
	}
	restorer, _ := testRestorer(client, NewPreviewStateStore(t.TempDir(), "/sock"), nil, time.Millisecond, time.Millisecond)

	reports := restorer.Run()

	if len(reports) != 2 || reports[0].PaneID != "wA:p1" || reports[0].Outcome != RestoreRestored ||
		reports[1].PaneID != "wA:p2" || reports[1].Outcome != RestoreTimeout {
		t.Fatalf("reports = %v, want restored first, then the timeout", reports)
	}
}

func TestRestoreRecognizesTruncatedViewerProcessNames(t *testing.T) {
	for _, name := range []string{"herdr-file-viewer", "herdr-file-view", "herdr-file-vie"} {
		if !isViewerProcessName(name) {
			t.Fatalf("isViewerProcessName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"herdr", "herdr-file", "herdr-file-v", "bash", ""} {
		if isViewerProcessName(name) {
			t.Fatalf("isViewerProcessName(%q) = true, want false", name)
		}
	}
}
