package herdr

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPreviewStateStoreSaveLoadRemoveRoundTrip(t *testing.T) {
	store := NewPreviewStateStore(t.TempDir(), "/tmp/server.sock")
	if err := store.Save("wY:p9Z", "/pre view/file=1.md"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	file, found, err := store.Load("wY:p9Z")
	if err != nil || !found || file != "/pre view/file=1.md" {
		t.Fatalf("Load() = %q, %v, %v; want the saved file", file, found, err)
	}
	if err := store.Remove("wY:p9Z"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, found, err := store.Load("wY:p9Z"); err != nil || found {
		t.Fatalf("Load() after Remove = found %v, error %v; want gone", found, err)
	}
	if err := store.Remove("wY:p9Z"); err != nil {
		t.Fatalf("Remove() of a missing pane error = %v, want nil", err)
	}
}

func TestPreviewStateStoreLoadDistinguishesMissingFromCorrupt(t *testing.T) {
	stateDir := t.TempDir()
	store := NewPreviewStateStore(stateDir, "/sock")

	if _, found, err := store.Load("wY:p1"); found || err != nil {
		t.Fatalf("Load(missing) = found %v, error %v; want not found without error", found, err)
	}

	path := filepath.Join(stateDir, "preview", previewStateNamespace("/sock"), "wY:p2.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, content := range []string{"not json", ""} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if _, found, err := store.Load("wY:p2"); found || err == nil {
			t.Fatalf("Load(%q) = found %v, error %v; want corrupt error", content, found, err)
		}
	}

	// A state file with an empty file path is equally unusable.
	if err := os.WriteFile(path, []byte(`{"file":""}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, found, err := store.Load("wY:p2"); found || err == nil {
		t.Fatalf("Load(empty file) = found %v, error %v; want corrupt error", found, err)
	}
}

func TestPreviewStateStoreIsolatedPerServerSocket(t *testing.T) {
	stateDir := t.TempDir()
	first := NewPreviewStateStore(stateDir, "/server/one.sock")
	second := NewPreviewStateStore(stateDir, "/server/two.sock")

	if err := first.Save("wY:p9Z", "/one.md"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	// The same pane ID in another server's namespace must not see the state.
	if _, found, err := second.Load("wY:p9Z"); err != nil || found {
		t.Fatalf("second server Load() = found %v, error %v; want isolated namespaces", found, err)
	}
	if err := second.Save("wY:p9Z", "/two.md"); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	file, found, err := first.Load("wY:p9Z")
	if err != nil || !found || file != "/one.md" {
		t.Fatalf("first server Load() = %q, %v, %v; want its own state kept", file, found, err)
	}
}

func TestPreviewStateStoreDetachedWithoutHerdrContext(t *testing.T) {
	store := NewPreviewStateStore("", "")
	if err := store.Save("wY:p9Z", "/a.md"); err != nil {
		t.Fatalf("Save() error = %v, want no-op", err)
	}
	if _, found, err := store.Load("wY:p9Z"); err != nil || found {
		t.Fatalf("Load() = found %v, error %v; want not found no-op", found, err)
	}
	if err := store.Remove("wY:p9Z"); err != nil {
		t.Fatalf("Remove() error = %v, want no-op", err)
	}
	if ids, err := store.ListPaneIDs(); err != nil || len(ids) != 0 {
		t.Fatalf("ListPaneIDs() = %v, %v; want empty no-op", ids, err)
	}
}

func TestPreviewStateStoreListsOnlyThisNamespace(t *testing.T) {
	stateDir := t.TempDir()
	store := NewPreviewStateStore(stateDir, "/sock")
	other := NewPreviewStateStore(stateDir, "/other")

	ids := []string{"wY:p1", "wY:p2"}
	for _, paneID := range ids {
		if err := store.Save(paneID, "/"+paneID+".md"); err != nil {
			t.Fatalf("Save(%s) error = %v", paneID, err)
		}
	}
	if err := other.Save("wX:p9", "/x.md"); err != nil {
		t.Fatalf("other Save() error = %v", err)
	}

	got, err := store.ListPaneIDs()
	if err != nil {
		t.Fatalf("ListPaneIDs() error = %v", err)
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("ListPaneIDs() = %v, want %v", got, ids)
	}
}

func TestPreviewStateStoreRejectsPaneIDTraversal(t *testing.T) {
	store := NewPreviewStateStore(t.TempDir(), "/sock")
	for _, paneID := range []string{"../escape", "a/b", ".", ".."} {
		if err := store.Save(paneID, "/a.md"); err == nil {
			t.Fatalf("Save(%q) error = nil, want traversal rejection", paneID)
		}
		if _, _, err := store.Load(paneID); err == nil {
			t.Fatalf("Load(%q) error = nil, want traversal rejection", paneID)
		}
		if err := store.Remove(paneID); err == nil {
			t.Fatalf("Remove(%q) error = nil, want traversal rejection", paneID)
		}
	}
}

func TestPreviewStateStoreNamespaceIsStablePerSocketPath(t *testing.T) {
	first := NewPreviewStateStore("/state", "/home/u7dev/.config/herdr/herdr.sock")
	second := NewPreviewStateStore("/state", "/home/u7dev/.config/herdr/herdr.sock")
	if first.namespace == "" || first.namespace != second.namespace {
		t.Fatalf("namespaces = %q and %q, want one stable digest", first.namespace, second.namespace)
	}
	if strings.ContainsAny(first.namespace, "/.") {
		t.Fatalf("namespace = %q, want a filesystem-safe digest", first.namespace)
	}
}