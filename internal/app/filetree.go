package app

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/cupskrrtt/seven/internal/svn"
)

// treeNode is a directory or file in the Files panel tree.
type treeNode struct {
	key      string // slash-separated path, stable across refreshes
	name     string // display name; compressed chains read "a/b/c"
	dir      bool
	file     *svn.FileStatus // status entry for this path, nil for plain folders
	children []*treeNode
}

// fileRow is one visible line of the Files panel.
type fileRow struct {
	node  *treeNode
	depth int
}

// buildTree turns status entries into a directory tree. Folders sort
// before files; chains of folders with a single sub-folder are merged.
func buildTree(files []svn.FileStatus) *treeNode {
	root := &treeNode{dir: true}
	for i := range files {
		parts := strings.Split(filepath.ToSlash(files[i].Path), "/")
		n := root
		for j, part := range parts {
			key := strings.Join(parts[:j+1], "/")
			var next *treeNode
			for _, c := range n.children {
				if c.key == key {
					next = c
					break
				}
			}
			if next == nil {
				next = &treeNode{key: key, name: part}
				n.children = append(n.children, next)
			}
			n.dir = true
			n = next
		}
		n.file = &files[i]
	}
	finishTree(root)
	return root
}

func finishTree(n *treeNode) {
	for _, c := range n.children {
		finishTree(c)
	}
	sort.SliceStable(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.dir != b.dir {
			return a.dir
		}
		return a.name < b.name
	})
	// Merge "a" → "b" into "a/b" when a has nothing else and no status of its own.
	for i, c := range n.children {
		for c.dir && c.file == nil && len(c.children) == 1 && c.children[0].dir {
			only := c.children[0]
			only.name = c.name + "/" + only.name
			c = only
		}
		n.children[i] = c
	}
}

// flatten lists the visible rows, skipping collapsed folders' contents.
func flatten(n *treeNode, depth int, collapsed map[string]bool, out []fileRow) []fileRow {
	for _, c := range n.children {
		out = append(out, fileRow{node: c, depth: depth})
		if c.dir && !collapsed[c.key] {
			out = flatten(c, depth+1, collapsed, out)
		}
	}
	return out
}

// filesUnder returns every status entry at or below n.
func filesUnder(n *treeNode) []svn.FileStatus {
	var out []svn.FileStatus
	if n.file != nil {
		out = append(out, *n.file)
	}
	for _, c := range n.children {
		out = append(out, filesUnder(c)...)
	}
	return out
}

// rebuildRows recomputes the visible rows from m.files, keeping the cursor
// on the same path when it is still visible.
func (m *Model) rebuildRows() {
	var current string
	if r, ok := m.currentRow(); ok {
		current = r.node.key
	}
	if m.flatFiles {
		m.fileRows = m.fileRows[:0]
		for i := range m.files {
			f := &m.files[i]
			m.fileRows = append(m.fileRows, fileRow{node: &treeNode{key: filepath.ToSlash(f.Path), name: f.Path, file: f}})
		}
	} else {
		m.fileRows = flatten(buildTree(m.files), 0, m.collapsed, nil)
	}
	cursor := min(m.fileList.cursor, max(0, len(m.fileRows)-1))
	for i, r := range m.fileRows {
		if r.node.key == current {
			cursor = i
			break
		}
	}
	m.fileList.set(cursor, len(m.fileRows))
}

func (m *Model) currentRow() (fileRow, bool) {
	if m.fileList.cursor < len(m.fileRows) {
		return m.fileRows[m.fileList.cursor], true
	}
	return fileRow{}, false
}

// currentDir returns the folder under the cursor, if it is one.
func (m *Model) currentDir() (*treeNode, bool) {
	r, ok := m.currentRow()
	if !ok || !r.node.dir {
		return nil, false
	}
	return r.node, true
}

// setCollapsedAll collapses or expands every folder.
func (m *Model) setCollapsedAll(collapse bool) {
	m.collapsed = map[string]bool{}
	if collapse {
		var walk func(n *treeNode)
		walk = func(n *treeNode) {
			for _, c := range n.children {
				if c.dir {
					m.collapsed[c.key] = true
					walk(c)
				}
			}
		}
		walk(buildTree(m.files))
	}
	m.rebuildRows()
}

// topLevel drops paths that lie inside another path of the list, so a
// recursive svn command does not visit them twice.
func topLevel(paths []string) []string {
	var out []string
	for _, p := range paths {
		inside := false
		for _, q := range paths {
			if q != p && strings.HasPrefix(filepath.ToSlash(p), filepath.ToSlash(q)+"/") {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, p)
		}
	}
	return out
}
