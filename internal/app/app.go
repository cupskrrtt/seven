// Package app is the Bubble Tea root model: panel layout, focus and keys.
// Each panel's keys and rendering live in its own file.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/config"
	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

type panel int

const (
	panelStatus panel = iota
	panelFiles
	panelBranches
	panelLog
	panelMain
)

// leftPanels is the tab order of the left column.
var leftPanels = []panel{panelStatus, panelFiles, panelBranches, panelLog}

const defaultRefresh = 10 * time.Second

type tickMsg struct{}

// Model is the root application model.
type Model struct {
	client *svn.Client

	info *svn.Info
	head int

	files     []svn.FileStatus
	selected  map[string]bool
	fileList  list      // cursor over fileRows
	fileRows  []fileRow // visible tree (or flat) rows
	collapsed map[string]bool
	flatFiles bool // show a flat list instead of the tree

	branches        []svn.Branch
	branchList      list
	branchErr       string
	branchesLoading bool
	branchesStale   bool

	revs       []svn.Revision
	logList    list
	logLoading bool
	logDone    bool
	logStale   bool
	logGen     int            // bumped whenever target/filters change
	logTarget  string         // URL being logged; "" means the working copy's URL
	logLabel   string         // branch name when logTarget is set
	logPath    string         // working-copy relative path filter
	logOpts    svn.LogOptions // search / range / stop-on-copy (Limit and paging set per request)

	focus    panel
	lastLeft panel // left panel the main view belongs to

	mainKey     string
	mainTitle   string
	mainLines   []string
	mainRaw     []svn.DiffLine
	mainFile    string // file whose diff is shown; enables hunk revert
	mainHunks   []svn.DiffHunk
	mainHunk    int // selected hunk, -1 for none
	mainScroll  int // index into mainVisible
	mainHScroll int

	mainSections  []diffSection // per-file sections of a multi-file diff
	mainSectionOf []int         // raw line → section, -1 before the first file
	mainFolded    map[int]bool  // folded sections
	mainCurFile   int           // section that z / } / { act on, -1 if none
	mainVisible   []int         // raw lines shown after folding
	mainSearch    string
	mainSearchRe  *regexp.Regexp
	mainMatches   []int // raw lines matching the search
	mainMatch     int   // current match, -1 for none

	conflict *conflictView // non-nil while the hunk picker owns the main view
	blame    *blameView    // non-nil while blame owns the main view
	props    *propsView    // non-nil while the property editor owns the main view
	browser  *browserView  // non-nil while the repository browser owns the main view

	remoteStatus bool // include incoming changes (svn status -u)
	against      int  // HEAD revision the last remote status compared with

	ignoreWhitespace bool
	showCmdLog       bool
	refreshEvery     time.Duration // status auto-refresh; 0 disables
	backupRoot       string        // overrides the backup directory (tests)
	shelfRoot        string        // overrides the shelf directory (tests)

	editor string
	remap  map[string]string
	custom []config.CustomCommand

	popup  popup
	status string

	width, height int
}

// New returns a model for the working copy client points at; cfg may be nil.
func New(client *svn.Client, cfg *config.Config) *Model {
	if cfg == nil {
		cfg = &config.Config{}
	}
	m := &Model{
		client:        client,
		selected:      map[string]bool{},
		collapsed:     map[string]bool{},
		focus:         panelFiles,
		lastLeft:      panelFiles,
		logStale:      true,
		branchesStale: true,
		mainHunk:      -1,
		refreshEvery:  defaultRefresh,
		flatFiles:     cfg.FlatFileList,
		editor:        cfg.Editor,
		remap:         cfg.Remap,
		custom:        cfg.CustomCommands,
	}
	switch {
	case cfg.RefreshSeconds < 0:
		m.refreshEvery = 0
	case cfg.RefreshSeconds > 0:
		m.refreshEvery = time.Duration(cfg.RefreshSeconds) * time.Second
	}
	return m
}

// mainMode reports whether a special view (conflict picker, blame,
// properties, browser) owns the main view.
func (m *Model) mainMode() bool {
	return m.conflict != nil || m.blame != nil || m.props != nil || m.browser != nil
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
	m.branchesStale = true
	return tea.Batch(m.loadInfo(), m.loadStatus())
}

func (m *Model) location() svn.Location {
	if m.info == nil {
		return svn.Location{}
	}
	return svn.ParseLocation(m.info.RelativeURL)
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
		prev := m.location()
		urlChanged := m.info == nil || m.info.URL != msg.info.URL
		m.info, m.head = msg.info, msg.head
		var cmds []tea.Cmd
		if urlChanged {
			// Switched branch: the log follows the working copy again.
			m.logTarget, m.logLabel = "", ""
		}
		if m.logStale || urlChanged {
			m.logStale = false
			cmds = append(cmds, m.reloadLog())
		}
		if loc := m.location(); m.branchesStale || loc.Root != prev.Root {
			m.branchesStale = false
			if loc.HasLayout() {
				m.branchesLoading = true
				cmds = append(cmds, m.loadBranches(loc))
			} else {
				m.branches, m.branchErr = nil, "No trunk/branches/tags layout found."
			}
		}
		return m, tea.Batch(cmds...)

	case statusMsg:
		if msg.against > 0 {
			m.against = msg.against
			m.head = max(m.head, msg.against) // fresher than the last info
		}
		m.applyStatus(msg.files)
		return m, m.syncMain()

	case propsMsg:
		if m.props != nil && m.props.target == msg.target {
			m.props.props, m.props.loading = msg.props, false
			m.props.cursor = min(m.props.cursor, max(0, len(msg.props)-1))
		}
		return m, nil

	case propEditedMsg:
		return m, m.finishPropEdit(msg)

	case listMsg:
		if b := m.browser; b != nil && b.url == msg.url && b.rev == msg.rev {
			b.entries, b.loading = msg.entries, false
			b.cursor = min(b.cursor, max(0, len(msg.entries)-1))
		}
		return m, nil

	case customDoneMsg:
		if msg.output != "" {
			m.popup = &messagePopup{heading: msg.title, text: msg.output, isError: msg.failed}
		}
		m.status = msg.status
		return m, m.refresh()

	case tea.MouseMsg:
		if m.popup != nil {
			return m, nil
		}
		return m, m.handleMouse(msg)

	case branchesMsg:
		m.branchesLoading = false
		m.branches, m.branchErr = msg.branches, ""
		if msg.err != nil {
			m.branchErr = msg.err.Error()
		}
		m.branchList.set(m.branchList.cursor, len(m.branches))
		return m, m.syncMain()

	case logMsg:
		if msg.gen != m.logGen {
			return m, nil // filters changed while this page was loading
		}
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
			m.setMainDiff(msg.text, msg.file)
		}
		return m, nil

	case blameMsg:
		if m.blame != nil && m.blame.path == msg.path {
			m.blame.lines = msg.lines
			m.blame.loading = false
		}
		return m, nil

	case revisionMsg:
		return m, m.showRevision(msg.rev)

	case opDoneMsg:
		m.status = msg.status
		if m.conflict != nil {
			m.reloadConflict() // e.g. after editing the file in $EDITOR
		}
		m.mainKey = "" // whatever is shown may have changed
		cmds := []tea.Cmd{m.refresh()}
		if m.props != nil {
			cmds = append(cmds, m.loadProps(m.props.target))
		}
		if m.browser != nil && !m.browser.viewing {
			cmds = append(cmds, m.loadListing())
		}
		return m, tea.Batch(cmds...)

	case errMsg:
		m.logLoading = false
		m.status = ""
		text := msg.err.Error()
		switch {
		case svn.IsOutOfDate(msg.err):
			text += "\n\nThe working copy is out of date. Press 'u' to update, then try again."
		case svn.IsMixedRevision(msg.err):
			text += "\n\nThe working copy has mixed revisions. Press 'u' to update, then merge again."
		case errors.Is(msg.err, svn.ErrPatchRejected):
			text += "\n\nThe file was left unchanged where the hunk did not apply."
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

	if p := m.popup; p != nil {
		closed, cmd := p.update(msg)
		// The popup may have opened a follow-up popup (e.g. menu → confirm).
		if closed && m.popup == p {
			m.popup = nil
		}
		return m, cmd
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		key := k.String()
		if to, ok := m.remap[key]; ok {
			key = to
		}
		return m, m.handleKey(key)
	}
	return m, nil
}

func (m *Model) setFocus(p panel) tea.Cmd {
	m.focus = p
	if p != panelMain {
		m.lastLeft = p
		if m.mainMode() {
			m.conflict, m.blame, m.props, m.browser = nil, nil, nil, nil
			m.mainKey = "" // reload whatever the panel shows
		}
	}
	return m.syncMain()
}

func (m *Model) cycleFocus(step int) tea.Cmd {
	for i, p := range leftPanels {
		if p == m.lastLeft {
			return m.setFocus(leftPanels[(i+step+len(leftPanels))%len(leftPanels)])
		}
	}
	return m.setFocus(panelFiles)
}

func (m *Model) handleKey(key string) tea.Cmd {
	// Main-view modes take precedence over global keys except quit/help.
	if m.focus == panelMain && key != "q" && key != "ctrl+c" && key != "?" {
		var handled bool
		var cmd tea.Cmd
		switch {
		case m.conflict != nil:
			handled, cmd = m.handleConflictKey(key)
		case m.blame != nil:
			handled, cmd = m.handleBlameKey(key)
		case m.props != nil:
			handled, cmd = m.handlePropsKey(key)
		case m.browser != nil:
			handled, cmd = m.handleBrowserKey(key)
		}
		if handled {
			return cmd
		}
	}
	// User-defined commands win over built-in keys in their context.
	if cmd, ok := m.runCustom(key); ok {
		return cmd
	}
	// Panel keys that shadow global ones.
	if m.focus == panelLog && key == "U" {
		return m.logUpdateMenu()
	}

	switch key {
	case "x":
		m.actionsMenu()
		return nil
	case "*":
		return m.toggleRemoteStatus()
	case "B":
		return m.openBrowser("")
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
			rev, err := parseRev(v[0])
			if err != nil {
				return errCmd(err)
			}
			m.status = fmt.Sprintf("Updating to r%d…", rev)
			return m.op(func(ctx context.Context) (string, error) { return m.updateTo(ctx, rev) })
		})
		return nil
	case "tab":
		return m.cycleFocus(1)
	case "shift+tab":
		return m.cycleFocus(-1)
	case "1", "2", "3", "4":
		return m.setFocus(leftPanels[key[0]-'1'])
	}

	switch m.focus {
	case panelFiles:
		return m.handleFilesKey(key)
	case panelBranches:
		return m.handleBranchesKey(key)
	case panelLog:
		return m.handleLogKey(key)
	case panelMain:
		return m.handleMainKey(key)
	case panelStatus:
		switch key {
		case "enter":
			return m.setFocus(panelMain)
		case "P":
			return m.openProps(".")
		}
	}
	return nil
}

func errCmd(err error) tea.Cmd { return func() tea.Msg { return errMsg{err} } }

// parseRev accepts "42" or "r42".
func parseRev(s string) (int, error) {
	rev, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "r"))
	if err != nil || rev <= 0 {
		return 0, fmt.Errorf("invalid revision %q", s)
	}
	return rev, nil
}

func (m *Model) abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(m.client.Dir, p)
}

// describeUpdate summarises an update/switch/merge result for the status bar.
func describeUpdate(prefix string, res *svn.UpdateResult) string {
	s := prefix
	if res.Conflicts > 0 {
		s += fmt.Sprintf(" — %d conflict(s)! Resolve them in the Files panel", res.Conflicts)
	}
	return s
}

func (m *Model) updateTo(ctx context.Context, rev int, paths ...string) (string, error) {
	res, err := m.client.Update(ctx, rev, paths...)
	if err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("Updated to r%d", res.Revision)
	if len(paths) > 0 {
		prefix = fmt.Sprintf("Updated %s to r%d", strings.Join(paths, ", "), res.Revision)
	}
	return describeUpdate(prefix, res), nil
}

type editorDoneMsg struct {
	file  string
	paths []string
	err   error
}

// editorCommand opens path in the configured editor, falling back to
// $SVN_EDITOR, $VISUAL, $EDITOR, then notepad / vi.
func (m *Model) editorCommand(path string) *exec.Cmd {
	editor := m.editor
	if editor == "" {
		editor = os.Getenv("SVN_EDITOR")
	}
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
	return tea.ExecProcess(m.editorCommand(path), func(err error) tea.Msg {
		if err != nil {
			return errMsg{err}
		}
		return opDoneMsg{"Edited " + path}
	})
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

// leftHeights splits the left column; the active panel gets extra room.
func (m *Model) leftHeights(avail int) (status, files, branches, log int) {
	status = 4
	rest := avail - status
	weights := map[panel]int{panelFiles: 3, panelBranches: 2, panelLog: 3}
	if _, ok := weights[m.lastLeft]; ok {
		weights[m.lastLeft] += 3
	}
	total := weights[panelFiles] + weights[panelBranches] + weights[panelLog]
	files = max(3, rest*weights[panelFiles]/total)
	branches = max(3, rest*weights[panelBranches]/total)
	log = rest - files - branches
	return
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < 50 || m.height < 16 {
		return "Terminal too small for seven."
	}
	lw := m.leftWidth()
	rw := m.width - lw
	statusH, filesH, branchesH, logH := m.leftHeights(m.height - 1)

	left := lipgloss.JoinVertical(lipgloss.Left,
		style.Box("[1] Status", m.statusLines(lw-2), lw, statusH, m.focus == panelStatus),
		m.filesBox(lw, filesH),
		m.branchesBox(lw, branchesH),
		m.logBox(lw, logH),
	)

	var right string
	switch {
	case m.conflict != nil:
		right = m.conflictBox(rw, m.mainHeight())
	case m.blame != nil:
		right = m.blameBox(rw, m.mainHeight())
	case m.props != nil:
		right = m.propsBox(rw, m.mainHeight())
	case m.browser != nil && !m.browser.viewing:
		right = m.browserBox(rw, m.mainHeight())
	default:
		right = m.mainBox(rw, m.mainHeight())
	}
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
	loc := m.location()
	rev := fmt.Sprintf(" r%d", m.info.Revision)
	if m.head > m.info.Revision {
		rev += lipgloss.NewStyle().Foreground(style.Yellow).Render(
			fmt.Sprintf(" ↓%d behind HEAD r%d", m.head-m.info.Revision, m.head))
	} else if m.head > 0 {
		rev += style.Dim.Render(" (up to date)")
	}
	if m.remoteStatus {
		n := 0
		for _, f := range m.files {
			if f.RemoteChanged {
				n++
			}
		}
		rev += lipgloss.NewStyle().Foreground(style.Yellow).Render(fmt.Sprintf(" · %d incoming", n))
	}
	return []string{
		" " + lipgloss.NewStyle().Foreground(style.Green).Bold(true).Render(loc.String()),
		rev,
	}
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
		i.WCRoot, i.URL, i.RepoRoot, i.RepoUUID, m.location(),
		i.Revision, head, i.LastChanged, i.Author)
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
	switch {
	case m.focus == panelMain && m.conflict != nil:
		hints = "n/p next/prev conflict · m mine · t theirs · b both · B base · z undo · e edit · esc back"
	case m.focus == panelMain && m.blame != nil:
		hints = "j/k move · enter show revision · esc back"
	case m.focus == panelMain && m.props != nil:
		hints = "e edit value · a add · d delete · j/k move · esc back"
	case m.focus == panelMain && m.browser != nil && m.browser.viewing:
		hints = "/ search · j/k scroll · esc back to listing"
	case m.focus == panelMain && m.browser != nil:
		hints = "enter open · h up · R revision · L log · x export · c checkout · K lock · P props · esc close"
	case m.focus == panelMain && m.mainSearch != "":
		hints = "n/N next/prev match · / new search · esc clear search"
	case m.focus == panelMain && m.mainFile != "" && len(m.mainHunks) > 0:
		hints = "]/[ next/prev hunk · d revert hunk · / search · j/k scroll · esc back"
	case m.focus == panelMain && len(m.mainSections) > 1:
		hints = "/ search · z fold file · Z fold all · }/{ next/prev file · j/k scroll · esc back"
	case m.focus == panelMain:
		hints = "/ search · j/k scroll · ctrl+d/u page · h/l pan · esc back"
	case m.focus == panelFiles:
		hints = "space select · c commit · a add · d discard · enter fold · b blame · l log · S shelve · K lock · P props · x more"
	case m.focus == panelBranches:
		hints = "space switch · n new branch · t new tag · m merge · e unmerged · enter log · D delete"
	case m.focus == panelLog:
		hints = "c cherry-pick · R revert rev · U update to rev · / filter · s stop-on-copy · esc reset"
	default:
		hints = "enter details · u update · P root properties · * incoming · B browse repo · x actions"
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
