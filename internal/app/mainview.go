package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

// syncMain loads whatever the main view should show for the current
// left-panel selection, skipping the load if it is already shown.
func (m *Model) syncMain() tea.Cmd {
	// The focused main view keeps what it was opened with, unless an
	// operation invalidated it (mainKey cleared), e.g. after a hunk revert.
	if m.focus == panelMain && (m.mainKey != "" || m.mainMode()) {
		return nil
	}
	switch m.lastLeft {
	case panelFiles:
		if dir, ok := m.currentDir(); ok {
			key := "dir:" + dir.key
			if key == m.mainKey {
				return nil
			}
			m.mainKey, m.mainTitle = key, "Diff · "+dir.key+"/"
			if dir.group {
				m.mainTitle = "Diff · " + groupLabel(dir)
			}
			m.setMainText("Loading…")
			return m.loadDirDiff(key, filesUnder(dir))
		}
		f, ok := m.currentFile()
		if !ok {
			m.mainKey, m.mainTitle = "", "Diff"
			m.setMainText("Working copy is clean.")
			return nil
		}
		if f.Path == m.mainKey {
			return nil
		}
		m.mainKey, m.mainTitle = f.Path, "Diff · "+f.Path
		m.setMainText("Loading…")
		return m.loadFileDiff(f)
	case panelBranches:
		b, ok := m.currentBranch()
		if !ok {
			m.mainKey, m.mainTitle = "", "Branches"
			m.setMainText(m.branchErr)
			return nil
		}
		m.mainKey, m.mainTitle = "branch:"+b.URL, "Branch · "+branchLabel(b)
		m.setMainText(m.branchDetail(b))
	case panelLog:
		r, ok := m.currentRev()
		if !ok || m.info == nil {
			return nil
		}
		key := fmt.Sprintf("r%d", r.Number)
		if key == m.mainKey {
			return nil
		}
		m.mainKey, m.mainTitle = key, fmt.Sprintf("Revision r%d", r.Number)
		m.setMainText("Loading…")
		return m.loadRevisionDiff(r, m.info.RepoRoot)
	case panelStatus:
		m.mainKey, m.mainTitle = "status", "Status"
		m.setMainText(m.statusDetail())
	}
	return nil
}

// setMainText shows text (diff-coloured where it looks like a diff).
func (m *Model) setMainText(text string) { m.setMainDiff(text, "") }

// setMainDiff shows text; file is set for a working-copy diff, which
// enables hunk selection. The selected hunk survives a reload of the
// same file (e.g. after reverting one hunk).
func (m *Model) setMainDiff(text, file string) {
	keepHunk := file != "" && file == m.mainFile && m.mainHunk >= 0
	m.mainRaw = svn.ParseDiff(text)
	m.mainLines = make([]string, len(m.mainRaw))
	for i, l := range m.mainRaw {
		m.mainLines[i] = style.DiffLine(l)
	}
	m.mainFile = file
	m.mainHunks = nil
	if file != "" {
		m.mainHunks = svn.DiffHunks(m.mainRaw)
	}
	m.mainScroll, m.mainHScroll = 0, 0
	m.resetDiffView()
	if keepHunk && len(m.mainHunks) > 0 {
		m.mainHunk = min(m.mainHunk, len(m.mainHunks)-1)
		m.scrollToMainHunk()
	} else {
		m.mainHunk = -1
	}
}

func (m *Model) scrollToMainHunk() {
	if m.mainHunk < 0 || m.mainHunk >= len(m.mainHunks) {
		return
	}
	h := m.mainHunks[m.mainHunk]
	m.scrollToRaw(h.Start, h.End)
}

func (m *Model) stepHunk(step int) {
	if len(m.mainHunks) == 0 {
		return
	}
	if m.mainHunk < 0 {
		m.mainHunk = 0
	} else {
		m.mainHunk = (m.mainHunk + step + len(m.mainHunks)) % len(m.mainHunks)
	}
	m.scrollToMainHunk()
}

func (m *Model) handleMainKey(key string) tea.Cmd {
	page := max(1, m.mainHeight()-2)
	switch key {
	case "j", "down":
		m.mainScroll++
	case "k", "up":
		m.mainScroll--
	case "ctrl+d", "pgdown", " ":
		m.mainScroll += page / 2
	case "ctrl+u", "pgup":
		m.mainScroll -= page / 2
	case "g", "home":
		m.mainScroll = 0
	case "G", "end":
		m.mainScroll = len(m.mainRows)
	case "h", "left":
		m.mainHScroll = max(0, m.mainHScroll-8)
	case "l", "right":
		m.mainHScroll += 8
	case "v":
		m.toggleSideBySide()
		return nil
	case "/":
		p := newInputPopup("Search diff", []string{"Find"}, nil, func(v []string) tea.Cmd {
			m.setSearch(v[0])
			return nil
		})
		p.inputs[0].SetValue(m.mainSearch)
		m.popup = p
		return nil
	case "n", "N":
		// n/N step through search matches; without a search they step hunks.
		step := 1
		if key == "N" {
			step = -1
		}
		if m.mainSearch != "" {
			m.nextMatch(step)
		} else {
			m.stepHunk(step)
		}
		return nil
	case "]":
		m.stepHunk(1)
		return nil
	case "[", "p":
		m.stepHunk(-1)
		return nil
	case "z":
		m.toggleFold()
		return nil
	case "Z":
		m.toggleFoldAll()
		return nil
	case "}":
		m.jumpFile(1)
		return nil
	case "{":
		m.jumpFile(-1)
		return nil
	case "d":
		return m.confirmRevertHunk()
	case "esc":
		if m.mainSearch != "" {
			m.setSearch("")
			m.status = "Search cleared"
			return nil
		}
		return m.setFocus(m.lastLeft)
	case "enter":
		return m.setFocus(m.lastLeft)
	}
	m.clampMainScroll()
	return nil
}

func (m *Model) confirmRevertHunk() tea.Cmd {
	if m.mainFile == "" || len(m.mainHunks) == 0 {
		return nil
	}
	if m.mainHunk < 0 {
		m.status = "Select a hunk with ] / [ first"
		return nil
	}
	h, file := m.mainHunks[m.mainHunk], m.mainFile
	patch, err := svn.HunkPatch(m.mainRaw, h)
	if err != nil {
		return errCmd(err)
	}
	n := m.mainHunk + 1
	m.popup = &confirmPopup{
		heading: "Revert hunk",
		text: fmt.Sprintf("Discard hunk %d of %d in %s?\n\n%s\n\nA backup is saved first.",
			n, len(m.mainHunks), file, strings.TrimSpace(m.mainRaw[h.Start].Text)),
		onYes: m.op(func(ctx context.Context) (string, error) {
			dir, err := m.backup([]string{file}, patch)
			if err != nil {
				return "", fmt.Errorf("backup failed, nothing reverted: %w", err)
			}
			pf, err := os.CreateTemp("", "seven-hunk-*.patch")
			if err != nil {
				return "", err
			}
			defer os.Remove(pf.Name())
			pf.WriteString(patch)
			pf.Close()
			if _, err := m.client.Patch(ctx, pf.Name(), svn.PatchOptions{Reverse: true}); err != nil {
				return "", err
			}
			return withBackup(fmt.Sprintf("Reverted hunk %d of %s", n, file), dir), nil
		}),
	}
	return nil
}

func (m *Model) mainBox(w, h int) string {
	inner := h - 2
	end := min(len(m.mainRows), m.mainScroll+inner)
	gutter := m.mainFile != "" && len(m.mainHunks) > 0
	var sel svn.DiffHunk
	if m.mainHunk >= 0 && m.mainHunk < len(m.mainHunks) {
		sel = m.mainHunks[m.mainHunk]
	} else {
		sel = svn.DiffHunk{Start: -1, End: -2}
	}
	marker := lipgloss.NewStyle().Foreground(style.Yellow).Bold(true).Render("▌")
	width := w - 2
	if gutter {
		width--
	}
	var lines []string
	for v := min(m.mainScroll, end); v < end; v++ {
		r := m.mainRows[v]
		i := r.raw
		var l string
		if r.full {
			l = m.renderMainLine(i)
			if m.mainHScroll > 0 {
				l = ansi.Cut(l, m.mainHScroll, m.mainHScroll+width)
			}
		} else {
			l = m.renderSideBySide(r, (width-1)/2)
		}
		if gutter {
			if i >= sel.Start && i <= sel.End {
				l = marker + l
			} else {
				l = " " + l
			}
		}
		lines = append(lines, l)
	}
	title := m.mainTitle
	if title == "" {
		title = "Main"
	}
	if m.ignoreWhitespace && m.lastLeft == panelFiles {
		title += " (ignoring whitespace)"
	}
	if n := len(m.mainSections); n > 1 {
		folded := 0
		for i := range m.mainSections {
			if m.mainFolded[i] {
				folded++
			}
		}
		title += fmt.Sprintf(" · file %d/%d", m.mainCurFile+1, n)
		if folded > 0 {
			title += fmt.Sprintf(" (%d folded)", folded)
		}
	}
	if gutter && m.mainHunk >= 0 {
		title += fmt.Sprintf(" · hunk %d/%d", m.mainHunk+1, len(m.mainHunks))
	}
	if m.mainSearch != "" {
		cur := 0
		if m.mainMatch >= 0 {
			cur = m.mainMatch + 1
		}
		title += fmt.Sprintf(" · /%s %d/%d", m.mainSearch, cur, len(m.mainMatches))
	}
	if len(m.mainRows) > inner {
		title += fmt.Sprintf(" %d%%", min(100, (m.mainScroll+inner)*100/len(m.mainRows)))
	}
	return style.Box(title, lines, w, h, m.focus == panelMain)
}

// showRevision displays rev in the main view and selects it in the Log
// panel if it is loaded there.
func (m *Model) showRevision(rev svn.Revision) tea.Cmd {
	if m.info == nil {
		return nil
	}
	m.blame = nil
	for i, r := range m.revs {
		if r.Number == rev.Number {
			m.logList.set(i, len(m.revs))
		}
	}
	m.mainKey, m.mainTitle = fmt.Sprintf("r%d", rev.Number), fmt.Sprintf("Revision r%d", rev.Number)
	m.setMainText("Loading…")
	m.focus = panelMain
	return m.loadRevisionDiff(rev, m.info.RepoRoot)
}

// blameView annotates a file in the main view.
type blameView struct {
	path    string
	lines   []svn.BlameLine
	loading bool
	cursor  int
	scroll  int
}

func (m *Model) openBlame(path string) tea.Cmd {
	m.conflict = nil
	m.blame = &blameView{path: path, loading: true}
	m.focus = panelMain
	return m.loadBlame(path)
}

func (m *Model) handleBlameKey(key string) (bool, tea.Cmd) {
	b := m.blame
	inner := m.mainHeight() - 2
	n := len(b.lines)
	switch key {
	case "j", "down":
		b.cursor = min(b.cursor+1, max(0, n-1))
	case "k", "up":
		b.cursor = max(0, b.cursor-1)
	case "ctrl+d", "pgdown", " ":
		b.cursor = min(b.cursor+inner/2, max(0, n-1))
	case "ctrl+u", "pgup":
		b.cursor = max(0, b.cursor-inner/2)
	case "g", "home":
		b.cursor = 0
	case "G", "end":
		b.cursor = max(0, n-1)
	case "enter":
		if b.cursor >= n || m.info == nil {
			return true, nil
		}
		if b.lines[b.cursor].Revision == 0 {
			m.status = "That line is an uncommitted local change"
			return true, nil
		}
		return true, m.loadRevision(m.info.RepoRoot, b.lines[b.cursor].Revision)
	case "esc":
		return true, m.setFocus(panelFiles)
	default:
		return false, nil
	}
	if b.cursor < b.scroll {
		b.scroll = b.cursor
	}
	if b.cursor >= b.scroll+inner {
		b.scroll = b.cursor - inner + 1
	}
	return true, nil
}

func (m *Model) blameBox(w, h int) string {
	b := m.blame
	title := "Blame · " + b.path
	if b.loading {
		return style.Box(title, []string{style.Dim.Render(" loading…")}, w, h, true)
	}
	revW := 1
	for _, l := range b.lines {
		revW = max(revW, len(fmt.Sprint(l.Revision)))
	}
	revStyle := lipgloss.NewStyle().Foreground(style.Yellow)
	authorStyle := lipgloss.NewStyle().Foreground(style.Cyan)
	var out []string
	for i := b.scroll; i < len(b.lines) && len(out) < h-2; i++ {
		l := b.lines[i]
		// Only the first line of a run from the same revision is labelled.
		annW := revW + 1 + 1 + 10 + 1 + 10
		var ann string
		switch {
		case i != b.scroll && b.lines[i-1].Revision == l.Revision:
			ann = strings.Repeat(" ", annW)
		case l.Revision == 0:
			ann = lipgloss.NewStyle().Foreground(style.Green).Render(fmt.Sprintf("%-*s", annW, "uncommitted"))
		default:
			ann = revStyle.Render(fmt.Sprintf("r%-*d", revW, l.Revision)) + " " +
				authorStyle.Render(truncate(l.Author, 10)) + " " + style.Dim.Render(l.Date.Local().Format("2006-01-02"))
		}
		line := fmt.Sprintf("%s %s %s", ann, style.Dim.Render("│"), style.ExpandTabs(l.Text))
		if i == b.cursor {
			line = style.Selected.Render(style.Pad(ansi.Strip(line), w-2))
		}
		out = append(out, line)
	}
	return style.Box(title, out, w, h, true)
}
