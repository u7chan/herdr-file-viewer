package browser

import (
	"strconv"
	"testing"
)

var benchmarkVisibleRowsSink []VisibleRow

func BenchmarkVisibleRows(b *testing.B) {
	fixtures := []struct {
		name  string
		build func() (*Tree, int)
	}{
		{name: "Small", build: benchmarkSmallVisibleTree},
		{name: "Wide", build: benchmarkWideVisibleTree},
		{name: "Deep", build: benchmarkDeepVisibleTree},
	}

	for _, fixture := range fixtures {
		b.Run("Rebuild/"+fixture.name, func(b *testing.B) {
			tree, wantRows := fixture.build()
			benchmarkPrepareVisibleRows(b, tree, wantRows)
			b.ReportAllocs()

			for b.Loop() {
				// Invalidate immediately before each call so rebuilds never
				// accidentally become cache-hit measurements.
				tree.visible.MarkDirty()
				benchmarkVisibleRowsSink = tree.VisibleRows()
			}
		})

		b.Run("CacheHit/"+fixture.name, func(b *testing.B) {
			tree, wantRows := fixture.build()
			benchmarkPrepareVisibleRows(b, tree, wantRows)
			b.ReportAllocs()

			for b.Loop() {
				benchmarkVisibleRowsSink = tree.VisibleRows()
			}
		})
	}
}

func benchmarkPrepareVisibleRows(b *testing.B, tree *Tree, wantRows int) {
	b.Helper()
	rows := tree.VisibleRows()
	if len(rows) != wantRows {
		b.Fatalf("fixture visible rows = %d, want %d", len(rows), wantRows)
	}
	if tree.VisibleRowsDirty() {
		b.Fatal("fixture visible rows remain dirty after setup")
	}
}

func benchmarkSmallVisibleTree() (*Tree, int) {
	root := newRootNode("/benchmark/small")
	root.expanded = true
	root.loaded = true
	tree := &Tree{root: root, visible: NewVisibleRows()}

	directory := newChildNode(root, root.path+"/directory", "directory", KindDirectory)
	directory.expanded = true
	directory.loaded = true
	directory.children = make([]*Node, 0, 3)
	for index := 0; index < 3; index++ {
		name := "nested-" + strconv.Itoa(index)
		directory.children = append(directory.children, newChildNode(
			directory,
			directory.path+"/"+name,
			name,
			KindFile,
		))
	}
	root.children = append(root.children, directory)
	for index := 0; index < 3; index++ {
		name := "file-" + strconv.Itoa(index)
		root.children = append(root.children, newChildNode(
			root,
			root.path+"/"+name,
			name,
			KindFile,
		))
	}

	return tree, 8
}

func benchmarkWideVisibleTree() (*Tree, int) {
	const childCount = 1024

	root := newRootNode("/benchmark/wide")
	root.expanded = true
	root.loaded = true
	root.children = make([]*Node, 0, childCount)
	for index := 0; index < childCount; index++ {
		name := "file-" + strconv.Itoa(index)
		root.children = append(root.children, newChildNode(
			root,
			root.path+"/"+name,
			name,
			KindFile,
		))
	}

	return &Tree{root: root, visible: NewVisibleRows()}, childCount + 1
}

func benchmarkDeepVisibleTree() (*Tree, int) {
	const depth = 64

	root := newRootNode("/benchmark/deep")
	root.expanded = true
	root.loaded = true
	tree := &Tree{root: root, visible: NewVisibleRows()}
	parent := root
	for level := 1; level <= depth; level++ {
		name := "directory-" + strconv.Itoa(level)
		child := newChildNode(parent, parent.path+"/"+name, name, KindDirectory)
		child.expanded = true
		child.loaded = true
		parent.children = []*Node{child}
		parent = child
	}

	name := "leaf"
	parent.children = []*Node{newChildNode(parent, parent.path+"/"+name, name, KindFile)}
	return tree, depth + 2
}
