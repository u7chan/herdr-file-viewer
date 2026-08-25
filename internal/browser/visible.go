package browser

// VisibleRow is one flattened tree row. Its fields make the path, node, and
// depth available without another tree walk.
type VisibleRow struct {
	Path  string
	Node  *Node
	Depth int
}

// VisibleRows caches the flattened rows. It is invalidated by structural tree
// changes, not by selection state maintained by a caller.
type VisibleRows struct {
	rows  []VisibleRow
	dirty bool
}

// NewVisibleRows constructs an empty, invalidated cache.
func NewVisibleRows() VisibleRows {
	return VisibleRows{dirty: true}
}

// Dirty reports whether the flattened rows need to be rebuilt.
func (v *VisibleRows) Dirty() bool {
	return v != nil && v.dirty
}

// MarkDirty invalidates the flattened rows after a structural change.
func (v *VisibleRows) MarkDirty() {
	if v != nil {
		v.dirty = true
	}
}

// Rows returns the cached flattening, rebuilding it only when invalidated.
// Callers should treat the returned slice as read-only.
func (v *VisibleRows) Rows(root *Node) []VisibleRow {
	if v == nil {
		return nil
	}
	if !v.dirty {
		return v.rows
	}

	v.rows = v.rows[:0]
	appendVisibleRows(&v.rows, root, 0)
	v.dirty = false
	return v.rows
}

func appendVisibleRows(rows *[]VisibleRow, node *Node, depth int) {
	if node == nil {
		return
	}

	*rows = append(*rows, VisibleRow{
		Path:  node.path,
		Node:  node,
		Depth: depth,
	})
	if !node.expanded || !node.loaded {
		return
	}

	for _, child := range node.children {
		appendVisibleRows(rows, child, depth+1)
	}
}
