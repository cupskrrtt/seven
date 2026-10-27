package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

func (m *Model) currentBranch() (svn.Branch, bool) {
	if m.branchList.cursor < len(m.branches) {
		return m.branches[m.branchList.cursor], true
	}
	return svn.Branch{}, false
}

// isCurrent reports whether the working copy is on b.
func (m *Model) isCurrent(b svn.Branch) bool {
	loc := m.location()
	return loc.HasLayout() && b.Kind == loc.Kind && b.Name == loc.Name
}

// branchTarget is b's URL plus the sub-path the working copy is rooted at,
// so a working copy of trunk/src switches/merges with branches/x/src.
func (m *Model) branchTarget(b svn.Branch) string {
	return b.URL + m.location().Sub
}

func branchLabel(b svn.Branch) string {
	switch b.Kind {
	case svn.KindTrunk:
		return "trunk"
	case svn.KindTag:
		return "tag " + b.Name
	}
	return b.Name
}

func (m *Model) handleBranchesKey(key string) tea.Cmd {
	n := len(m.branches)
	switch key {
	case "j", "down":
		m.branchList.move(1, n)
		return m.syncMain()
	case "k", "up":
		m.branchList.move(-1, n)
		return m.syncMain()
	case "g", "home":
		m.branchList.set(0, n)
		return m.syncMain()
	case "G", "end":
		m.branchList.set(n-1, n)
		return m.syncMain()
	case "n":
		return m.createBranchPopup(svn.KindBranch)
	case "t":
		return m.createBranchPopup(svn.KindTag)
	}
	b, ok := m.currentBranch()
	if !ok {
		return nil
	}
	switch key {
	case "enter":
		// Show this branch's history in the Log panel.
		m.logTarget, m.logLabel = m.branchTarget(b), branchLabel(b)
		if m.isCurrent(b) {
			m.logTarget, m.logLabel = "", ""
		}
		m.logPath = ""
		cmd := m.reloadLog()
		return tea.Batch(cmd, m.setFocus(panelLog))
	case " ":
		if m.isCurrent(b) {
			m.status = "Already on " + branchLabel(b)
			return nil
		}
		text := fmt.Sprintf("Switch the working copy to %s?\n\nLocal changes are kept and carried over.", branchLabel(b))
		if b.Kind == svn.KindTag {
			text += "\n\nNote: tags are meant to be read-only; don't commit on them."
		}
		m.popup = &confirmPopup{heading: "Switch", text: text, onYes: m.op(func(ctx context.Context) (string, error) {
			res, err := m.client.Switch(ctx, m.branchTarget(b))
			if err != nil {
				return "", err
			}
			return describeUpdate(fmt.Sprintf("Switched to %s at r%d", branchLabel(b), res.Revision), res), nil
		})}
	case "m":
		if m.isCurrent(b) {
			m.status = "Can't merge a branch into itself"
			return nil
		}
		m.popup = &confirmPopup{
			heading: "Merge",
			text: fmt.Sprintf("Merge all unmerged changes from %s into the working copy (%s)?\n\n"+
				"Nothing is committed: review the result, then commit it.", branchLabel(b), m.location()),
			onYes: m.op(func(ctx context.Context) (string, error) {
				res, err := m.client.Merge(ctx, m.branchTarget(b))
				if err != nil {
					return "", err
				}
				return describeUpdate("Merged "+branchLabel(b)+" — review and commit", res), nil
			}),
		}
	case "e":
		if m.isCurrent(b) {
			m.status = "That's the current branch"
			return nil
		}
		target := m.branchTarget(b)
		m.mainKey, m.mainTitle = "eligible:"+target, "Not yet merged from "+branchLabel(b)
		m.setMainText("Loading…")
		return tea.Batch(m.setFocus(panelMain), m.loadText(m.mainKey, func(ctx context.Context) (string, error) {
			out, err := m.client.EligibleLog(ctx, target)
			if err == nil && strings.TrimSpace(out) == "" {
				out = "Everything from " + branchLabel(b) + " is already merged."
			}
			return out, err
		}))
	case "D":
		if b.Kind == svn.KindTrunk || m.isCurrent(b) {
			m.status = "Refusing to delete trunk or the current branch"
			return nil
		}
		m.popup = &confirmPopup{
			heading: "Delete " + branchLabel(b),
			text: fmt.Sprintf("Delete %s from the repository?\n\n"+
				"This commits immediately. It stays in history and can be restored by copying it back from an older revision.", b.URL),
			onYes: m.op(func(ctx context.Context) (string, error) {
				rev, err := m.client.DeleteURL(ctx, b.URL, "Delete "+branchLabel(b))
				return fmt.Sprintf("Deleted %s in r%d", branchLabel(b), rev), err
			}),
		}
	}
	return nil
}

// createBranchPopup asks for a name and copies the current location there.
func (m *Model) createBranchPopup(kind svn.BranchKind) tea.Cmd {
	loc := m.location()
	if !loc.HasLayout() {
		m.status = "No trunk/branches/tags layout found"
		return nil
	}
	what := "branch"
	if kind == svn.KindTag {
		what = "tag"
	}
	src := loc.BranchURL(loc.Kind, loc.Name)
	heading := fmt.Sprintf("New %s from %s (HEAD)", what, loc)
	m.popup = newInputPopup(heading, []string{"Name", "Message (optional)"}, nil, func(v []string) tea.Cmd {
		name := strings.TrimSpace(v[0])
		if name == "" || strings.ContainsAny(name, "/\\") {
			return errCmd(fmt.Errorf("invalid %s name %q", what, v[0]))
		}
		msg := strings.TrimSpace(v[1])
		if msg == "" {
			msg = fmt.Sprintf("Create %s %s from %s", what, name, loc)
		}
		dst := loc.BranchURL(kind, name)
		return m.op(func(ctx context.Context) (string, error) {
			rev, err := m.client.CopyURL(ctx, src, dst, msg)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Created %s %s in r%d — press space on it to switch", what, name, rev), nil
		})
	})
	return nil
}

func (m *Model) branchDetail(b svn.Branch) string {
	var s strings.Builder
	fmt.Fprintf(&s, "%s\n\nURL:          %s\nLast change:  r%d by %s on %s\n", branchLabel(b), b.URL,
		b.Revision, b.Author, b.Date.Local().Format("2006-01-02 15:04"))
	if m.isCurrent(b) {
		s.WriteString("\nThis is the current working copy location.\n")
	} else {
		s.WriteString("\nspace  switch to it\nm      merge it into the working copy\ne      list revisions not merged yet\n")
	}
	s.WriteString("enter  show its log\n")
	return s.String()
}

func (m *Model) branchesBox(w, h int) string {
	var lines []string
	cur := lipgloss.NewStyle().Foreground(style.Green).Bold(true)
	tagStyle := lipgloss.NewStyle().Foreground(style.Magenta)
	for _, b := range m.branches {
		mark := "  "
		label := branchLabel(b)
		if m.isCurrent(b) {
			mark = cur.Render("* ")
			label = cur.Render(label)
		} else if b.Kind == svn.KindTag {
			label = tagStyle.Render(label)
		}
		lines = append(lines, fmt.Sprintf("%s%s %s", mark, label, style.Dim.Render(fmt.Sprintf("r%d %s", b.Revision, b.Author))))
	}
	switch {
	case m.branchesLoading && len(lines) == 0:
		lines = []string{style.Dim.Render(" loading…")}
	case m.branchErr != "":
		lines = []string{style.Dim.Render(" " + m.branchErr)}
	}
	focused := m.focus == panelBranches
	return style.Box("[3] Branches", m.branchList.render(lines, w-2, h-2, focused), w, h, focused)
}
