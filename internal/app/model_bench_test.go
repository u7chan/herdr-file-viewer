package app

import (
	"io/fs"
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

var benchmarkViewportLines []string

func BenchmarkViewportRendering(b *testing.B) {
	tests := []struct {
		name string
		read filesystem.FileSystem
	}{
		// The default tree renders no permission column.
		{name: "ManyRows", read: benchmarkFileSystem{entries: benchmarkViewportEntries(false)}},
		// The permission column adds one styled span per row, so it gets its
		// own baseline instead of hiding inside the default number.
		{name: "ManyRowsWithPermissions", read: benchmarkPermissionFileSystem{entries: benchmarkViewportEntries(true)}},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			model := benchmarkViewportModel(b, test.read)
			_, treeHeight, _ := layoutHeights(model.height)
			lines := model.renderTree(treeHeight)
			if len(lines) != treeHeight {
				b.Fatalf("fixture rendered rows = %d, want viewport height %d", len(lines), treeHeight)
			}
			b.ReportAllocs()

			for b.Loop() {
				benchmarkViewportLines = model.renderTree(treeHeight)
			}
		})
	}
}

func benchmarkViewportEntries(withPermissions bool) []filesystem.Entry {
	const rowCount = 2048
	entries := make([]filesystem.Entry, 0, rowCount)
	for index := 0; index < rowCount; index++ {
		entry := filesystem.Entry{Name: "file-" + strconv.Itoa(index)}
		if withPermissions {
			entry.Perm = fs.FileMode(0o600 + index%0o100)
			entry.HasPerm = true
		}
		entries = append(entries, entry)
	}
	return entries
}

func benchmarkViewportModel(b *testing.B, read filesystem.FileSystem) *Model {
	b.Helper()

	model := NewModel("/benchmark/viewport", "", read)
	load := model.Init()
	if load == nil {
		b.Fatal("fixture Init() returned nil command")
	}
	result := loadResultFromInit(b, load)
	model.Update(result)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model.selected = len(model.visibleRows) / 2
	model.keepSelectionVisible()
	if model.tree.VisibleRowsDirty() {
		b.Fatal("fixture visible rows are dirty after setup")
	}
	return model
}

type benchmarkFileSystem struct {
	entries []filesystem.Entry
}

func (f benchmarkFileSystem) ReadDir(string) ([]filesystem.Entry, error) {
	return append([]filesystem.Entry(nil), f.entries...), nil
}

// benchmarkPermissionFileSystem advertises the lstat capability so the model
// reserves the permission column while rendering the same in-memory rows.
type benchmarkPermissionFileSystem struct {
	entries []filesystem.Entry
}

func (f benchmarkPermissionFileSystem) ReadsPermissions() bool {
	return true
}

func (f benchmarkPermissionFileSystem) ReadDir(string) ([]filesystem.Entry, error) {
	return append([]filesystem.Entry(nil), f.entries...), nil
}
