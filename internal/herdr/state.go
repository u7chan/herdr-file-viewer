package herdr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PreviewState is the durable per-pane state a running preview owns. The
// startup restore reads it to relaunch the preview into the pane that lost
// its process during a cold session restore.
type PreviewState struct {
	File string `json:"file"`
}

// PreviewStateStore persists one small preview state per pane under
// HERDR_PLUGIN_STATE_DIR. State is namespaced by the server socket path
// because the state dir can be shared by Herdr servers with different
// configs, while public pane IDs are only stable inside one server; a plain
// pane-ID key would let one server's restore hijack another server's panes.
// Nothing in the store runs at server shutdown, so restore state survives
// stopping the server.
type PreviewStateStore struct {
	stateDir  string
	namespace string
}

// NewPreviewStateStore builds the per-pane store. An empty state dir or
// socket path leaves the store detached: all operations become no-ops, so
// callers outside a Herdr context never fail on state.
func NewPreviewStateStore(stateDir, socketPath string) *PreviewStateStore {
	if strings.TrimSpace(stateDir) == "" || strings.TrimSpace(socketPath) == "" {
		return &PreviewStateStore{}
	}
	return &PreviewStateStore{
		stateDir:  stateDir,
		namespace: previewStateNamespace(socketPath),
	}
}

// previewStateNamespace derives the per-server state namespace from the
// socket path: one hex digest of the path, truncated to 16 characters.
func previewStateNamespace(socketPath string) string {
	sum := sha256.Sum256([]byte(socketPath))
	return hex.EncodeToString(sum[:])[:16]
}

func (s *PreviewStateStore) detached() bool {
	return s.stateDir == ""
}

func (s *PreviewStateStore) panePath(paneID string) (string, error) {
	if filepath.Base(paneID) != paneID || paneID == "." || paneID == ".." {
		return "", fmt.Errorf("invalid pane id %q", paneID)
	}
	return filepath.Join(s.stateDir, "preview", s.namespace, paneID+".json"), nil
}

// Save durably writes the preview file of one pane. The write is atomic:
// the state lands in the namespace only after a completed rename, so a
// crash never leaves a half-written restore state.
func (s *PreviewStateStore) Save(paneID, file string) error {
	if s.detached() || file == "" {
		return nil
	}
	path, err := s.panePath(paneID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create preview state dir: %w", err)
	}
	content, err := json.Marshal(PreviewState{File: file})
	if err != nil {
		return fmt.Errorf("encode preview state: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create preview state file: %w", err)
	}
	_, writeErr := f.Write(content)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if writeErr != nil {
			return fmt.Errorf("write preview state file: %w", writeErr)
		}
		if syncErr != nil {
			return fmt.Errorf("sync preview state file: %w", syncErr)
		}
		return fmt.Errorf("close preview state file: %w", closeErr)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit preview state file: %w", err)
	}
	return nil
}

// Load returns the preview file saved for one pane. found=false means the
// pane has no saved state; a non-nil error means the state exists but is
// corrupt (unparseable or empty), which must skip the restore.
func (s *PreviewStateStore) Load(paneID string) (file string, found bool, err error) {
	if s.detached() {
		return "", false, nil
	}
	path, err := s.panePath(paneID)
	if err != nil {
		return "", false, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read preview state: %w", err)
	}
	var state PreviewState
	if err := json.Unmarshal(content, &state); err != nil {
		return "", false, fmt.Errorf("parse preview state: %w", err)
	}
	if state.File == "" {
		return "", false, errors.New("preview state contains no file")
	}
	return state.File, true, nil
}

// Remove deletes the saved state of one pane. Removing a pane without state
// succeeds.
func (s *PreviewStateStore) Remove(paneID string) error {
	if s.detached() {
		return nil
	}
	path, err := s.panePath(paneID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove preview state: %w", err)
	}
	return nil
}

// ListPaneIDs returns the pane IDs that have saved state in this server's
// namespace.
func (s *PreviewStateStore) ListPaneIDs() ([]string, error) {
	if s.detached() {
		return nil, nil
	}
	dir := filepath.Join(s.stateDir, "preview", s.namespace)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list preview state dir: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	return ids, nil
}
