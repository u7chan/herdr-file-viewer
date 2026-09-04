package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// RestoreOutcome classifies one candidate pane after the startup restore
// pass, for the hook log and for deterministic tests.
type RestoreOutcome string

const (
	// RestoreRestored: the pane now runs the viewer.
	RestoreRestored RestoreOutcome = "restored"
	// RestoreAlready: the viewer is already running in the pane.
	RestoreAlready RestoreOutcome = "already"
	// RestoreSkipped: the pane was left untouched for a stated reason.
	RestoreSkipped RestoreOutcome = "skipped"
	// RestoreTimeout: the pane never became ready within the polling window.
	RestoreTimeout RestoreOutcome = "timeout"
)

// RestoreReport is the per-pane outcome of one startup restore pass.
type RestoreReport struct {
	PaneID  string
	Label   string
	Outcome RestoreOutcome
	Reason  string
}

// RestoreLaunchEnv is the plugin runtime environment of the startup hook,
// passed on to the viewer processes that replace the restored shells. The
// hook's own HERDR_PLUGIN_EVENT is deliberately not part of it: the restored
// processes must start the TUI, not run the restore again.
type RestoreLaunchEnv struct {
	PluginRoot      string
	PluginConfigDir string
	PluginStateDir  string
	SocketPath      string
	BinPath         string
	// Executable is the absolute path of the running viewer binary.
	Executable string
}

// RestoreConfig wires the startup restore with its I/O boundaries. The
// defaults (1 second polling, 30 second window) apply when Poll or Timeout
// is left at zero; Sleep is injectable so tests never wait on real time.
type RestoreConfig struct {
	Client RestoreClient
	State  *PreviewStateStore
	Log    io.Writer
	Env    RestoreLaunchEnv
	Poll   time.Duration
	// Timeout bounds the pending re-check window.
	Timeout time.Duration
	Sleep   func(time.Duration)
}

// RestoreClient is the Herdr CLI surface the startup restore needs. It is a
// read-mostly surface: workspace and pane listings, process classification,
// and the single `pane run` substitution that replaces a shell in place.
type RestoreClient interface {
	ListWorkspaces() ([]Workspace, error)
	ListPanes(workspaceID string) ([]PaneInfo, error)
	ProcessInfo(paneID string) (PaneProcessInfo, error)
	RunCommand(paneID, command string) error
}

const (
	filesLabel   = "Files"
	previewLabel = "Preview"
	// filesEntrypointID is the manifest pane id of the tree entrypoint.
	filesEntrypointID = "files"
	// viewerProcessName is the binary basename used to recognize a running
	// viewer. Linux truncates comm to 15 chars, so the process name may be
	// a prefix of this value; argv[0] (not truncated) carries the full name.
	viewerProcessName = "herdr-file-viewer"
	// restorePollDefault and restoreTimeoutDefault bound the pending
	// re-check window: one re-classification per second for about 30
	// seconds, matching the shell-spawn race of a cold session restore.
	restorePollDefault    = time.Second
	restoreTimeoutDefault = 30 * time.Second
)

// Restorer restores File Viewer and Preview panes into their restored
// shells during the plugin startup hook. It enumerates every workspace,
// targets only panes whose label is exactly "Files" or "Preview", and
// replaces their shell process in place with the viewer binary, so pane ID,
// tab, layout, and focus survive. No pane is created, closed, moved, or
// focused by the restore.
type Restorer struct {
	cfg RestoreConfig
}

// NewRestorer wires the restore with its dependencies.
func NewRestorer(cfg RestoreConfig) *Restorer {
	return &Restorer{cfg: cfg}
}

// Run performs one restore pass and returns the per-pane outcomes in
// deterministic order: candidates in enumeration order, timeouts after the
// ready/already/skipped outcomes.
func (r *Restorer) Run() []RestoreReport {
	workspaces, err := r.cfg.Client.ListWorkspaces()
	if err != nil {
		r.logf("workspace list failed: %v; nothing restored", err)
		return nil
	}

	panes := make([]PaneInfo, 0)
	listingComplete := true
	for _, workspace := range workspaces {
		workspacePanes, err := r.cfg.Client.ListPanes(workspace.WorkspaceID)
		if err != nil {
			listingComplete = false
			r.logf("pane list failed for workspace %s: %v; its panes stay untouched", workspace.WorkspaceID, err)
			continue
		}
		panes = append(panes, workspacePanes...)
	}
	// Stale-state cleanup needs the complete pane picture: a partial listing
	// must never delete state files of panes it could not see.
	if listingComplete {
		r.cleanStalePreviewState(panes)
	}

	candidates := make([]PaneInfo, 0, len(panes))
	for _, pane := range panes {
		if pane.Label == filesLabel || pane.Label == previewLabel {
			candidates = append(candidates, pane)
		}
	}

	reports := make([]RestoreReport, 0, len(candidates))
	pending := make(map[string]PaneInfo)
	for _, pane := range candidates {
		if report := r.settle(pane); report != nil {
			reports = append(reports, *report)
		} else {
			pending[pane.PaneID] = pane
		}
	}

	rounds := r.maxPollRounds()
	for len(pending) > 0 && rounds > 0 {
		r.sleep(r.pollInterval())
		rounds--
		var settled []string
		for paneID, pane := range pending {
			if report := r.settle(pane); report != nil {
				reports = append(reports, *report)
				settled = append(settled, paneID)
			}
		}
		for _, paneID := range settled {
			delete(pending, paneID)
		}
	}
	for paneID := range pending {
		pane := pending[paneID]
		reports = append(reports, *r.report(pane, RestoreTimeout, fmt.Sprintf("foreground not ready within %s", r.timeout())))
	}
	return reports
}

// settle classifies one candidate pane and restores it when it is ready.
// It returns nil while the pane stays pending for a later poll round.
func (r *Restorer) settle(pane PaneInfo) *RestoreReport {
	info, err := r.cfg.Client.ProcessInfo(pane.PaneID)
	if err != nil {
		return r.report(pane, RestoreSkipped, "process-info failed: "+err.Error())
	}
	switch classifyProcess(info) {
	case processAlready:
		return r.report(pane, RestoreAlready, "viewer already running")
	case processPending:
		return nil
	default:
		if err := r.restore(pane); err != nil {
			return r.report(pane, RestoreSkipped, err.Error())
		}
		return r.report(pane, RestoreRestored, "")
	}
}

func (r *Restorer) report(pane PaneInfo, outcome RestoreOutcome, reason string) *RestoreReport {
	report := &RestoreReport{PaneID: pane.PaneID, Label: pane.Label, Outcome: outcome, Reason: reason}
	line := fmt.Sprintf("startup restore: pane=%s label=%s result=%s", pane.PaneID, pane.Label, outcome)
	if reason != "" {
		line += " reason=" + reason
	}
	r.logf("%s", line)
	return report
}

// restore execs the viewer into the candidate pane, keeping the pane in
// place. Files panes get a context built from their own saved cwd; Preview
// panes get their saved preview file, or a skip when no usable state exists.
func (r *Restorer) restore(pane PaneInfo) error {
	kind, previewFile, err := r.restoreTarget(pane)
	if err != nil {
		return err
	}
	command := r.buildRestoreCommand(pane, kind, previewFile)
	return r.cfg.Client.RunCommand(pane.PaneID, command)
}

// restoreTarget decides what the candidate pane must run. Preview panes
// need their saved preview file and are skipped when the state is missing
// or corrupt, so a shell without knowledge of its preview never turns into
// an empty preview.
func (r *Restorer) restoreTarget(pane PaneInfo) (kind, previewFile string, err error) {
	if pane.Label == previewLabel {
		file, found, err := r.cfg.State.Load(pane.PaneID)
		if err != nil {
			return "", "", fmt.Errorf("preview state is corrupt: %v", err)
		}
		if !found {
			return "", "", errNoPreviewState
		}
		return PreviewEntrypointID, file, nil
	}
	return filesEntrypointID, "", nil
}

// cleanStalePreviewState deletes restore-state files of panes that no longer
// exist in any workspace. Renamed or relabeled panes keep their state; only
// truly gone panes are cleaned, best-effort.
func (r *Restorer) cleanStalePreviewState(panes []PaneInfo) {
	existing := make(map[string]bool, len(panes))
	for _, pane := range panes {
		existing[pane.PaneID] = true
	}
	ids, err := r.cfg.State.ListPaneIDs()
	if err != nil {
		r.logf("preview state list failed: %v; stale state kept", err)
		return
	}
	for _, paneID := range ids {
		if existing[paneID] {
			continue
		}
		if err := r.cfg.State.Remove(paneID); err != nil {
			r.logf("stale preview state removal failed for pane %s: %v", paneID, err)
		} else {
			r.logf("removed stale preview state of missing pane %s", paneID)
		}
	}
}

func (r *Restorer) logf(format string, args ...any) {
	if r.cfg.Log != nil {
		// Logging is best-effort: a failed write must not abort the restore.
		_, _ = fmt.Fprintf(r.cfg.Log, format+"\n", args...)
	}
}

func (r *Restorer) pollInterval() time.Duration {
	if r.cfg.Poll > 0 {
		return r.cfg.Poll
	}
	return restorePollDefault
}

func (r *Restorer) timeout() time.Duration {
	if r.cfg.Timeout > 0 {
		return r.cfg.Timeout
	}
	return restoreTimeoutDefault
}

func (r *Restorer) maxPollRounds() int {
	rounds := int(r.timeout() / r.pollInterval())
	if rounds < 1 {
		rounds = 1
	}
	return rounds
}

func (r *Restorer) sleep(duration time.Duration) {
	if r.cfg.Sleep != nil {
		r.cfg.Sleep(duration)
	} else {
		time.Sleep(duration)
	}
}

// processCategory is the classification of one `pane process-info` snapshot.
type processCategory int

const (
	// processReady: every foreground process is a known interactive shell.
	processReady processCategory = iota
	// processAlready: the viewer binary is already running in the pane.
	processAlready
	// processPending: the foreground is empty, unknown, or not a known
	// shell; re-check on the next poll round.
	processPending
)

// classifyProcess maps a process-info snapshot to the restore decision. A
// running viewer always wins, so live handoff never double-launches; a pane
// with any unknown foreground process (a running command, a prompt helper)
// stays pending and is never touched while it works.
func classifyProcess(info PaneProcessInfo) processCategory {
	if len(info.ForegroundProcesses) == 0 {
		return processPending
	}
	for _, process := range info.ForegroundProcesses {
		name := processBasename(process)
		if isViewerProcessName(name) {
			return processAlready
		}
		if !isKnownShellName(name) {
			return processPending
		}
	}
	return processReady
}

// processBasename resolves the display name of one foreground process:
// argv[0] is the full executable path (never truncated) and wins over the
// process name field, which Linux truncates to 15 chars.
func processBasename(process PaneProcess) string {
	if len(process.Argv) > 0 && strings.TrimSpace(process.Argv[0]) != "" {
		return strings.TrimPrefix(filepath.Base(process.Argv[0]), "-")
	}
	return strings.TrimPrefix(filepath.Base(process.Name), "-")
}

// isKnownShellName reports whether the process is one of the idle shells a
// restored pane can own; only these are replaced with the viewer.
func isKnownShellName(name string) bool {
	switch name {
	case "bash", "zsh", "sh", "fish", "dash", "ksh":
		return true
	}
	return false
}

// isViewerProcessName recognizes the viewer binary by name. The exact
// basename matches the full argv[0]; a shorter observed name is accepted
// when it is a long prefix of the expected name, which is how Linux truncates
// comm. Short prefixes are refused so an unrelated process cannot mask a
// restore.
func isViewerProcessName(name string) bool {
	if name == viewerProcessName {
		return true
	}
	// Linux comm is truncated to 15 chars, so genuine truncations of the
	// 17-char name are 15 chars long ("herdr-file-view"); the 13-char bound
	// covers every plausible truncation while refusing short prefixes that
	// unrelated processes could share.
	return len(name) >= 13 && strings.HasPrefix(viewerProcessName, name)
}

// shellQuote quotes one value as a single POSIX shell word. Everything
// outside the safe set is wrapped in single quotes with embedded quotes
// escaped, so values containing spaces, single quotes, or equals signs
// survive the restored shell unchanged.
func shellQuote(value string) string {
	if value != "" && isShellSafeWord(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// isShellSafeWord reports whether value needs no quoting as a shell word.
// The safe set covers the characters that appear in paths and pane ids and
// are never special mid-word; everything else (spaces, quotes, braces,
// dollars, backslashes, globs, history bang) forces quoting.
func isShellSafeWord(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '@', '%', '+', '=', ':', ',', '.', '/', '-':
			continue
		}
		return false
	}
	return true
}

// shellJoinEnv joins KEY=VALUE assignments into one shell-safe word list.
func shellJoinEnv(env []string) string {
	quoted := make([]string, 0, len(env))
	for _, assignment := range env {
		key, value, ok := strings.Cut(assignment, "=")
		if !ok {
			continue
		}
		quoted = append(quoted, key+"="+shellQuote(value))
	}
	return strings.Join(quoted, " ")
}

// buildRestoreCommand builds the one-shot command that replaces a restored
// shell with the viewer: `exec env` keeps the pane's terminal and pid, and
// the explicit environment leaves no HERDR_PLUGIN_* variable behind in the
// shell afterwards. Files panes carry a context JSON built from their own
// saved cwd; Preview panes carry their saved preview file.
func (r *Restorer) buildRestoreCommand(pane PaneInfo, kind, previewFile string) string {
	env := []string{
		EntrypointIDEnv + "=" + kind,
		PluginRootEnv + "=" + r.cfg.Env.PluginRoot,
		PluginConfigDirEnv + "=" + r.cfg.Env.PluginConfigDir,
		PluginStateDirEnv + "=" + r.cfg.Env.PluginStateDir,
		PaneIDEnv + "=" + pane.PaneID,
		WorkspaceIDEnv + "=" + pane.WorkspaceID,
		BinPathEnv + "=" + r.cfg.Env.BinPath,
		SocketPathEnv + "=" + r.cfg.Env.SocketPath,
		"HERDR_ENV=1",
	}
	if kind == filesEntrypointID && pane.CWD != "" {
		env = append(env, contextJSONEnv+"="+paneContextJSON(pane.CWD))
	}
	if kind == PreviewEntrypointID && previewFile != "" {
		env = append(env, PreviewFileEnv+"="+previewFile)
	}
	return "exec env " + shellJoinEnv(env) + " " + shellQuote(r.viewerBinary())
}

// paneContextJSON JSON-encodes the pane-specific startup context. The
// restored shell has no HERDR_PLUGIN_CONTEXT_JSON of its own, and the hook's
// own focused-pane context must never be reused for other panes, so the
// context is built per pane from its saved cwd and carries only that cwd.
func paneContextJSON(cwd string) string {
	content, err := json.Marshal(struct {
		FocusedPaneCWD string `json:"focused_pane_cwd"`
	}{FocusedPaneCWD: cwd})
	if err != nil {
		return "{}"
	}
	return string(content)
}

func (r *Restorer) viewerBinary() string {
	if r.cfg.Env.Executable != "" {
		return r.cfg.Env.Executable
	}
	return filepath.Join(r.cfg.Env.PluginRoot, "bin", viewerProcessName)
}

// errNoPreviewState marks a Preview candidate that has no usable saved
// state: restoring it would open an empty preview, so the shell stays.
var errNoPreviewState = errors.New("no preview state to restore")
