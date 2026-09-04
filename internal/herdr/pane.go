package herdr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Herdr environment variables the viewer consumes. The plugin pane process
// receives these from Herdr at launch; the startup restore passes them on to
// the processes it execs into restored panes.
const (
	BinPathEnv         = "HERDR_BIN_PATH"
	EntrypointIDEnv    = "HERDR_PLUGIN_ENTRYPOINT_ID"
	PaneIDEnv          = "HERDR_PANE_ID"
	WorkspaceIDEnv     = "HERDR_WORKSPACE_ID"
	PreviewFileEnv     = "HERDR_PREVIEW_FILE"
	PluginRootEnv      = "HERDR_PLUGIN_ROOT"
	PluginConfigDirEnv = "HERDR_PLUGIN_CONFIG_DIR"
	PluginStateDirEnv  = "HERDR_PLUGIN_STATE_DIR"
	PluginEventEnv     = "HERDR_PLUGIN_EVENT"
	SocketPathEnv      = "HERDR_SOCKET_PATH"
	// HelpContextEnv tells the help entrypoint which caller opened it.
	HelpContextEnv = "HERDR_HELP_CONTEXT"
	// PreviewEntrypointID identifies the preview pane entrypoint.
	PreviewEntrypointID = "preview"
	// HelpEntrypointID identifies the popup help pane entrypoint.
	HelpEntrypointID = "help"
	paneNotFoundCode      = "pane_not_found"
	pluginPaneNotFoundCode = "plugin_pane_not_found"
)

// IsStartupEvent reports whether this process is the plugin startup hook
// (`HERDR_PLUGIN_EVENT=startup`). The startup hook runs once per enabled
// plugin after the session restore and must restore panes without starting
// the TUI.
func IsStartupEvent() bool {
	return os.Getenv(PluginEventEnv) == "startup"
}

// HelpContext returns the caller context passed through HelpContextEnv for
// the help entrypoint: "tree" or "preview". A missing or unknown value
// falls back to "tree".
func HelpContext() string {
	switch os.Getenv(HelpContextEnv) {
	case "preview":
		return "preview"
	default:
		return "tree"
	}
}

// EntrypointID returns HERDR_PLUGIN_ENTRYPOINT_ID, the pane entrypoint that
// launched this process.
func EntrypointID() string {
	return os.Getenv(EntrypointIDEnv)
}

// IsPreviewEntrypoint reports whether this process runs the preview pane.
func IsPreviewEntrypoint() bool {
	return EntrypointID() == PreviewEntrypointID
}

// IsHelpEntrypoint reports whether this process runs the help popup pane.
func IsHelpEntrypoint() bool {
	return EntrypointID() == HelpEntrypointID
}

// PaneID returns HERDR_PANE_ID, the caller's own pane identity.
func PaneID() string {
	return os.Getenv(PaneIDEnv)
}

// WorkspaceID returns HERDR_WORKSPACE_ID, the workspace the caller runs in.
func WorkspaceID() string {
	return os.Getenv(WorkspaceIDEnv)
}

// PreviewFile returns HERDR_PREVIEW_FILE, the file the preview pane must
// open, or "" when the environment did not supply one.
func PreviewFile() string {
	return os.Getenv(PreviewFileEnv)
}

// ErrPaneNotFound reports that the requested pane does not exist (or no
// longer exists) from the Herdr daemon's point of view.
var ErrPaneNotFound = errors.New("pane not found")

// ErrPluginPaneNotFound reports that the requested pane is not owned by any
// plugin. Panes restored into plain shells during the startup restore lose
// their plugin ownership and answer plugin commands with this error.
var ErrPluginPaneNotFound = errors.New("plugin pane not found")

// OpenPaneRequest describes one `herdr plugin pane open` invocation.
type OpenPaneRequest struct {
	Plugin     string
	Entrypoint string
	Placement  string
	TargetPane string
	Direction  string
	// Focus requests the keyboard focus move to the opened pane. The zero
	// value keeps the focus in the caller's pane.
	Focus bool
	Env   []string // KEY=VALUE pairs propagated to the launched process
}

// ReportMetadataRequest describes one `herdr pane report-metadata`
// invocation.
type ReportMetadataRequest struct {
	PaneID string
	Source string
	Tokens []string // NAME=VALUE pairs
}

// PaneInfo is the pane data the viewer needs from the daemon. Label and CWD
// are the panes' manual label and saved working directory, which only exist
// when Herdr reports them. Tokens are the metadata tokens reported by the
// pane's process; a pane without metadata has an empty (nil) map.
type PaneInfo struct {
	PaneID      string
	WorkspaceID string
	TabID       string
	Label       string
	CWD         string
	Tokens      map[string]string
}

// Workspace is the workspace data the startup restore needs: the stable
// workspace ID used to scope pane listings.
type Workspace struct {
	WorkspaceID string
	Label       string
}

// PaneProcess is one foreground process of a pane as reported by
// `pane process-info`. Name is the process name (comm, potentially truncated
// to 15 chars on Linux); Argv is the process argument vector when Herdr could
// read it.
type PaneProcess struct {
	Name string
	Argv []string
}

// PaneProcessInfo is the `pane process-info` data the startup restore uses to
// decide whether a restored shell is idle, already runs the viewer, or still
// needs time.
type PaneProcessInfo struct {
	PaneID              string
	ForegroundProcesses []PaneProcess
}

// PaneClient is the Herdr pane CLI surface used by the viewer. It is an
// interface so tests and the composition root can replace the subprocess
// boundary.
type PaneClient interface {
	// OpenPane opens a plugin pane and returns its pane ID.
	OpenPane(request OpenPaneRequest) (string, error)
	// ClosePane closes a pane. Closing an already-missing pane succeeds.
	ClosePane(paneID string) error
	// ClosePreviewPane closes a preview pane with the plugin-close primary
	// path and the plain pane-close fallback for panes that lost plugin
	// ownership during a session restore.
	ClosePreviewPane(paneID string) error
	// GetPane reports whether the pane exists, and its data when it does.
	GetPane(paneID string) (PaneInfo, bool, error)
	// ListPanes lists the panes of one workspace, or all panes when
	// workspaceID is empty.
	ListPanes(workspaceID string) ([]PaneInfo, error)
	// ReportMetadata reports metadata tokens for the caller's pane.
	ReportMetadata(request ReportMetadataRequest) error
}

// runner executes one herdr CLI invocation. The split is injectable so tests
// can assert argument construction and canned JSON responses without a
// daemon.
type runner interface {
	Run(args ...string) (stdout, stderr string, err error)
}

type execRunner struct {
	binary string
}

func (r execRunner) Run(args ...string) (string, string, error) {
	command := exec.Command(r.binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

// CLIPaneClient drives the herdr binary. Success output arrives on stdout as
// a JSON envelope; failures arrive on stderr with a non-zero exit status.
type CLIPaneClient struct {
	runner runner
}

// NewCLIPaneClient builds a client that runs the herdr binary resolved from
// HERDR_BIN_PATH, falling back to the PATH lookup.
func NewCLIPaneClient() *CLIPaneClient {
	return &CLIPaneClient{runner: execRunner{binary: resolvePaneBinary(os.LookupEnv)}}
}

// resolvePaneBinary prefers HERDR_BIN_PATH; without it, exec.Command falls
// back to a PATH lookup of "herdr".
func resolvePaneBinary(lookupEnv func(string) (string, bool)) string {
	if path, ok := lookupEnv(BinPathEnv); ok && strings.TrimSpace(path) != "" {
		return path
	}
	return "herdr"
}

// OpenPane runs `herdr plugin pane open`. Focused overlays pass --focus;
// every other pane keeps the keyboard focus in the caller's pane through
// --no-focus. The placement flag is omitted when the request leaves it
// empty, letting the manifest's pane declaration (overlay, popup, split,
// or zoomed and, for popups, the fixed size) take effect; a non-empty
// placement is passed through explicitly. The target flag is included only
// when a target pane is given: overlay and popup placements always target
// the active pane and reject an explicit target, while split and zoomed
// placements require one. The direction flag is omitted when the placement
// does not use one (overlay).
func (c *CLIPaneClient) OpenPane(request OpenPaneRequest) (string, error) {
	args := []string{
		"plugin", "pane", "open",
		"--plugin", request.Plugin,
		"--entrypoint", request.Entrypoint,
	}
	if request.Placement != "" {
		args = append(args, "--placement", request.Placement)
	}
	if request.TargetPane != "" {
		args = append(args, "--target-pane", request.TargetPane)
	}
	if request.Direction != "" {
		args = append(args, "--direction", request.Direction)
	}
	if request.Focus {
		args = append(args, "--focus")
	} else {
		args = append(args, "--no-focus")
	}
	for _, env := range request.Env {
		args = append(args, "--env", env)
	}

	var response struct {
		Type       string `json:"type"`
		PluginPane struct {
			Pane struct {
				PaneID string `json:"pane_id"`
			} `json:"pane"`
		} `json:"plugin_pane"`
	}
	if err := c.runCLI(args, &response); err != nil {
		return "", err
	}
	if response.PluginPane.Pane.PaneID == "" {
		// popup placements answer with a bare ok envelope and no pane id:
		// the popup owns no tracked pane. Every other placement must
		// report the opened pane, so a missing id stays an error there.
		if response.Type == "ok" {
			return "", nil
		}
		return "", fmt.Errorf("open response contains no pane_id")
	}
	return response.PluginPane.Pane.PaneID, nil
}

// ClosePane runs `herdr plugin pane close`. A pane that already disappeared
// (pane_not_found) counts as closed.
func (c *CLIPaneClient) ClosePane(paneID string) error {
	args := []string{"plugin", "pane", "close", paneID}
	if err := c.runCLI(args, nil); err != nil && !errors.Is(err, ErrPaneNotFound) {
		return err
	}
	return nil
}

// GetPane runs `herdr pane get`. pane_not_found maps to found=false with no
// error; every other failure is an error.
func (c *CLIPaneClient) GetPane(paneID string) (PaneInfo, bool, error) {
	var response struct {
		Pane *paneJSON `json:"pane"`
	}
	if err := c.runCLI([]string{"pane", "get", paneID}, &response); err != nil {
		if errors.Is(err, ErrPaneNotFound) {
			return PaneInfo{}, false, nil
		}
		return PaneInfo{}, false, err
	}
	if response.Pane == nil {
		return PaneInfo{}, false, fmt.Errorf("get response contains no pane")
	}
	info := response.Pane.info()
	if info.PaneID == "" {
		return PaneInfo{}, false, fmt.Errorf("get response contains no pane_id")
	}
	return info, true, nil
}

// paneJSON mirrors the pane object of `pane get` / `pane list`. Every field
// beyond the ids is optional: Herdr omits label, cwd, and tokens when they
// are unset.
type paneJSON struct {
	PaneID      string            `json:"pane_id"`
	WorkspaceID string            `json:"workspace_id"`
	TabID       string            `json:"tab_id"`
	Label       string            `json:"label"`
	CWD         string            `json:"cwd"`
	Tokens      map[string]string `json:"tokens"`
}

func (p paneJSON) info() PaneInfo {
	return PaneInfo(p)
}

// ListPanes runs `herdr pane list`, scoped to one workspace when given.
func (c *CLIPaneClient) ListPanes(workspaceID string) ([]PaneInfo, error) {
	args := []string{"pane", "list"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}

	var response struct {
		Panes []paneJSON `json:"panes"`
	}
	if err := c.runCLI(args, &response); err != nil {
		return nil, err
	}
	panes := make([]PaneInfo, 0, len(response.Panes))
	for _, pane := range response.Panes {
		panes = append(panes, pane.info())
	}
	return panes, nil
}

// ListWorkspaces runs `herdr workspace list` and returns every workspace of
// the session. Workspace IDs are the stable handles the restore uses to
// scope pane listings to each space.
func (c *CLIPaneClient) ListWorkspaces() ([]Workspace, error) {
	var response struct {
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
		} `json:"workspaces"`
	}
	if err := c.runCLI([]string{"workspace", "list"}, &response); err != nil {
		return nil, err
	}
	workspaces := make([]Workspace, 0, len(response.Workspaces))
	for _, workspace := range response.Workspaces {
		workspaces = append(workspaces, Workspace{
			WorkspaceID: workspace.WorkspaceID,
			Label:       workspace.Label,
		})
	}
	return workspaces, nil
}

// ProcessInfo runs `herdr pane process-info --pane <id>` for one specific
// pane. The explicit --pane flag keeps the call independent of the caller's
// own pane id and of the UI-focused pane.
func (c *CLIPaneClient) ProcessInfo(paneID string) (PaneProcessInfo, error) {
	var response struct {
		ProcessInfo struct {
			PaneID              string `json:"pane_id"`
			ForegroundProcesses []struct {
				Name string   `json:"name"`
				Argv []string `json:"argv"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	}
	if err := c.runCLI([]string{"pane", "process-info", "--pane", paneID}, &response); err != nil {
		return PaneProcessInfo{}, err
	}
	info := PaneProcessInfo{PaneID: response.ProcessInfo.PaneID}
	for _, process := range response.ProcessInfo.ForegroundProcesses {
		info.ForegroundProcesses = append(info.ForegroundProcesses, PaneProcess{
			Name: process.Name,
			Argv: process.Argv,
		})
	}
	return info, nil
}

// RunCommand runs the shell that owns paneID. The command is a single
// argument: `pane run` submits it with Enter exactly as given, so the
// restore never assembles commands through the caller's shell.
func (c *CLIPaneClient) RunCommand(paneID, command string) error {
	return c.runCLI([]string{"pane", "run", paneID, command}, nil)
}

// ClosePreviewPane closes a preview pane whether or not Herdr still tracks
// plugin ownership: `plugin pane close` is the normal path for plugin-opened
// previews, and a plugin_pane_not_found answer falls back to the ordinary
// pane close for preview panes that the startup restore exec'd into their
// shells. The caller only reaches this with a pane it verified as a preview,
// so the fallback never widens the close scope to unrelated panes.
func (c *CLIPaneClient) ClosePreviewPane(paneID string) error {
	err := c.ClosePane(paneID)
	if !errors.Is(err, ErrPluginPaneNotFound) {
		return err
	}
	err = c.runCLI([]string{"pane", "close", paneID}, nil)
	if errors.Is(err, ErrPaneNotFound) {
		return nil
	}
	return err
}

// ReportMetadata runs `herdr pane report-metadata`. The pane ID is positional
// and must precede the options. A successful call prints nothing.
func (c *CLIPaneClient) ReportMetadata(request ReportMetadataRequest) error {
	args := []string{"pane", "report-metadata", request.PaneID, "--source", request.Source}
	for _, token := range request.Tokens {
		args = append(args, "--token", token)
	}
	return c.runCLI(args, nil)
}

type cliEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// runCLI executes one CLI call and decodes its JSON envelope. Success JSON
// arrives on stdout; failure JSON arrives on stderr with a non-zero exit.
func (c *CLIPaneClient) runCLI(args []string, result any) error {
	stdout, stderr, runErr := c.runner.Run(args...)
	output := stdout
	if strings.TrimSpace(output) == "" {
		output = stderr
	}

	if strings.TrimSpace(output) != "" {
		var envelope cliEnvelope
		if err := json.Unmarshal([]byte(output), &envelope); err != nil {
			if runErr != nil {
				return fmt.Errorf("herdr CLI failed (%v): %s", runErr, strings.TrimSpace(output))
			}
			return fmt.Errorf("parse herdr CLI output: %w", err)
		}
		if envelope.Error != nil {
			switch envelope.Error.Code {
			case paneNotFoundCode:
				return ErrPaneNotFound
			case pluginPaneNotFoundCode:
				return ErrPluginPaneNotFound
			}
			return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
		}
		if result != nil {
			if err := json.Unmarshal(envelope.Result, result); err != nil {
				return fmt.Errorf("parse herdr CLI result: %w", err)
			}
		}
		return nil
	}

	if runErr != nil {
		return fmt.Errorf("herdr CLI: %w", runErr)
	}
	return nil
}
