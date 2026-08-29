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
// receives these from Herdr at launch.
const (
	BinPathEnv      = "HERDR_BIN_PATH"
	EntrypointIDEnv = "HERDR_PLUGIN_ENTRYPOINT_ID"
	PaneIDEnv       = "HERDR_PANE_ID"
	WorkspaceIDEnv  = "HERDR_WORKSPACE_ID"
	PreviewFileEnv  = "HERDR_PREVIEW_FILE"
	// PreviewEntrypointID identifies the preview pane entrypoint.
	PreviewEntrypointID = "preview"
	paneNotFoundCode    = "pane_not_found"
)

// EntrypointID returns HERDR_PLUGIN_ENTRYPOINT_ID, the pane entrypoint that
// launched this process.
func EntrypointID() string {
	return os.Getenv(EntrypointIDEnv)
}

// IsPreviewEntrypoint reports whether this process runs the preview pane.
func IsPreviewEntrypoint() bool {
	return EntrypointID() == PreviewEntrypointID
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

// OpenPaneRequest describes one `herdr plugin pane open` invocation.
type OpenPaneRequest struct {
	Plugin     string
	Entrypoint string
	Placement  string
	TargetPane string
	Direction  string
	Env        []string // KEY=VALUE pairs propagated to the launched process
}

// ReportMetadataRequest describes one `herdr pane report-metadata`
// invocation.
type ReportMetadataRequest struct {
	PaneID string
	Source string
	Tokens []string // NAME=VALUE pairs
}

// PaneInfo is the pane data the viewer needs from the daemon. Tokens are the
// metadata tokens reported by the pane's process; a pane without metadata has
// an empty (nil) map.
type PaneInfo struct {
	PaneID string
	Tokens map[string]string
}

// PaneClient is the Herdr pane CLI surface used by the viewer. It is an
// interface so tests and the composition root can replace the subprocess
// boundary.
type PaneClient interface {
	// OpenPane opens a plugin pane and returns its pane ID.
	OpenPane(request OpenPaneRequest) (string, error)
	// ClosePane closes a pane. Closing an already-missing pane succeeds.
	ClosePane(paneID string) error
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

// OpenPane runs `herdr plugin pane open` with `--no-focus` so the keyboard
// focus stays in the caller's pane.
func (c *CLIPaneClient) OpenPane(request OpenPaneRequest) (string, error) {
	args := []string{
		"plugin", "pane", "open",
		"--plugin", request.Plugin,
		"--entrypoint", request.Entrypoint,
		"--placement", request.Placement,
		"--target-pane", request.TargetPane,
		"--direction", request.Direction,
		"--no-focus",
	}
	for _, env := range request.Env {
		args = append(args, "--env", env)
	}

	var response struct {
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
		Pane *struct {
			PaneID string            `json:"pane_id"`
			Tokens map[string]string `json:"tokens"`
		} `json:"pane"`
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
	info := PaneInfo{
		PaneID: response.Pane.PaneID,
		Tokens: response.Pane.Tokens,
	}
	if info.PaneID == "" {
		return PaneInfo{}, false, fmt.Errorf("get response contains no pane_id")
	}
	return info, true, nil
}

// ListPanes runs `herdr pane list`, scoped to one workspace when given.
func (c *CLIPaneClient) ListPanes(workspaceID string) ([]PaneInfo, error) {
	args := []string{"pane", "list"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}

	var response struct {
		Panes []struct {
			PaneID string            `json:"pane_id"`
			Tokens map[string]string `json:"tokens"`
		} `json:"panes"`
	}
	if err := c.runCLI(args, &response); err != nil {
		return nil, err
	}
	panes := make([]PaneInfo, 0, len(response.Panes))
	for _, pane := range response.Panes {
		panes = append(panes, PaneInfo{PaneID: pane.PaneID, Tokens: pane.Tokens})
	}
	return panes, nil
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
			if envelope.Error.Code == paneNotFoundCode {
				return ErrPaneNotFound
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
