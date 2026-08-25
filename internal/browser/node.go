package browser

import "path/filepath"

// NodeKind describes the entry type known without following a symlink.
type NodeKind uint8

const (
	KindFile NodeKind = iota
	KindDirectory
	KindSymlink
)

// Node is a filesystem entry in a Tree. Its state is changed by Tree methods;
// a load result is applied by the caller that owns the update loop.
type Node struct {
	path      string
	name      string
	kind      NodeKind
	parent    *Node
	children  []*Node
	expanded  bool
	loaded    bool
	loading   bool
	loadID    uint64
	loadError error
}

func newRootNode(path string) *Node {
	return &Node{
		path: path,
		name: filepath.Base(path),
		kind: KindDirectory,
	}
}

func newChildNode(parent *Node, path, name string, kind NodeKind) *Node {
	return &Node{
		path:   path,
		name:   name,
		kind:   kind,
		parent: parent,
	}
}

// Path returns the node's absolute path.
func (n *Node) Path() string {
	if n == nil {
		return ""
	}
	return n.path
}

// Name returns the display name supplied by the directory entry. The root
// uses the final component of its absolute path.
func (n *Node) Name() string {
	if n == nil {
		return ""
	}
	return n.name
}

// Kind returns the node's entry kind.
func (n *Node) Kind() NodeKind {
	if n == nil {
		return KindFile
	}
	return n.kind
}

// IsDirectory reports whether the node can be expanded.
func (n *Node) IsDirectory() bool {
	return n != nil && n.kind == KindDirectory
}

// IsSymlink reports whether the node is a symbolic link.
func (n *Node) IsSymlink() bool {
	return n != nil && n.kind == KindSymlink
}

// Parent returns the parent node, or nil for the root.
func (n *Node) Parent() *Node {
	if n == nil {
		return nil
	}
	return n.parent
}

// Children returns the cached children in deterministic display order.
func (n *Node) Children() []*Node {
	if n == nil {
		return nil
	}
	return append([]*Node(nil), n.children...)
}

// Expanded reports whether the node is currently expanded.
func (n *Node) Expanded() bool {
	return n != nil && n.expanded
}

// Loaded reports whether a directory has completed a successful read. An
// empty directory is loaded too; an errored directory remains retryable.
func (n *Node) Loaded() bool {
	return n != nil && n.loaded
}

// Loading reports whether a read request is currently outstanding.
func (n *Node) Loading() bool {
	return n != nil && n.loading
}

// LoadError returns the last recoverable directory read error, if any.
func (n *Node) LoadError() error {
	if n == nil {
		return nil
	}
	return n.loadError
}
