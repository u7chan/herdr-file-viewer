package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const contextJSONEnv = "HERDR_PLUGIN_CONTEXT_JSON"

// RootSource identifies which startup directory supplied the resolved root.
type RootSource string

const (
	FocusedPaneRoot RootSource = "focused_pane_cwd"
	WorkspaceRoot   RootSource = "workspace_cwd"
	ProcessRoot     RootSource = "process_cwd"
)

// ContextSnapshot is the startup context supplied by Herdr. Unknown fields
// are intentionally ignored so adding fields to Herdr's snapshot is harmless.
type ContextSnapshot struct {
	FocusedPaneCWD string `json:"focused_pane_cwd"`
	WorkspaceCWD   string `json:"workspace_cwd"`
}

// RootResolution is the result of the one-time startup root lookup.
type RootResolution struct {
	Path    string
	Source  RootSource
	Warning string
}

// ResolveRoot reads the Herdr context and process cwd once, then chooses the
// first existing directory in focused-pane, workspace, process order.
func ResolveRoot() (RootResolution, error) {
	processCWD, err := os.Getwd()
	if err != nil {
		return RootResolution{}, fmt.Errorf("get process cwd: %w", err)
	}
	return ResolveRootAt(processCWD, os.LookupEnv)
}

// ResolveRootAt is the injectable form used by tests and keeps all environment
// and context parsing inside this package.
func ResolveRootAt(processCWD string, lookupEnv func(string) (string, bool)) (RootResolution, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	processRoot := absolutePath(processCWD)

	warnings := make([]string, 0, 3)
	snapshot, warning := readSnapshot(lookupEnv)
	if warning != "" {
		warnings = append(warnings, warning)
	}

	candidates := []struct {
		source RootSource
		path   string
	}{
		{source: FocusedPaneRoot, path: snapshot.FocusedPaneCWD},
		{source: WorkspaceRoot, path: snapshot.WorkspaceCWD},
	}

	for _, candidate := range candidates {
		if candidate.path == "" {
			continue
		}
		candidatePath := resolvePath(processRoot, candidate.path)
		if err := ensureDirectory(candidatePath); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s %q unavailable: %v", candidate.source, candidate.path, err))
			continue
		}
		return RootResolution{
			Path:    candidatePath,
			Source:  candidate.source,
			Warning: strings.Join(warnings, "; "),
		}, nil
	}

	if snapshot.FocusedPaneCWD == "" && snapshot.WorkspaceCWD == "" && warning == "" {
		warnings = append(warnings, "Herdr context contains no cwd candidates")
	}

	if err := ensureDirectory(processRoot); err != nil {
		return RootResolution{}, fmt.Errorf("process cwd %q is unavailable: %w", processCWD, err)
	}

	return RootResolution{
		Path:    processRoot,
		Source:  ProcessRoot,
		Warning: strings.Join(warnings, "; "),
	}, nil
}

func readSnapshot(lookupEnv func(string) (string, bool)) (ContextSnapshot, string) {
	raw, ok := lookupEnv(contextJSONEnv)
	if !ok || strings.TrimSpace(raw) == "" {
		return ContextSnapshot{}, "Herdr context is missing"
	}

	var snapshot ContextSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return ContextSnapshot{}, fmt.Sprintf("Herdr context is invalid JSON: %v", err)
	}
	return snapshot, ""
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("not a directory")
	}
	return nil
}

func absolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func resolvePath(base, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}
