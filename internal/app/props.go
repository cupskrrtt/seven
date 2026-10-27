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

// propsView lists and edits the properties of one path in the main view.
type propsView struct {
	target   string // working-copy path, or a URL when readOnly
	readOnly bool   // repository URLs can't be changed without a commit
	props    []svn.Property
	loading  bool
	cursor   int
}

type propsMsg struct {
	target string
	props  []svn.Property
}

// propEditedMsg returns from $EDITOR with a property's new value in file.
type propEditedMsg struct {
	path, name, file string
	err              error
}

// Well-known properties offered when adding one.
var knownProps = []struct{ name, hint string }{
	{"svn:ignore", "unversioned names to ignore in this folder, one per line"},
	{"svn:global-ignores", "like svn:ignore, inherited by subfolders"},
	{"svn:externals", "other repository paths to check out here"},
	{"svn:eol-style", "native, LF, CRLF or CR"},
	{"svn:keywords", "e.g. Id Rev Date Author URL"},
	{"svn:executable", "set to * to make the file executable"},
	{"svn:mime-type", "e.g. application/octet-stream to mark binary"},
	{"svn:needs-lock", "set to * to require a lock before editing"},
}

func (m *Model) openProps(target string) tea.Cmd {
	return m.openPropsView(&propsView{target: target, loading: true})
}

func (m *Model) openPropsView(v *propsView) tea.Cmd {
	m.conflict, m.blame = nil, nil
	m.props = v
	m.focus = panelMain
	return m.loadProps(v.target)
}

func (m *Model) loadProps(target string) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		props, err := m.client.Props(ctx, target)
		if err != nil {
			return errMsg{err}
		}
		return propsMsg{target: target, props: props}
	})
}

func (m *Model) handlePropsKey(key string) (bool, tea.Cmd) {
	v := m.props
	switch key {
	case "j", "down":
		v.cursor = min(v.cursor+1, max(0, len(v.props)-1))
	case "k", "up":
		v.cursor = max(0, v.cursor-1)
	case "esc":
		if m.browser != nil {
			m.props = nil // back to the repository browser
			return true, nil
		}
		return true, m.setFocus(m.lastLeft)
	case "e", "enter":
		if v.readOnly || v.cursor >= len(v.props) {
			return true, m.readOnlyNote(v)
		}
		p := v.props[v.cursor]
		return true, m.editPropValue(v.target, p.Name, p.Value)
	case "a":
		if v.readOnly {
			return true, m.readOnlyNote(v)
		}
		m.addPropMenu(v.target)
	case "d":
		if v.readOnly || v.cursor >= len(v.props) {
			return true, m.readOnlyNote(v)
		}
		name, target := v.props[v.cursor].Name, v.target
		m.popup = &confirmPopup{
			heading: "Delete property",
			text:    fmt.Sprintf("Remove %s from %s? (Takes effect when you commit.)", name, target),
			onYes: m.op(func(ctx context.Context) (string, error) {
				return "Deleted " + name, m.client.DeleteProp(ctx, name, target)
			}),
		}
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) readOnlyNote(v *propsView) tea.Cmd {
	if v.readOnly {
		m.status = "Repository properties are read-only here — edit them in a working copy"
	}
	return nil
}

func (m *Model) addPropMenu(target string) {
	items := []menuItem{{"n", "Custom…", "any property name", func() tea.Cmd {
		m.popup = newInputPopup("New property on "+target, []string{"Name"}, nil, func(v []string) tea.Cmd {
			name := strings.TrimSpace(v[0])
			if name == "" {
				return nil
			}
			return m.editPropValue(target, name, "")
		})
		return nil
	}}}
	for i, p := range knownProps {
		name := p.name
		items = append(items, menuItem{fmt.Sprint(i + 1), name, p.hint, func() tea.Cmd {
			return m.editPropValue(target, name, "")
		}})
	}
	m.popup = &menuPopup{heading: "Add property", items: items}
}

// editPropValue opens the value in $EDITOR (properties are often
// multi-line, like svn:ignore or svn:externals).
func (m *Model) editPropValue(path, name, value string) tea.Cmd {
	f, err := os.CreateTemp("", "seven-prop-*.txt")
	if err != nil {
		return errCmd(err)
	}
	f.WriteString(value)
	f.Close()
	return tea.ExecProcess(m.editorCommand(f.Name()), func(err error) tea.Msg {
		return propEditedMsg{path: path, name: name, file: f.Name(), err: err}
	})
}

func (m *Model) finishPropEdit(msg propEditedMsg) tea.Cmd {
	defer os.Remove(msg.file)
	if msg.err != nil {
		return errCmd(msg.err)
	}
	data, err := os.ReadFile(msg.file)
	if err != nil {
		return errCmd(err)
	}
	// Editors add a final newline; single-line values shouldn't keep it.
	value := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.Count(value, "\n") <= 1 {
		value = strings.TrimSpace(value)
	}
	if value == "" {
		m.status = "Empty value — property not changed (use d to delete it)"
		return nil
	}
	return m.op(func(ctx context.Context) (string, error) {
		return fmt.Sprintf("Set %s on %s", msg.name, msg.path), m.client.SetProp(ctx, msg.name, value, msg.path)
	})
}

func (m *Model) propsBox(w, h int) string {
	v := m.props
	title := "Properties · " + v.target
	if v.readOnly {
		title += " (read-only)"
	}
	if v.loading {
		return style.Box(title, []string{style.Dim.Render(" loading…")}, w, h, true)
	}
	if len(v.props) == 0 {
		return style.Box(title, []string{style.Dim.Render(" no properties — press a to add one")}, w, h, true)
	}
	nameStyle := lipgloss.NewStyle().Foreground(style.Accent).Bold(true)
	var lines []string
	cursorLine := 0
	for i, p := range v.props {
		if i == v.cursor {
			cursorLine = len(lines)
			lines = append(lines, style.Selected.Render(style.Pad(" "+p.Name, w-2)))
		} else {
			lines = append(lines, " "+nameStyle.Render(p.Name))
		}
		for _, l := range strings.Split(strings.TrimRight(p.Value, "\n"), "\n") {
			lines = append(lines, "     "+style.ExpandTabs(l))
		}
		lines = append(lines, "")
	}
	// Keep the selected property in view.
	inner := h - 2
	start := max(0, min(cursorLine-inner/3, len(lines)-inner))
	out := lines[start:]
	for i := range out {
		out[i] = ansi.Truncate(out[i], w-2, "")
	}
	return style.Box(title, out, w, h, true)
}
