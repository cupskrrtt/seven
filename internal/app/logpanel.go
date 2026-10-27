package app

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

func (m *Model) currentRev() (svn.Revision, bool) {
	if m.logList.cursor < len(m.revs) {
		return m.revs[m.logList.cursor], true
	}
	return svn.Revision{}, false
}

// ownLog reports whether the log shows the working copy's own branch.
func (m *Model) ownLog() bool { return m.logTarget == "" }

// logURL is what the Log panel shows history for.
func (m *Model) logURL() string {
	if m.logTarget != "" {
		return m.logTarget
	}
	if m.info == nil {
		return ""
	}
	if m.logPath == "" {
		return m.info.URL
	}
	segs := strings.Split(filepath.ToSlash(m.logPath), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return m.info.URL + "/" + strings.Join(segs, "/")
}

// reloadLog restarts the log from the top with the current target/filters.
func (m *Model) reloadLog() tea.Cmd {
	if m.info == nil {
		return nil
	}
	m.logGen++
	m.revs, m.logDone, m.logLoading = nil, false, true
	m.logList.set(0, 0)
	m.mainKey = ""
	return m.loadLog(0)
}

// setLogPath shows the history of one working-copy path.
func (m *Model) setLogPath(path string) tea.Cmd {
	m.logTarget, m.logLabel, m.logPath = "", "", path
	cmd := m.reloadLog()
	return tea.Batch(cmd, m.setFocus(panelLog))
}

func (m *Model) handleLogKey(key string) tea.Cmd {
	switch key {
	case "j", "down":
		m.logList.move(1, len(m.revs))
	case "k", "up":
		m.logList.move(-1, len(m.revs))
	case "g", "home":
		m.logList.set(0, len(m.revs))
	case "G", "end":
		m.logList.set(len(m.revs)-1, len(m.revs))
	case "enter":
		return m.setFocus(panelMain)
	case "/":
		m.logFilterPopup()
		return nil
	case "s":
		m.logOpts.StopOnCopy = !m.logOpts.StopOnCopy
		return m.reloadLog()
	case "esc":
		if m.logTarget == "" && m.logPath == "" && m.logOpts == (svn.LogOptions{}) {
			return nil
		}
		m.logTarget, m.logLabel, m.logPath, m.logOpts = "", "", "", svn.LogOptions{}
		m.status = "Log reset to the working copy"
		return m.reloadLog()
	case "c":
		return m.cherryPick()
	case "R":
		return m.revertRevision()
	default:
		return nil
	}
	return tea.Batch(m.syncMain(), m.maybeLoadMoreLog())
}

// maybeLoadMoreLog fetches the next page when the cursor nears the end.
func (m *Model) maybeLoadMoreLog() tea.Cmd {
	if m.logLoading || m.logDone || len(m.revs) == 0 || m.logList.cursor < len(m.revs)-10 {
		return nil
	}
	next := m.revs[len(m.revs)-1].Number - 1
	if next < 1 || (m.logOpts.To > 0 && next < m.logOpts.To) {
		m.logDone = true
		return nil
	}
	m.logLoading = true
	return m.loadLog(next)
}

func (m *Model) logFilterPopup() {
	p := newInputPopup("Filter log", []string{"Search (author, message, path)", "Path", "Revisions (e.g. 120:80)"}, nil,
		func(v []string) tea.Cmd {
			from, to, err := parseRange(v[2])
			if err != nil {
				return errCmd(err)
			}
			m.logOpts.Search = strings.TrimSpace(v[0])
			m.logOpts.From, m.logOpts.To = from, to
			if path := strings.TrimSpace(v[1]); path != m.logPath {
				m.logTarget, m.logLabel, m.logPath = "", "", path
			}
			return m.reloadLog()
		})
	p.inputs[0].SetValue(m.logOpts.Search)
	p.inputs[1].SetValue(m.logPath)
	if m.logOpts.From > 0 || m.logOpts.To > 0 {
		p.inputs[2].SetValue(fmt.Sprintf("%d:%d", m.logOpts.From, m.logOpts.To))
	}
	m.popup = p
}

// parseRange accepts "", "N", "A:B" or "A-B" and returns newest, oldest.
func parseRange(s string) (from, to int, err error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "r", "")
	if s == "" {
		return 0, 0, nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ':' || r == '-' })
	if len(parts) == 0 || len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid revision range %q", s)
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		if nums[i], err = strconv.Atoi(p); err != nil || nums[i] <= 0 {
			return 0, 0, fmt.Errorf("invalid revision range %q", s)
		}
	}
	if len(nums) == 1 {
		return nums[0], nums[0], nil
	}
	return max(nums[0], nums[1]), min(nums[0], nums[1]), nil
}

func (m *Model) cherryPick() tea.Cmd {
	r, ok := m.currentRev()
	if !ok {
		return nil
	}
	if m.ownLog() {
		m.status = fmt.Sprintf("r%d is already on this branch — open another branch's log (Branches → enter) to cherry-pick", r.Number)
		return nil
	}
	src, label := m.logTarget, m.logLabel
	m.popup = &confirmPopup{
		heading: "Cherry-pick",
		text: fmt.Sprintf("Merge r%d from %s into the working copy?\n\n  %s\n\nNothing is committed: review the result, then commit it.",
			r.Number, label, r.Summary()),
		onYes: m.op(func(ctx context.Context) (string, error) {
			res, err := m.client.CherryPick(ctx, src, r.Number)
			if err != nil {
				return "", err
			}
			return describeUpdate(fmt.Sprintf("Cherry-picked r%d — review and commit", r.Number), res), nil
		}),
	}
	return nil
}

func (m *Model) revertRevision() tea.Cmd {
	r, ok := m.currentRev()
	if !ok || m.info == nil {
		return nil
	}
	if !m.ownLog() {
		m.status = "Revert works on the working copy's own history — press esc to go back to it"
		return nil
	}
	src := m.info.URL
	m.popup = &confirmPopup{
		heading: "Revert revision",
		text: fmt.Sprintf("Undo all of r%d in the working copy (reverse merge)?\n\n  %s\n\n"+
			"History is kept: commit the result to record the undo.", r.Number, r.Summary()),
		onYes: m.op(func(ctx context.Context) (string, error) {
			res, err := m.client.RevertRevision(ctx, src, r.Number)
			if err != nil {
				return "", err
			}
			return describeUpdate(fmt.Sprintf("Reverted r%d in the working copy — review and commit", r.Number), res), nil
		}),
	}
	return nil
}

// logUpdateMenu offers updating to the selected revision.
func (m *Model) logUpdateMenu() tea.Cmd {
	r, ok := m.currentRev()
	if !ok {
		return nil
	}
	if !m.ownLog() {
		m.status = "That revision belongs to another branch — switch to it in Branches first"
		return nil
	}
	rev := r.Number
	items := []menuItem{{"w", fmt.Sprintf("Working copy → r%d", rev), "update everything to this revision", func() tea.Cmd {
		m.status = fmt.Sprintf("Updating to r%d…", rev)
		return m.op(func(ctx context.Context) (string, error) { return m.updateTo(ctx, rev) })
	}}}
	if path := m.logPath; path != "" {
		items = append([]menuItem{{"f", fmt.Sprintf("%s → r%d", path, rev), "update only this path (u brings it back)", func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) { return m.updateTo(ctx, rev, path) })
		}}}, items...)
	}
	m.popup = &menuPopup{heading: fmt.Sprintf("Update to r%d", rev), items: items}
	return nil
}

func (m *Model) logTitle() string {
	var parts []string
	if m.logLabel != "" {
		parts = append(parts, m.logLabel)
	}
	if m.logPath != "" {
		parts = append(parts, m.logPath)
	}
	if m.logOpts.Search != "" {
		parts = append(parts, fmt.Sprintf("%q", m.logOpts.Search))
	}
	if m.logOpts.From > 0 {
		parts = append(parts, fmt.Sprintf("r%d:%d", m.logOpts.From, m.logOpts.To))
	}
	if m.logOpts.StopOnCopy {
		parts = append(parts, "stop-on-copy")
	}
	if len(parts) == 0 {
		return "[4] Log"
	}
	return "[4] Log · " + strings.Join(parts, " · ")
}

func (m *Model) logBox(w, h int) string {
	lines := make([]string, len(m.revs))
	revStyle := lipgloss.NewStyle().Foreground(style.Yellow)
	authorStyle := lipgloss.NewStyle().Foreground(style.Cyan)
	for i, r := range m.revs {
		marker := " "
		if m.ownLog() && m.info != nil && r.Number > m.info.Revision {
			marker = lipgloss.NewStyle().Foreground(style.Yellow).Render("↓")
		}
		lines[i] = fmt.Sprintf("%s%s %s %s %s", marker, revStyle.Render(fmt.Sprintf("r%-5d", r.Number)),
			style.Dim.Render(r.Date.Local().Format("01-02")), authorStyle.Render(truncate(r.Author, 10)), r.Summary())
	}
	switch {
	case m.logLoading:
		lines = append(lines, style.Dim.Render(" loading…"))
	case len(lines) == 0:
		lines = []string{style.Dim.Render(" no matching revisions")}
	}
	focused := m.focus == panelLog
	return style.Box(m.logTitle(), m.logList.render(lines, w-2, h-2, focused), w, h, focused)
}
