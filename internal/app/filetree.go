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
	group    bool            // a changelist section header (name is the changelist)
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

// fileGroup is the files of one changelist (or of none) and, when the
// working copy uses changelists, the header row shown above them.
type fileGroup struct {
	header *treeNode // nil when there are no changelists
	files  []svn.FileStatus
}

// changelistGroups splits files by changelist: one group per changelist,
// sorted, then "Changes" for files in none. Without changelists there is a
// single headerless group.
func changelistGroups(files []svn.FileStatus) []fileGroup {
	byName := map[string][]svn.FileStatus{}
	var names []string
	for _, f := range files {
		if _, ok := byName[f.Changelist]; !ok && f.Changelist != "" {
			names = append(names, f.Changelist)
		}
		byName[f.Changelist] = append(byName[f.Changelist], f)
	}
	if len(names) == 0 {
		return []fileGroup{{files: files}}
	}
	sort.Strings(names)
	var groups []fileGroup
	for _, n := range names {
		groups = append(groups, fileGroup{header: &treeNode{key: "cl:" + n, name: n, dir: true, group: true}, files: byName[n]})
	}
	if rest := byName[""]; len(rest) > 0 {
		groups = append(groups, fileGroup{header: &treeNode{key: "cl:", name: "Changes", dir: true, group: true}, files: rest})
	}
	return groups
}

// groupTree is the tree (or, in flat mode, the list) of one group's files.
func (m *Model) groupTree(files []svn.FileStatus) *treeNode {
	if !m.flatFiles {
		return buildTree(files)
	}
	root := &treeNode{dir: true}
	for i := range files {
		f := &files[i]
		root.children = append(root.children, &treeNode{key: filepath.ToSlash(f.Path), name: f.Path, file: f})
	}
	return root
}

// rebuildRows recomputes the visible rows from m.files, keeping the cursor
// on the same path when it is still visible.
func (m *Model) rebuildRows() {
	var current string
	if r, ok := m.currentRow(); ok {
		current = r.node.key
	}
	m.fileRows = nil
	for _, g := range changelistGroups(m.files) {
		root := m.groupTree(g.files)
		if g.header == nil {
			m.fileRows = flatten(root, 0, m.collapsed, m.fileRows)
			continue
		}
		g.header.children = root.children
		m.fileRows = append(m.fileRows, fileRow{node: g.header})
		if !m.collapsed[g.header.key] {
			m.fileRows = flatten(root, 1, m.collapsed, m.fileRows)
		}
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
