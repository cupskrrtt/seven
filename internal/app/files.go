package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

// applyStatus replaces the file list, keeping the cursor on the same path
// and dropping selections of paths that no longer have changes.
func (m *Model) applyStatus(files []svn.FileStatus) {
	m.files = files
	present := map[string]bool{}
	for _, f := range files {
		present[f.Path] = true
	}
	for p := range m.selected {
		if !present[p] {
			delete(m.selected, p)
		}
	}
	m.rebuildRows()
}

// currentFile returns the file under the cursor; false on a folder row.
func (m *Model) currentFile() (svn.FileStatus, bool) {
	r, ok := m.currentRow()
	if !ok || r.node.dir || r.node.file == nil {
		return svn.FileStatus{}, false
	}
	return *r.node.file, true
}

func (m *Model) handleFilesKey(key string) tea.Cmd {
	n := len(m.fileRows)
	switch key {
	case "j", "down":
		m.fileList.move(1, n)
		return m.syncMain()
	case "k", "up":
		m.fileList.move(-1, n)
		return m.syncMain()
	case "g", "home":
		m.fileList.set(0, n)
		return m.syncMain()
	case "G", "end":
		m.fileList.set(n-1, n)
		return m.syncMain()
	case "`":
		m.flatFiles = !m.flatFiles
		m.rebuildRows()
		return m.syncMain()
	case "-", "=":
		if !m.flatFiles {
			m.setCollapsedAll(key == "-")
		}
		return m.syncMain()
	case "w":
		m.ignoreWhitespace = !m.ignoreWhitespace
		m.mainKey = ""
		return m.syncMain()
	case "c":
		return m.startCommit(false)
	case "C":
		return m.startCommit(true)
	case "S":
		m.shelvesMenu()
		return nil
	}
	if dir, ok := m.currentDir(); ok {
		return m.handleDirKey(dir, key)
	}
	f, ok := m.currentFile()
	if !ok {
		return nil
	}
	if key == "enter" {
		if f.Item == svn.ItemConflicted {
			return m.openConflict(f)
		}
		return m.setFocus(panelMain)
	}

	switch key {
	case " ":
		if !committable(f) && !m.selected[f.Path] {
			m.status = f.Path + " has no local change to commit"
			return nil
		}
		if m.selected[f.Path] {
			delete(m.selected, f.Path)
		} else {
			m.selected[f.Path] = true
		}
		m.fileList.move(1, n)
		return m.syncMain()
	case "A":
		// Toggle selection of all versioned changes.
		all := len(m.selected) == 0
		m.selected = map[string]bool{}
		if all {
			for _, f := range m.files {
				if committable(f) {
					m.selected[f.Path] = true
				}
			}
		}
	case "a":
		if f.Item != svn.ItemUnversioned {
			m.status = f.Path + " is already versioned"
			return nil
		}
		return m.op(func(ctx context.Context) (string, error) {
			return "Added " + f.Path, m.client.Add(ctx, f.Path)
		})
	case "d":
		m.confirmRevert(f)
	case "D":
		if !f.Versioned() {
			m.status = "Use 'd' to delete unversioned files"
			return nil
		}
		m.popup = &confirmPopup{
			heading: "Schedule deletion",
			text:    fmt.Sprintf("svn rm %s? The file is removed from disk and deleted on next commit.", f.Path),
			onYes: m.op(func(ctx context.Context) (string, error) {
				return "Scheduled " + f.Path + " for deletion", m.client.Remove(ctx, f.Path)
			}),
		}
	case "i":
		if f.Item != svn.ItemUnversioned {
			m.status = "Only unversioned files can be ignored"
			return nil
		}
		return m.op(func(ctx context.Context) (string, error) {
			return "Ignored " + f.Path, m.client.Ignore(ctx, f.Path)
		})
	case "R":
		if !f.Conflicted() {
			m.status = f.Path + " is not conflicted"
			return nil
		}
		m.resolveMenu(f)
	case "e":
		return m.openEditor(m.abs(f.Path))
	case "b":
		if f.Item == svn.ItemUnversioned || f.Item == svn.ItemAdded {
			m.status = f.Path + " has no history to blame yet"
			return nil
		}
		return m.openBlame(f.Path)
	case "l":
		if f.Item == svn.ItemUnversioned || f.Item == svn.ItemAdded {
			m.status = f.Path + " has no history yet"
			return nil
		}
		return m.setLogPath(f.Path)
	case "L":
		if !f.Versioned() {
			m.status = "Only versioned files can be in a changelist"
			return nil
		}
		m.changelistMenu(f)
	case "K":
		if !f.Versioned() || f.Item == svn.ItemAdded {
			m.status = "Only committed files can be locked"
			return nil
		}
		m.lockMenu(f)
	case "P":
		if !f.Versioned() {
			m.status = "Unversioned files have no properties"
			return nil
		}
		return m.openProps(f.Path)
	}
	return nil
}

// handleDirKey applies folder actions to everything below dir.
func (m *Model) handleDirKey(dir *treeNode, key string) tea.Cmd {
	under := filesUnder(dir)
	if dir.group {
		switch key {
		case "d":
			m.confirmDiscardAll(groupLabel(dir), under)
			return nil
		case "l", "P", "b", "L", "R", "e", "D", "i", "K":
			m.status = "Not available on a changelist — pick a file inside it"
			return nil
		}
	}
	switch key {
	case "enter":
		m.collapsed[dir.key] = !m.collapsed[dir.key]
		m.rebuildRows()
		return m.syncMain()
	case " ":
		// Select every versioned change below; if all are selected, clear them.
		all := true
		for _, f := range under {
			if committable(f) && !m.selected[f.Path] {
				all = false
			}
		}
		for _, f := range under {
			if !committable(f) {
				continue
			}
			if all {
				delete(m.selected, f.Path)
			} else {
				m.selected[f.Path] = true
			}
		}
		m.fileList.move(1, len(m.fileRows))
		return m.syncMain()
	case "a":
		var paths []string
		for _, f := range under {
			if f.Item == svn.ItemUnversioned {
				paths = append(paths, f.Path)
			}
		}
		if len(paths) == 0 {
			m.status = "Nothing unversioned under " + dir.key
			return nil
		}
		return m.op(func(ctx context.Context) (string, error) {
			return fmt.Sprintf("Added %d item(s) under %s", len(paths), dir.key), m.client.Add(ctx, paths...)
		})
	case "d":
		m.confirmDiscardAll(dir.key+"/", under)
	case "l":
		return m.setLogPath(filepath.FromSlash(dir.key))
	case "P":
		return m.openProps(filepath.FromSlash(dir.key))
	case "b", "L", "R", "e", "D", "i", "K":
		m.status = "Not available on a folder — expand it (enter) and pick a file"
	}
	return nil
}

// confirmDiscardAll reverts the versioned entries and deletes the
// unversioned ones in files, after backing all of them up.
func (m *Model) confirmDiscardAll(label string, files []svn.FileStatus) {
	var versioned, unversioned, all []string
	for _, f := range files {
		all = append(all, f.Path)
		if f.Versioned() {
			versioned = append(versioned, f.Path)
		} else {
			unversioned = append(unversioned, f.Path)
		}
	}
	text := fmt.Sprintf("Discard all changes under %s?\n\n  %d versioned change(s) are reverted\n", label, len(versioned))
	if len(unversioned) > 0 {
		text += fmt.Sprintf("  %d unversioned item(s) are deleted\n", len(unversioned))
	}
	text += "\nA backup is saved first."
	m.popup = &confirmPopup{heading: "Discard folder", text: text, onYes: m.op(func(ctx context.Context) (string, error) {
		versioned := topLevel(versioned)
		var diff string
		if len(versioned) > 0 {
			diff, _ = m.client.Diff(ctx, versioned[0], false, versioned[1:]...)
		}
		dir, err := m.backup(topLevel(all), diff)
		if err != nil {
			return "", fmt.Errorf("backup failed, nothing discarded: %w", err)
		}
		if len(versioned) > 0 {
			if err := m.client.Revert(ctx, versioned...); err != nil {
				return "", err
			}
		}
		for _, p := range unversioned {
			if err := os.RemoveAll(m.abs(p)); err != nil {
				return "", err
			}
		}
		return withBackup(fmt.Sprintf("Discarded %d item(s) under %s", len(all), label), dir), nil
	})}
}

// groupLabel names a changelist section: "changelist wip" or "Changes".
func groupLabel(n *treeNode) string {
	if n.key == "cl:" {
		return n.name
	}
	return "changelist " + n.name
}

// committable reports whether f has a local change svn commit would send.
func committable(f svn.FileStatus) bool { return f.Versioned() && f.LocalChange() }

// confirmRevert asks before discarding f's changes; a backup is saved first.
func (m *Model) confirmRevert(f svn.FileStatus) {
	if f.Item == svn.ItemUnversioned {
		m.popup = &confirmPopup{
			heading: "Delete unversioned file",
			text:    fmt.Sprintf("Delete %s from disk? A copy is kept in the backup folder.", f.Path),
			onYes: m.op(func(context.Context) (string, error) {
				dir, err := m.backup([]string{f.Path}, "")
				if err != nil {
					return "", fmt.Errorf("backup failed, nothing deleted: %w", err)
				}
				if err := os.RemoveAll(m.abs(f.Path)); err != nil {
					return "", err
				}
				return withBackup("Deleted "+f.Path, dir), nil
			}),
		}
		return
	}
	m.popup = &confirmPopup{
		heading: "Revert",
		text:    fmt.Sprintf("Discard all local changes to %s? A backup is saved first.", f.Path),
		onYes: m.op(func(ctx context.Context) (string, error) {
			diff, _ := m.client.Diff(ctx, f.Path, false)
			dir, err := m.backup([]string{f.Path}, diff)
			if err != nil {
				return "", fmt.Errorf("backup failed, nothing reverted: %w", err)
			}
			if err := m.client.Revert(ctx, f.Path); err != nil {
				return "", err
			}
			return withBackup("Reverted "+f.Path, dir), nil
		}),
	}
}

func withBackup(status, dir string) string {
	if dir == "" {
		return status
	}
	return status + " · backup: " + dir
}

// changelists returns the names of changelists in use, sorted.
func (m *Model) changelists() []string {
	seen := map[string]bool{}
	var names []string
	for _, f := range m.files {
		if f.Changelist != "" && !seen[f.Changelist] {
			seen[f.Changelist] = true
			names = append(names, f.Changelist)
		}
	}
	sort.Strings(names)
	return names
}

func (m *Model) changelistMenu(f svn.FileStatus) {
	move := func(name string) func() tea.Cmd {
		return func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) {
				return fmt.Sprintf("Moved %s to changelist %s", f.Path, name), m.client.SetChangelist(ctx, name, f.Path)
			})
		}
	}
	var items []menuItem
	n := 0
	for _, name := range m.changelists() {
		if name == f.Changelist || n == 9 {
			continue
		}
		n++
		items = append(items, menuItem{fmt.Sprint(n), "Move to " + name, "", move(name)})
	}
	items = append(items, menuItem{"n", "New changelist…", "create one and move this file into it", func() tea.Cmd {
		m.popup = newInputPopup("New changelist", []string{"Name"}, nil, func(v []string) tea.Cmd {
			name := strings.TrimSpace(v[0])
			if name == "" {
				return nil
			}
			return move(name)()
		})
		return nil
	}})
	if f.Changelist != "" {
		cl := f.Changelist
		items = append(items,
			menuItem{"s", "Select all in " + cl, "mark its files for the next commit", func() tea.Cmd {
				m.selected = map[string]bool{}
				for _, g := range m.files {
					if g.Changelist == cl {
						m.selected[g.Path] = true
					}
				}
				m.status = fmt.Sprintf("Selected %d file(s) in %s — press c to commit", len(m.selected), cl)
				return nil
			}},
			menuItem{"x", "Remove from " + cl, "", func() tea.Cmd {
				return m.op(func(ctx context.Context) (string, error) {
					return "Removed " + f.Path + " from " + cl, m.client.RemoveChangelist(ctx, f.Path)
				})
			}},
		)
	}
	m.popup = &menuPopup{heading: "Changelist · " + f.Path, items: items}
}

// commitTargets returns selected paths, or every versioned change if
// nothing is selected.
func (m *Model) commitTargets() []string {
	var paths []string
	for _, f := range m.files {
		if m.selected[f.Path] || (len(m.selected) == 0 && committable(f)) {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

func (m *Model) startCommit(useEditor bool) tea.Cmd {
	paths := m.commitTargets()
	if len(paths) == 0 {
		m.status = "Nothing to commit"
		return nil
	}
	for _, p := range paths {
		for _, f := range m.files {
			if f.Path == p && f.Conflicted() {
				m.popup = &messagePopup{heading: "Cannot commit", text: p + " is conflicted. Resolve it first (R).", isError: true}
				return nil
			}
		}
	}
	if useEditor {
		return m.editCommitMessage(paths)
	}
	heading := fmt.Sprintf("Commit %d file(s)", len(paths))
	if len(m.selected) == 0 {
		heading = fmt.Sprintf("Commit all %d change(s)", len(paths))
	}
	m.popup = newCommitPopup(heading, func(msg string) tea.Cmd { return m.commit(msg, paths) })
	return nil
}

func (m *Model) commit(message string, paths []string) tea.Cmd {
	m.status = "Committing…"
	return m.op(func(ctx context.Context) (string, error) {
		rev, err := m.client.Commit(ctx, message, paths...)
		if err != nil {
			return "", err
		}
		// Committed paths drop out of status, which prunes the selection.
		return fmt.Sprintf("Committed r%d", rev), nil
	})
}

const editorTemplate = "\n# Write the commit message above. Lines starting with '#' are ignored.\n# An empty message aborts the commit.\n#\n# Files to commit:\n"

func (m *Model) editCommitMessage(paths []string) tea.Cmd {
	f, err := os.CreateTemp("", "seven-commit-*.txt")
	if err != nil {
		return errCmd(err)
	}
	var b strings.Builder
	b.WriteString(editorTemplate)
	for _, p := range paths {
		b.WriteString("#   " + p + "\n")
	}
	f.WriteString(b.String())
	f.Close()
	return tea.ExecProcess(m.editorCommand(f.Name()), func(err error) tea.Msg {
		return editorDoneMsg{file: f.Name(), paths: paths, err: err}
	})
}

func (m *Model) commitFromEditor(msg editorDoneMsg) tea.Cmd {
	defer os.Remove(msg.file)
	if msg.err != nil {
		return errCmd(msg.err)
	}
	data, err := os.ReadFile(msg.file)
	if err != nil {
		return errCmd(err)
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		m.status = "Commit aborted: empty message"
		return nil
	}
	return m.commit(text, msg.paths)
}

func (m *Model) filesBox(w, h int) string {
	lines := make([]string, len(m.fileRows))
	selMark := lipgloss.NewStyle().Foreground(style.Green)
	dirStyle := lipgloss.NewStyle().Foreground(style.Accent)
	for i, r := range m.fileRows {
		indent := strings.Repeat("  ", r.depth)
		if r.node.dir {
			lines[i] = " " + m.dirMark(r.node, selMark) + " " + indent + m.dirLine(r.node, dirStyle)
			continue
		}
		f := *r.node.file
		mark := " "
		if m.selected[f.Path] {
			mark = selMark.Render("●")
		}
		lines[i] = fmt.Sprintf(" %s %s%s %s", mark, indent, statusCode(f), r.node.name) + fileSuffix(f)
	}
	if len(lines) == 0 {
		lines = []string{style.Dim.Render(" clean")}
	}
	title := fmt.Sprintf("[2] Files (%d)", len(m.files))
	if n := len(m.selected); n > 0 {
		title = fmt.Sprintf("[2] Files (%d, %d selected)", len(m.files), n)
	}
	if m.remoteStatus {
		title += fmt.Sprintf(" · vs r%d", m.against)
	}
	focused := m.focus == panelFiles
	return style.Box(title, m.fileList.render(lines, w-2, h-2, focused), w, h, focused)
}

// statusCode renders the three status columns: text, props, tree conflict.
func statusCode(f svn.FileStatus) string {
	props := " "
	if f.Props == svn.ItemModified || f.Props == svn.ItemConflicted {
		props = f.Props.Code()
	}
	tree := " "
	if f.TreeConflict {
		tree = "C"
	}
	return style.ItemStyle(f).Render(f.Item.Code() + props + tree)
}

func fileSuffix(f svn.FileStatus) string {
	// The changelist is shown by the section the file is listed under.
	var s string
	warn := lipgloss.NewStyle().Foreground(style.Yellow)
	switch {
	case f.Locked:
		s += warn.Render(" (locked by me)")
	case f.RemoteLockOwner != "":
		s += warn.Render(" (locked by " + f.RemoteLockOwner + ")")
	}
	if f.RemoteChanged {
		// Incoming change from the server (svn status -u marks these with *).
		s += warn.Render(" ↓" + f.RemoteItem.Code())
	}
	return s
}

// dirMark shows whether a folder's versioned changes are all (●) or partly
// (◐) selected.
func (m *Model) dirMark(n *treeNode, sel lipgloss.Style) string {
	total, picked := 0, 0
	for _, f := range filesUnder(n) {
		if committable(f) {
			total++
			if m.selected[f.Path] {
				picked++
			}
		}
	}
	switch {
	case picked == 0:
		return " "
	case picked == total:
		return sel.Render("●")
	}
	return sel.Render("◐")
}

// dirLine renders a folder row: fold arrow, its own status (if the folder
// itself is added/modified) and the name. A conflict below is flagged.
func (m *Model) dirLine(n *treeNode, dirStyle lipgloss.Style) string {
	arrow := "▼"
	if m.collapsed[n.key] {
		arrow = "▶"
	}
	if n.group {
		label := lipgloss.NewStyle().Foreground(style.Magenta).Bold(true).Render(groupLabel(n))
		return fmt.Sprintf("%s %s %s", arrow, label, style.Dim.Render(fmt.Sprintf("(%d)", len(filesUnder(n)))))
	}
	line := arrow + " "
	if n.file != nil {
		line += statusCode(*n.file) + " "
	}
	line += dirStyle.Render(n.name + "/")
	if m.collapsed[n.key] {
		line += style.Dim.Render(fmt.Sprintf(" (%d)", len(filesUnder(n))))
	}
	for _, f := range filesUnder(n) {
		if f.Conflicted() {
			line += style.ErrorText.Render(" !")
			break
		}
	}
	return line
}
