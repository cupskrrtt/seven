package app

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

// conflictView is the per-hunk conflict picker shown in the main view.
// Every pick is written to disk immediately; undo restores earlier content.
type conflictView struct {
	path   string // working-copy relative, for svn
	abs    string
	lines  []string
	hunks  []svn.ConflictHunk
	cur    int
	scroll int
	undo   [][]string
}

// resolveMenu offers the resolution strategies that apply to f.
func (m *Model) resolveMenu(f svn.FileStatus) {
	resolve := func(accept, done string) func() tea.Cmd {
		return func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) {
				return done + " " + f.Path, m.client.Resolve(ctx, accept, f.Path)
			})
		}
	}
	// Choices that throw away local edits ask first.
	confirmed := func(text string, next func() tea.Cmd) func() tea.Cmd {
		return func() tea.Cmd {
			m.popup = &confirmPopup{heading: "Discard local changes", text: text, onYes: next()}
			return nil
		}
	}

	var items []menuItem
	text := f.Item == svn.ItemConflicted
	if text {
		items = append(items, menuItem{"p", "Pick hunks…", "choose mine / theirs / both for each conflict",
			func() tea.Cmd { return m.openConflict(f) }})
	}
	if text || f.Props == svn.ItemConflicted {
		items = append(items,
			menuItem{"m", "Mine (full)", "keep my version entirely, drop incoming changes",
				resolve("mine-full", "Kept mine for")},
			menuItem{"t", "Theirs (full)", "take the incoming version entirely, drop my changes",
				confirmed(fmt.Sprintf("Replace %s with the incoming version? Your local changes to it are lost.", f.Path),
					resolve("theirs-full", "Took theirs for"))},
		)
	}
	if text || f.TreeConflict {
		items = append(items,
			menuItem{"M", "Mine for conflicts", "merge both, keep mine where they conflict",
				resolve("mine-conflict", "Kept mine (conflicts) for")},
			menuItem{"T", "Theirs for conflicts", "merge both, take theirs where they conflict",
				confirmed(fmt.Sprintf("Take theirs for every conflicting hunk in %s? Your side of those hunks is lost.", f.Path),
					resolve("theirs-conflict", "Took theirs (conflicts) for"))},
		)
	}
	if text || f.Props == svn.ItemConflicted {
		items = append(items, menuItem{"B", "Base", "restore the common ancestor, drop both sides",
			confirmed(fmt.Sprintf("Restore the base version of %s? Both your and the incoming changes are lost.", f.Path),
				resolve("base", "Restored base for"))})
	}
	items = append(items,
		menuItem{"r", "Mark resolved", "accept the file as it is now (after manual edits)",
			resolve("working", "Resolved")},
		menuItem{"e", "Open in editor", "edit the conflict markers by hand",
			func() tea.Cmd { return m.openEditor(m.abs(f.Path)) }},
	)
	m.popup = &menuPopup{heading: "Resolve " + f.Path, items: items}
}

// openConflict loads f into the hunk picker.
func (m *Model) openConflict(f svn.FileStatus) tea.Cmd {
	abs := m.abs(f.Path)
	data, err := os.ReadFile(abs)
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	lines := svn.SplitLines(string(data))
	hunks := svn.ParseConflicts(lines)
	if len(hunks) == 0 {
		m.status = "No conflict markers left in " + f.Path + " — press R → r to mark it resolved"
		return nil
	}
	m.conflict = &conflictView{path: f.Path, abs: abs, lines: lines, hunks: hunks}
	m.focus = panelMain
	return nil
}

func (m *Model) closeConflict() tea.Cmd { return m.setFocus(panelFiles) }

// reloadConflict re-reads the file after it was edited outside the picker.
func (m *Model) reloadConflict() {
	c := m.conflict
	data, err := os.ReadFile(c.abs)
	if err != nil {
		return
	}
	c.lines = svn.SplitLines(string(data))
	c.hunks = svn.ParseConflicts(c.lines)
	c.cur = max(0, min(c.cur, len(c.hunks)-1))
	c.scroll = min(c.scroll, max(0, len(c.lines)-1))
}

// handleConflictKey processes picker keys. handled=false lets the key fall
// through to the global bindings.
func (m *Model) handleConflictKey(key string) (handled bool, cmd tea.Cmd) {
	c := m.conflict
	inner := m.mainHeight() - 2
	if len(c.hunks) == 0 {
		// Everything is picked; only scrolling, undo and leaving make sense.
		switch key {
		case "n", "tab", "right", "l", "p", "N", "shift+tab", "left", "h", "m", "o", "t", "b", "B":
			m.status = "No conflicts left — z to undo, esc to go back"
			return true, nil
		}
	}
	switch key {
	case "j", "down":
		c.scroll = min(c.scroll+1, max(0, len(c.lines)-inner))
	case "k", "up":
		c.scroll = max(0, c.scroll-1)
	case "n", "tab", "right", "l":
		c.cur = (c.cur + 1) % len(c.hunks)
		m.scrollToHunk()
	case "p", "N", "shift+tab", "left", "h":
		c.cur = (c.cur + len(c.hunks) - 1) % len(c.hunks)
		m.scrollToHunk()
	case "m", "o":
		return true, m.pick(svn.ChooseMine)
	case "t":
		return true, m.pick(svn.ChooseTheirs)
	case "b":
		return true, m.pick(svn.ChooseBoth)
	case "B":
		if c.hunks[c.cur].Base < 0 {
			m.status = "This conflict has no base section"
			return true, nil
		}
		return true, m.pick(svn.ChooseBase)
	case "z", "ctrl+z":
		if len(c.undo) == 0 {
			m.status = "Nothing to undo"
			return true, nil
		}
		prev := c.undo[len(c.undo)-1]
		c.undo = c.undo[:len(c.undo)-1]
		return true, m.writeConflict(prev, "Undone")
	case "e":
		return true, m.openEditor(c.abs)
	case "esc", "enter":
		return true, m.closeConflict()
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) pick(choice svn.Choice) tea.Cmd {
	c := m.conflict
	c.undo = append(c.undo, c.lines)
	names := map[svn.Choice]string{svn.ChooseMine: "mine", svn.ChooseTheirs: "theirs",
		svn.ChooseBoth: "both", svn.ChooseBase: "base"}
	return m.writeConflict(svn.ResolveHunk(c.lines, c.hunks[c.cur], choice),
		fmt.Sprintf("Kept %s for conflict %d", names[choice], c.cur+1))
}

// writeConflict saves lines to disk and refreshes the picker. When no
// conflicts remain it offers to mark the file resolved.
func (m *Model) writeConflict(lines []string, status string) tea.Cmd {
	c := m.conflict
	mode := os.FileMode(0o644)
	if st, err := os.Stat(c.abs); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(c.abs, []byte(svn.JoinLines(lines)), mode); err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	c.lines = lines
	c.hunks = svn.ParseConflicts(lines)
	m.status = status
	if len(c.hunks) == 0 {
		path := c.path
		m.popup = &confirmPopup{
			heading: "All conflicts picked",
			text: fmt.Sprintf("No conflicts left in %s. Mark it resolved now?\n\n"+
				"(Choose no to keep reviewing; z undoes the last pick.)", path),
			onYesFn: func() tea.Cmd {
				return tea.Batch(m.closeConflict(), m.op(func(ctx context.Context) (string, error) {
					return "Resolved " + path, m.client.Resolve(ctx, "working", path)
				}))
			},
		}
		return nil
	}
	c.cur = min(c.cur, len(c.hunks)-1)
	m.scrollToHunk()
	return nil
}

// scrollToHunk keeps the current hunk on screen with a little context.
func (m *Model) scrollToHunk() {
	c := m.conflict
	if len(c.hunks) == 0 {
		return
	}
	h := c.hunks[c.cur]
	inner := m.mainHeight() - 2
	if h.Start < c.scroll || h.End >= c.scroll+inner {
		c.scroll = max(0, h.Start-3)
	}
}

func (m *Model) conflictBox(w, h int) string {
	c := m.conflict
	mine := lipgloss.NewStyle().Foreground(style.Green)
	theirs := lipgloss.NewStyle().Foreground(style.Cyan)
	marker := style.Dim
	current := lipgloss.NewStyle().Foreground(style.Yellow).Bold(true)

	// Classify every line once: which hunk it is in and which section.
	type kind int
	const (
		plain kind = iota
		markerLine
		mineLine
		baseLine
		theirsLine
	)
	kinds := make([]kind, len(c.lines))
	hunkOf := make([]int, len(c.lines))
	for i := range hunkOf {
		hunkOf[i] = -1
	}
	for hi, hk := range c.hunks {
		section := mineLine
		for i := hk.Start; i <= hk.End; i++ {
			hunkOf[i] = hi
			switch i {
			case hk.Start, hk.End:
				kinds[i] = markerLine
			case hk.Base:
				kinds[i], section = markerLine, baseLine
			case hk.Sep:
				kinds[i], section = markerLine, theirsLine
			default:
				kinds[i] = section
			}
		}
	}

	var out []string
	for i := c.scroll; i < len(c.lines) && len(out) < h-2; i++ {
		text := style.ExpandTabs(trimCR(c.lines[i]))
		gutter := "  "
		if hunkOf[i] == c.cur {
			gutter = current.Render("▌ ")
		}
		switch kinds[i] {
		case markerLine:
			label := ""
			switch {
			case i == c.hunks[hunkOf[i]].Start:
				label = "  ← mine (m)"
			case i == c.hunks[hunkOf[i]].End:
				label = "  ← theirs (t)"
			case i == c.hunks[hunkOf[i]].Base:
				label = "  ← base (B)"
			}
			s := marker
			if hunkOf[i] == c.cur {
				s = current
			}
			text = s.Render(text + label)
		case mineLine:
			text = mine.Render(text)
		case theirsLine:
			text = theirs.Render(text)
		case baseLine:
			text = style.Dim.Render(text)
		}
		out = append(out, gutter+text)
	}
	title := fmt.Sprintf("Conflict · %s · %d/%d", c.path, c.cur+1, len(c.hunks))
	if len(c.hunks) == 0 {
		title = "Conflict · " + c.path + " · all picked"
	}
	return style.Box(title, out, w, h, true)
}

func trimCR(s string) string {
	if n := len(s); n > 0 && s[n-1] == '\r' {
		return s[:n-1]
	}
	return s
}
