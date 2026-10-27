package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"text/template"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/config"
	"github.com/cupskrrtt/seven/internal/svn"
)

// actionsMenu (x) collects working-copy maintenance and the less frequent
// features so they are discoverable.
func (m *Model) actionsMenu() {
	remote := "Show incoming changes"
	if m.remoteStatus {
		remote = "Hide incoming changes"
	}
	items := []menuItem{
		{"*", remote, "svn status -u: what changed on the server", m.toggleRemoteStatus},
		{"S", "Shelves…", "stash local changes and bring them back", func() tea.Cmd { m.shelvesMenu(); return nil }},
		{"B", "Repository browser", "browse any path at any revision", func() tea.Cmd { return m.openBrowser("") }},
		{"P", "Working-copy root properties", "svn:ignore, svn:externals, …", func() tea.Cmd { return m.openProps(".") }},
		{"c", "Cleanup", "recover from an interrupted operation", func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) { return "Cleanup done", m.client.Cleanup(ctx, false, false) })
		}},
		{"u", "Cleanup + delete unversioned", "removes every unversioned file", func() tea.Cmd {
			m.popup = &confirmPopup{heading: "Delete unversioned files",
				text: "Permanently delete every unversioned file and folder in the working copy? No backup is made.",
				onYes: m.op(func(ctx context.Context) (string, error) {
					return "Unversioned files removed", m.client.Cleanup(ctx, true, false)
				})}
			return nil
		}},
		{"i", "Cleanup + delete ignored", "removes build output and other ignored files", func() tea.Cmd {
			m.popup = &confirmPopup{heading: "Delete ignored files",
				text: "Permanently delete every ignored file and folder in the working copy? No backup is made.",
				onYes: m.op(func(ctx context.Context) (string, error) {
					return "Ignored files removed", m.client.Cleanup(ctx, false, true)
				})}
			return nil
		}},
		{"L", "Relocate…", "the repository moved to a new URL", m.relocatePopup},
	}
	for _, c := range m.custom {
		if c.Context == "global" {
			c := c
			items = append(items, menuItem{c.Key, c.Description, "custom: " + c.Command, func() tea.Cmd { return m.startCustom(c) }})
		}
	}
	m.popup = &menuPopup{heading: "Actions", items: items}
}

func (m *Model) relocatePopup() tea.Cmd {
	if m.info == nil {
		return nil
	}
	from := m.info.RepoRoot
	p := newInputPopup("Relocate "+from+" to", []string{"New root URL"}, nil, func(v []string) tea.Cmd {
		to := strings.TrimSpace(v[0])
		if to == "" || to == from {
			return nil
		}
		return m.op(func(ctx context.Context) (string, error) {
			return "Relocated to " + to, m.client.Relocate(ctx, from, to)
		})
	})
	p.inputs[0].SetValue(from)
	m.popup = p
	return nil
}

// toggleRemoteStatus switches the Files panel to svn status -u, which also
// lists incoming changes and repository locks.
func (m *Model) toggleRemoteStatus() tea.Cmd {
	m.remoteStatus = !m.remoteStatus
	if m.remoteStatus {
		m.status = "Checking the server for incoming changes…"
	} else {
		m.status = "Incoming changes hidden"
	}
	return m.refresh()
}

// lockMenu locks, unlocks or breaks the lock of a working-copy file.
func (m *Model) lockMenu(f svn.FileStatus) {
	path := f.Path
	switch {
	case f.Locked:
		m.popup = &confirmPopup{heading: "Unlock", text: fmt.Sprintf("Release your lock on %s?", path),
			onYes: m.op(func(ctx context.Context) (string, error) {
				return "Unlocked " + path, m.client.Unlock(ctx, false, path)
			})}
	case f.RemoteLockOwner != "":
		m.popup = &menuPopup{heading: fmt.Sprintf("%s is locked by %s", path, f.RemoteLockOwner), items: []menuItem{
			{"b", "Break the lock", "unlock --force: the owner loses it", func() tea.Cmd {
				return m.op(func(ctx context.Context) (string, error) {
					return "Broke lock on " + path, m.client.Unlock(ctx, true, path)
				})
			}},
		}}
	default:
		m.lockPopup(path, path)
	}
}

// lockURLMenu is lockMenu for a repository file in the browser.
func (m *Model) lockURLMenu(e svn.Entry, u string) {
	if e.LockOwner == "" {
		m.lockPopup(e.Name, u)
		return
	}
	m.popup = &menuPopup{heading: fmt.Sprintf("%s is locked by %s", e.Name, e.LockOwner), items: []menuItem{
		{"u", "Unlock", "release a lock you own", func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) { return "Unlocked " + e.Name, m.client.Unlock(ctx, false, u) })
		}},
		{"b", "Break the lock", "unlock --force: the owner loses it", func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) {
				return "Broke lock on " + e.Name, m.client.Unlock(ctx, true, u)
			})
		}},
	}}
}

func (m *Model) lockPopup(label, target string) {
	m.popup = newInputPopup("Lock "+label, []string{"Message (optional)"}, nil, func(v []string) tea.Cmd {
		return m.op(func(ctx context.Context) (string, error) {
			return "Locked " + label, m.client.Lock(ctx, strings.TrimSpace(v[0]), target)
		})
	})
}

// ---- Custom commands ----

type customDoneMsg struct {
	title, output, status string
	failed                bool
}

// customData is what command templates can use.
type customData struct {
	File      string   // file under the cursor (Files panel)
	Files     []string // selected files, or the file under the cursor
	Dir       string   // working-copy root
	URL       string   // working-copy URL
	RepoRoot  string
	Branch    string // current location, e.g. "trunk"
	Revision  int    // revision under the cursor in the Log panel
	BranchURL string // branch under the cursor in the Branches panel
}

func (m *Model) customData() customData {
	d := customData{Dir: m.client.Dir}
	if m.info != nil {
		d.URL, d.RepoRoot, d.Branch = m.info.URL, m.info.RepoRoot, m.location().String()
	}
	if f, ok := m.currentFile(); ok {
		d.File = f.Path
	}
	for _, f := range m.files {
		if m.selected[f.Path] {
			d.Files = append(d.Files, f.Path)
		}
	}
	if len(d.Files) == 0 && d.File != "" {
		d.Files = []string{d.File}
	}
	if r, ok := m.currentRev(); ok {
		d.Revision = r.Number
	}
	if b, ok := m.currentBranch(); ok && m.info != nil {
		d.BranchURL = m.info.RepoRoot + strings.TrimPrefix(b.URL, "^")
	}
	return d
}

// shellQuote quotes s for the platform shell.
func shellQuote(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var customFuncs = template.FuncMap{
	"quote": shellQuote,
	"join":  func(items []string, sep string) string { return strings.Join(items, sep) },
	"quoteAll": func(items []string) string {
		q := make([]string, len(items))
		for i, s := range items {
			q[i] = shellQuote(s)
		}
		return strings.Join(q, " ")
	},
}

func expandCustom(c config.CustomCommand, d customData) (string, error) {
	t, err := template.New(c.Key).Funcs(customFuncs).Option("missingkey=error").Parse(c.Command)
	if err != nil {
		return "", fmt.Errorf("custom command %q: %w", c.Key, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return "", fmt.Errorf("custom command %q: %w", c.Key, err)
	}
	return b.String(), nil
}

func shellCommand(line, dir string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", line)
	} else {
		cmd = exec.Command("sh", "-c", line)
	}
	cmd.Dir = dir
	return cmd
}

// runCustom handles key if a custom command is bound to it in the
// focused context.
func (m *Model) runCustom(key string) (tea.Cmd, bool) {
	ctx := map[panel]string{panelFiles: "files", panelBranches: "branches", panelLog: "log"}[m.focus]
	for _, c := range m.custom {
		if c.Key == key && (c.Context == ctx || c.Context == "global") && (m.focus != panelMain || c.Context == "global") {
			return m.startCustom(c), true
		}
	}
	return nil, false
}

func (m *Model) startCustom(c config.CustomCommand) tea.Cmd {
	line, err := expandCustom(c, m.customData())
	if err != nil {
		return errCmd(err)
	}
	run := func() tea.Cmd {
		if c.Subprocess {
			return tea.ExecProcess(shellCommand(line, m.client.Dir), func(err error) tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				return opDoneMsg{"Ran " + line}
			})
		}
		m.status = "Running " + line + "…"
		return func() tea.Msg {
			out, err := shellCommand(line, m.client.Dir).CombinedOutput()
			msg := customDoneMsg{title: c.Description, status: "Ran " + line}
			if msg.title == "" {
				msg.title = line
			}
			if err != nil {
				msg.failed, msg.status = true, fmt.Sprintf("%s failed: %v", line, err)
				msg.output = strings.TrimSpace(string(out) + "\n" + err.Error())
			} else if c.ShowOutput {
				msg.output = strings.TrimSpace(string(out))
				if msg.output == "" {
					msg.output = "(no output)"
				}
			}
			return msg
		}
	}
	if c.Confirm {
		m.popup = &confirmPopup{heading: "Run custom command", text: line, onYesFn: run}
		return nil
	}
	return run()
}

// ---- Mouse ----

// handleMouse focuses and selects on click and scrolls on the wheel.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	wheel := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		wheel = -1
	case tea.MouseButtonWheelDown:
		wheel = 1
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return nil
		}
	default:
		return nil
	}

	lw := m.leftWidth()
	if msg.X >= lw {
		if wheel != 0 {
			if m.mainMode() {
				key := map[int]string{-1: "k", 1: "j"}[wheel]
				var cmd tea.Cmd
				for i := 0; i < 3; i++ {
					cmd = m.handleKey(key)
				}
				return cmd
			}
			m.mainScroll += 3 * wheel
			m.clampMainScroll()
			return nil
		}
		if m.focus != panelMain {
			return m.setFocus(panelMain)
		}
		return nil
	}

	statusH, filesH, branchesH, _ := m.leftHeights(m.height - 1)
	y := msg.Y
	type region struct {
		p     panel
		top   int
		l     *list
		count int
	}
	regions := []region{
		{panelStatus, 0, nil, 0},
		{panelFiles, statusH, &m.fileList, len(m.fileRows)},
		{panelBranches, statusH + filesH, &m.branchList, len(m.branches)},
		{panelLog, statusH + filesH + branchesH, &m.logList, len(m.revs)},
	}
	for i := len(regions) - 1; i >= 0; i-- {
		r := regions[i]
		if y < r.top {
			continue
		}
		var cmds []tea.Cmd
		if m.focus != r.p {
			cmds = append(cmds, m.setFocus(r.p))
		}
		if r.l != nil {
			if wheel != 0 {
				r.l.move(wheel, r.count)
			} else if row := y - r.top - 1; row >= 0 {
				if idx := r.l.offset + row; idx < r.count {
					r.l.set(idx, r.count)
				}
			}
			cmds = append(cmds, m.syncMain())
			if r.p == panelLog {
				cmds = append(cmds, m.maybeLoadMoreLog())
			}
		}
		return tea.Batch(cmds...)
	}
	return nil
}
