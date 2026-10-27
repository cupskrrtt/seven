// Package app is the Bubble Tea root model: panel layout, focus and keys.
package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

type panel int

const (
	panelStatus panel = iota
	panelFiles
	panelLog
	panelMain
)

var panelNames = map[panel]string{
	panelStatus: "Status", panelFiles: "Files", panelLog: "Log", panelMain: "Main",
}

const defaultRefresh = 10 * time.Second

type tickMsg struct{}

// Model is the root application model.
type Model struct {
	client *svn.Client

	info *svn.Info
	head int

	files    []svn.FileStatus
	selected map[string]bool
	fileList list

	revs       []svn.Revision
	logList    list
	logLoading bool
	logDone    bool
	logStale   bool

	focus    panel
	lastLeft panel // left panel the main view belongs to

	mainKey     string
	mainTitle   string
	mainLines   []string
	mainScroll  int
	mainHScroll int

	ignoreWhitespace bool
	showCmdLog       bool
	refreshEvery     time.Duration // status auto-refresh; 0 disables

	popup  popup
	status string

	width, height int
}

// New returns a model for the working copy client points at.
func New(client *svn.Client) *Model {
	return &Model{
		client:       client,
		selected:     map[string]bool{},
		focus:        panelFiles,
		lastLeft:     panelFiles,
		logStale:     true,
		refreshEvery: defaultRefresh,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadInfo(), m.loadStatus(), m.tick())
}

func (m *Model) tick() tea.Cmd {
	if m.refreshEvery <= 0 {
		return nil
	}
	return tea.Tick(m.refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) refresh() tea.Cmd {
	m.logStale = true
	return tea.Batch(m.loadInfo(), m.loadStatus())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if m.popup != nil {
			return m, m.tick()
		}
		return m, tea.Batch(m.loadStatus(), m.tick())

	case infoMsg:
		urlChanged := m.info == nil || m.info.URL != msg.info.URL
		m.info, m.head = msg.info, msg.head
		if m.logStale || urlChanged {
			m.logStale = false
			m.logLoading = true
			return m, m.loadLog(m.info.URL, 0)
		}
		return m, nil

	case statusMsg:
		m.applyStatus(msg.files)
		return m, m.syncMain()

	case logMsg:
		m.logLoading = false
		if msg.append {
			m.revs = append(m.revs, msg.revs...)
		} else {
			m.revs = msg.revs
			m.logList.set(m.logList.cursor, len(m.revs))
		}
		m.logDone = len(msg.revs) < logPageSize
		return m, m.syncMain()

	case diffMsg:
		if msg.key == m.mainKey {
			m.setMain(msg.text)
		}
		return m, nil

	case opDoneMsg:
		m.status = msg.status
		return m, m.refresh()

	case errMsg:
		m.logLoading = false
		m.status = ""
		text := msg.err.Error()
		if svn.IsOutOfDate(msg.err) {
			text += "\n\nThe working copy is out of date. Press 'u' to update, then commit again."
		}
		m.popup = &messagePopup{heading: "Error", text: text, isError: true}
		return m, m.loadStatus()

	case authMsg:
		retry := msg.retry
		m.popup = newInputPopup("Authentication required", []string{"Username", "Password"}, []bool{false, true},
			func(v []string) tea.Cmd {
				m.client.SetCredentials(&svn.Credentials{Username: v[0], Password: v[1]})
				return retry
			})
		return m, nil

	case editorDoneMsg:
		return m, m.commitFromEditor(msg)
	}

	if m.popup != nil {
		closed, cmd := m.popup.update(msg)
		if closed {
			m.popup = nil
		}
		return m, cmd
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		return m, m.handleKey(k)
	}
	return m, nil
}

// applyStatus replaces the file list, keeping the cursor on the same path
// and dropping selections of paths that no longer have changes.
func (m *Model) applyStatus(files []svn.FileStatus) {
	var current string
	if f, ok := m.currentFile(); ok {
		current = f.Path
	}
	m.files = files
	present := map[string]bool{}
	cursor := min(m.fileList.cursor, max(0, len(files)-1))
	for i, f := range files {
		present[f.Path] = true
		if f.Path == current {
			cursor = i
		}
	}
	for p := range m.selected {
		if !present[p] {
			delete(m.selected, p)
		}
	}
	m.fileList.set(cursor, len(files))
}

func (m *Model) currentFile() (svn.FileStatus, bool) {
	if m.fileList.cursor < len(m.files) {
		return m.files[m.fileList.cursor], true
	}
	return svn.FileStatus{}, false
}

func (m *Model) currentRev() (svn.Revision, bool) {
	if m.logList.cursor < len(m.revs) {
		return m.revs[m.logList.cursor], true
	}
	return svn.Revision{}, false
}

// syncMain loads whatever the main view should show for the current
// left-panel selection, skipping the load if it is already shown.
func (m *Model) syncMain() tea.Cmd {
	switch m.lastLeft {
	case panelFiles:
		f, ok := m.currentFile()
		if !ok {
			m.mainKey, m.mainTitle = "", "Diff"
			m.setMain("Working copy is clean.")
			return nil
		}
		key := f.Path
		if key == m.mainKey {
			return nil
		}
		m.mainKey, m.mainTitle = key, "Diff · "+f.Path
		m.setMain("Loading…")
		return m.loadFileDiff(f)
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
		m.setMain("Loading…")
		return m.loadRevisionDiff(r, m.info.RepoRoot)
	case panelStatus:
		m.mainKey, m.mainTitle = "status", "Status"
		m.setMain(m.statusDetail())
	}
	return nil
}

func (m *Model) setMain(text string) {
	lines := svn.ParseDiff(text)
	m.mainLines = make([]string, len(lines))
	for i, l := range lines {
		m.mainLines[i] = style.DiffLine(l)
	}
	m.mainScroll, m.mainHScroll = 0, 0
}

func (m *Model) statusDetail() string {
	if m.info == nil {
		return "Loading…"
	}
	i := m.info
	head := "unknown (offline?)"
	if m.head > 0 {
		head = "r" + strconv.Itoa(m.head)
	}
	return fmt.Sprintf("Working copy: %s\nURL:          %s\nRepository:   %s\nUUID:         %s\n"+
		"Location:     %s\nBASE:         r%d\nHEAD:         %s\nLast change:  r%d by %s\n\n"+
		"Press 'u' to update, '?' for all keybindings.",
		i.WCRoot, i.URL, i.RepoRoot, i.RepoUUID, svn.ParseLocation(i.RelativeURL),
		i.Revision, head, i.LastChanged, i.Author)
}

func (m *Model) setFocus(p panel) tea.Cmd {
	m.focus = p
	if p != panelMain {
		m.lastLeft = p
	}
	return m.syncMain()
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()

	// Global keys.
	switch key {
	case "q", "ctrl+c":
		return tea.Quit
	case "?":
		m.popup = &helpPopup{lines: helpLines()}
		return nil
	case "r":
		m.status = "Refreshed"
		return m.refresh()
	case "@":
		m.showCmdLog = !m.showCmdLog
		return nil
	case "u":
		m.status = "Updating…"
		return m.op(func(ctx context.Context) (string, error) {
			return m.updateTo(ctx, 0)
		})
	case "U":
		m.popup = newInputPopup("Update to revision", []string{"Revision"}, nil, func(v []string) tea.Cmd {
			rev, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(v[0]), "r"))
			if err != nil || rev <= 0 {
				return func() tea.Msg { return errMsg{fmt.Errorf("invalid revision %q", v[0])} }
			}
			m.status = fmt.Sprintf("Updating to r%d…", rev)
			return m.op(func(ctx context.Context) (string, error) { return m.updateTo(ctx, rev) })
		})
		return nil
	case "tab":
		return m.setFocus([]panel{panelFiles, panelLog, panelStatus}[m.lastLeft])
	case "shift+tab":
		return m.setFocus([]panel{panelLog, panelStatus, panelFiles}[m.lastLeft])
	case "1":
		return m.setFocus(panelStatus)
	case "2":
		return m.setFocus(panelFiles)
	case "3":
		return m.setFocus(panelLog)
	}

	switch m.focus {
	case panelFiles:
		return m.handleFilesKey(key)
	case panelLog:
		return m.handleLogKey(key)
	case panelMain:
		return m.handleMainKey(key)
	case panelStatus:
		if key == "enter" {
			return m.setFocus(panelMain)
		}
	}
	return nil
}

func (m *Model) handleFilesKey(key string) tea.Cmd {
	f, ok := m.currentFile()
	switch key {
	case "j", "down":
		m.fileList.move(1, len(m.files))
		return m.syncMain()
	case "k", "up":
		m.fileList.move(-1, len(m.files))
		return m.syncMain()
	case "g", "home":
		m.fileList.set(0, len(m.files))
		return m.syncMain()
	case "G", "end":
		m.fileList.set(len(m.files)-1, len(m.files))
		return m.syncMain()
	case "enter":
		if ok {
			return m.setFocus(panelMain)
		}
	case "w":
		m.ignoreWhitespace = !m.ignoreWhitespace
		m.mainKey = ""
		return m.syncMain()
	case "c":
		return m.startCommit(false)
	case "C":
		return m.startCommit(true)
	}
	if !ok {
		return nil
	}

	switch key {
	case " ":
		if m.selected[f.Path] {
			delete(m.selected, f.Path)
		} else {
			m.selected[f.Path] = true
		}
		m.fileList.move(1, len(m.files))
		return m.syncMain()
	case "A":
		// Toggle selection of all versioned changes.
		all := len(m.selected) == 0
		m.selected = map[string]bool{}
		if all {
			for _, f := range m.files {
				if f.Versioned() {
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
		if f.Item == svn.ItemUnversioned {
			m.popup = &confirmPopup{
				heading: "Delete unversioned file",
				text:    fmt.Sprintf("Permanently delete %s from disk? This cannot be undone.", f.Path),
				onYes: m.op(func(context.Context) (string, error) {
					return "Deleted " + f.Path, os.RemoveAll(m.abs(f.Path))
				}),
			}
			return nil
		}
		m.popup = &confirmPopup{
			heading: "Revert",
			text:    fmt.Sprintf("Discard all local changes to %s?", f.Path),
			onYes: m.op(func(ctx context.Context) (string, error) {
				return "Reverted " + f.Path, m.client.Revert(ctx, f.Path)
			}),
		}
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
		m.popup = &confirmPopup{
			heading: "Mark resolved",
			text:    fmt.Sprintf("Mark %s as resolved using the current working file?", f.Path),
			onYes: m.op(func(ctx context.Context) (string, error) {
				return "Resolved " + f.Path, m.client.Resolve(ctx, "working", f.Path)
			}),
		}
	case "e":
		return m.openEditor(m.abs(f.Path))
	}
	return nil
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
	default:
		return nil
	}
	cmds := []tea.Cmd{m.syncMain()}
	// Fetch the next page when nearing the end.
	if !m.logLoading && !m.logDone && m.logList.cursor >= len(m.revs)-10 && len(m.revs) > 0 && m.info != nil {
		if next := m.revs[len(m.revs)-1].Number - 1; next > 0 {
			m.logLoading = true
			cmds = append(cmds, m.loadLog(m.info.URL, next))
		} else {
			m.logDone = true
		}
	}
	return tea.Batch(cmds...)
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
		m.mainScroll = len(m.mainLines)
	case "h", "left":
		m.mainHScroll = max(0, m.mainHScroll-8)
	case "l", "right":
		m.mainHScroll += 8
	case "esc", "enter":
		return m.setFocus(m.lastLeft)
	}
	m.mainScroll = max(0, min(m.mainScroll, len(m.mainLines)-page))
	return nil
}

func (m *Model) abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(m.client.Dir, p)
}

func (m *Model) updateTo(ctx context.Context, rev int) (string, error) {
	res, err := m.client.Update(ctx, rev)
	if err != nil {
		return "", err
	}
	s := fmt.Sprintf("Updated to r%d", res.Revision)
	if res.Conflicts > 0 {
		s += fmt.Sprintf(" — %d conflict(s)! Resolve them in the Files panel", res.Conflicts)
	}
	return s, nil
}

// commitTargets returns selected paths, or every versioned change if
// nothing is selected.
func (m *Model) commitTargets() []string {
	var paths []string
	for _, f := range m.files {
		if m.selected[f.Path] || (len(m.selected) == 0 && f.Versioned() && f.Item != svn.ItemNormal) {
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

type editorDoneMsg struct {
	file  string
	paths []string
	err   error
}

func editorCommand(path string) *exec.Cmd {
	editor := os.Getenv("SVN_EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}
	// Allow editors with arguments, e.g. "code --wait".
	parts := strings.Fields(editor)
	return exec.Command(parts[0], append(parts[1:], path)...)
}

func (m *Model) openEditor(path string) tea.Cmd {
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
		if err != nil {
			return errMsg{err}
		}
		return opDoneMsg{"Edited " + path}
	})
}

func (m *Model) editCommitMessage(paths []string) tea.Cmd {
	f, err := os.CreateTemp("", "seven-commit-*.txt")
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	var b strings.Builder
	b.WriteString(editorTemplate)
	for _, p := range paths {
		b.WriteString("#   " + p + "\n")
	}
	f.WriteString(b.String())
	f.Close()
	return tea.ExecProcess(editorCommand(f.Name()), func(err error) tea.Msg {
		return editorDoneMsg{file: f.Name(), paths: paths, err: err}
	})
}

func (m *Model) commitFromEditor(msg editorDoneMsg) tea.Cmd {
	defer os.Remove(msg.file)
	if msg.err != nil {
		return func() tea.Msg { return errMsg{msg.err} }
	}
	data, err := os.ReadFile(msg.file)
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
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

// ---- View ----

func (m *Model) leftWidth() int {
	return min(max(m.width/3, 30), 60, m.width)
}

func (m *Model) cmdLogHeight() int {
	if m.showCmdLog {
		return 8
	}
	return 0
}

func (m *Model) mainHeight() int {
	return m.height - 1 - m.cmdLogHeight()
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < 50 || m.height < 12 {
		return "Terminal too small for seven."
	}
	lw := m.leftWidth()
	rw := m.width - lw
	avail := m.height - 1

	statusH := 4
	rest := avail - statusH
	filesH := rest * 55 / 100
	if m.focus == panelLog || (m.focus == panelMain && m.lastLeft == panelLog) {
		filesH = rest * 35 / 100
	}
	logH := rest - filesH

	left := lipgloss.JoinVertical(lipgloss.Left,
		style.Box("[1] Status", m.statusLines(lw-2), lw, statusH, m.focus == panelStatus),
		m.filesBox(lw, filesH),
		m.logBox(lw, logH),
	)

	right := m.mainBox(rw, m.mainHeight())
	if m.showCmdLog {
		right = lipgloss.JoinVertical(lipgloss.Left, right, m.cmdLogBox(rw, m.cmdLogHeight()))
	}

	view := lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n" + m.bottomBar()
	if m.popup != nil {
		pw := min(m.width-4, 80)
		view = overlay(view, renderPopup(m.popup, pw), m.width, m.height)
	}
	return view
}

func (m *Model) statusLines(w int) []string {
	if m.info == nil {
		return []string{style.Dim.Render(" loading…")}
	}
	loc := svn.ParseLocation(m.info.RelativeURL)
	rev := fmt.Sprintf(" r%d", m.info.Revision)
	if m.head > m.info.Revision {
		rev += lipgloss.NewStyle().Foreground(style.Yellow).Render(
			fmt.Sprintf(" ↓%d behind HEAD r%d", m.head-m.info.Revision, m.head))
	} else if m.head > 0 {
		rev += style.Dim.Render(" (up to date)")
	}
	return []string{
		" " + lipgloss.NewStyle().Foreground(style.Green).Bold(true).Render(loc.String()),
		rev,
	}
}

func (m *Model) filesBox(w, h int) string {
	lines := make([]string, len(m.files))
	for i, f := range m.files {
		mark := " "
		if m.selected[f.Path] {
			mark = lipgloss.NewStyle().Foreground(style.Green).Render("●")
		}
		props := " "
		if f.Props == svn.ItemModified || f.Props == svn.ItemConflicted {
			props = f.Props.Code()
		}
		tree := " "
		if f.TreeConflict {
			tree = "C"
		}
		code := style.ItemStyle(f).Render(f.Item.Code() + props + tree)
		line := fmt.Sprintf(" %s %s %s", mark, code, f.Path)
		if f.Changelist != "" {
			line += style.Dim.Render(" [" + f.Changelist + "]")
		}
		if f.Locked {
			line += style.Dim.Render(" (locked)")
		}
		lines[i] = line
	}
	if len(lines) == 0 {
		lines = []string{style.Dim.Render(" clean")}
	}
	title := fmt.Sprintf("[2] Files (%d)", len(m.files))
	if n := len(m.selected); n > 0 {
		title = fmt.Sprintf("[2] Files (%d, %d selected)", len(m.files), n)
	}
	focused := m.focus == panelFiles
	return style.Box(title, m.fileList.render(lines, w-2, h-2, focused), w, h, focused)
}

func (m *Model) logBox(w, h int) string {
	lines := make([]string, len(m.revs))
	revStyle := lipgloss.NewStyle().Foreground(style.Yellow)
	authorStyle := lipgloss.NewStyle().Foreground(style.Cyan)
	for i, r := range m.revs {
		marker := " "
		if m.info != nil && r.Number > m.info.Revision {
			marker = lipgloss.NewStyle().Foreground(style.Yellow).Render("↓")
		}
		lines[i] = fmt.Sprintf("%s%s %s %s %s", marker, revStyle.Render(fmt.Sprintf("r%-5d", r.Number)),
			style.Dim.Render(r.Date.Local().Format("01-02")), authorStyle.Render(truncate(r.Author, 10)), r.Summary())
	}
	if m.logLoading {
		lines = append(lines, style.Dim.Render(" loading…"))
	}
	focused := m.focus == panelLog
	return style.Box("[3] Log", m.logList.render(lines, w-2, h-2, focused), w, h, focused)
}

func (m *Model) mainBox(w, h int) string {
	inner := h - 2
	end := min(len(m.mainLines), m.mainScroll+inner)
	var lines []string
	for _, l := range m.mainLines[min(m.mainScroll, end):end] {
		if m.mainHScroll > 0 {
			l = ansi.Cut(l, m.mainHScroll, m.mainHScroll+w-2)
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
	if len(m.mainLines) > inner {
		title += fmt.Sprintf(" %d%%", min(100, (m.mainScroll+inner)*100/len(m.mainLines)))
	}
	return style.Box(title, lines, w, h, m.focus == panelMain)
}

func (m *Model) cmdLogBox(w, h int) string {
	entries := m.client.CommandLog()
	var lines []string
	for _, e := range entries[max(0, len(entries)-(h-2)):] {
		l := fmt.Sprintf(" svn %s %s", strings.Join(e.Args, " "), style.Dim.Render(e.Duration.Round(time.Millisecond).String()))
		if e.Err != nil {
			l = style.ErrorText.Render(ansi.Strip(l) + " ✗")
		}
		lines = append(lines, l)
	}
	return style.Box("Command log", lines, w, h, false)
}

func (m *Model) bottomBar() string {
	var hints string
	switch m.focus {
	case panelFiles:
		hints = "space select · c commit · a add · d revert · D rm · i ignore · R resolve · u update"
	case panelLog:
		hints = "enter view · j/k move · u update · U update to rev"
	case panelMain:
		hints = "j/k scroll · ctrl+d/u page · h/l pan · esc back"
	default:
		hints = "enter details · u update"
	}
	hints += " · ? help · q quit"
	bar := style.Dim.Render(hints)
	if m.status != "" {
		bar = lipgloss.NewStyle().Foreground(style.Green).Render(m.status) + "  " + bar
	}
	return ansi.Truncate(bar, m.width, "…")
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return fmt.Sprintf("%-*s", n, s)
	}
	return string([]rune(s)[:n-1]) + "…"
}
