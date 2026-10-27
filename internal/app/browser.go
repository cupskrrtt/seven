package app

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

// browserView browses the repository at any revision in the main view.
type browserView struct {
	url     string // directory being listed (full URL)
	rev     int    // 0 = HEAD
	entries []svn.Entry
	loading bool
	cursor  int
	viewing bool  // a file is shown in the regular main view
	history []int // cursor positions of parent directories
	view    list  // scroll state of the listing
}

type listMsg struct {
	url     string
	rev     int
	entries []svn.Entry
}

// openBrowser opens the browser at url, or at the working copy's URL.
func (m *Model) openBrowser(at string) tea.Cmd {
	if m.info == nil {
		return nil
	}
	if at == "" {
		at = m.info.URL
	}
	m.conflict, m.blame, m.props = nil, nil, nil
	m.browser = &browserView{url: strings.TrimRight(at, "/"), loading: true}
	m.focus = panelMain
	return m.loadListing()
}

func (m *Model) loadListing() tea.Cmd {
	b := m.browser
	u, rev := b.url, b.rev
	b.loading = true
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		entries, err := m.client.List(ctx, u, rev)
		if err != nil {
			return errMsg{err}
		}
		return listMsg{url: u, rev: rev, entries: entries}
	})
}

// relURL shows a URL relative to the repository root ("^/trunk/src").
func (m *Model) relURL(u string) string {
	if m.info == nil || !strings.HasPrefix(u, m.info.RepoRoot) {
		return u
	}
	rel := strings.TrimPrefix(u, m.info.RepoRoot)
	if rel == "" {
		rel = "/"
	}
	return "^" + rel
}

func (b *browserView) current() (svn.Entry, string, bool) {
	if b.cursor >= len(b.entries) {
		return svn.Entry{}, "", false
	}
	e := b.entries[b.cursor]
	return e, b.url + "/" + url.PathEscape(e.Name), true
}

func revLabel(rev int) string {
	if rev <= 0 {
		return "HEAD"
	}
	return fmt.Sprintf("r%d", rev)
}

func (m *Model) handleBrowserKey(key string) (bool, tea.Cmd) {
	b := m.browser
	if b.viewing {
		// esc clears an active search first (handled by the main view).
		if (key == "esc" && m.mainSearch == "") || key == "backspace" {
			b.viewing = false
			m.mainKey = ""
			return true, nil
		}
		return false, nil // scrolling and search go to the regular main view
	}
	inner := m.mainHeight() - 3
	switch key {
	case "j", "down":
		b.cursor = min(b.cursor+1, max(0, len(b.entries)-1))
	case "k", "up":
		b.cursor = max(0, b.cursor-1)
	case "ctrl+d", "pgdown":
		b.cursor = min(b.cursor+inner/2, max(0, len(b.entries)-1))
	case "ctrl+u", "pgup":
		b.cursor = max(0, b.cursor-inner/2)
	case "g", "home":
		b.cursor = 0
	case "G", "end":
		b.cursor = max(0, len(b.entries)-1)
	case "enter", "l", "right":
		e, u, ok := b.current()
		if !ok {
			return true, nil
		}
		if e.Dir {
			b.history = append(b.history, b.cursor)
			b.url, b.cursor = u, 0
			return true, m.loadListing()
		}
		return true, m.viewRepoFile(u, b.rev)
	case "h", "left", "backspace":
		if m.info == nil || b.url == m.info.RepoRoot {
			m.status = "Already at the repository root"
			return true, nil
		}
		b.url = b.url[:strings.LastIndex(b.url, "/")]
		b.cursor = 0
		if n := len(b.history); n > 0 {
			b.cursor, b.history = b.history[n-1], b.history[:n-1]
		}
		return true, m.loadListing()
	case "R":
		m.popup = newInputPopup("Browse at revision (empty = HEAD)", []string{"Revision"}, nil, func(v []string) tea.Cmd {
			rev := 0
			if strings.TrimSpace(v[0]) != "" && !strings.EqualFold(strings.TrimSpace(v[0]), "head") {
				r, err := parseRev(v[0])
				if err != nil {
					return errCmd(err)
				}
				rev = r
			}
			b.rev = rev
			return m.loadListing()
		})
	case "L":
		_, u, ok := b.current()
		if !ok {
			u = b.url
		}
		m.logTarget, m.logLabel, m.logPath = u, m.relURL(u), ""
		m.browser = nil
		cmd := m.reloadLog()
		return true, tea.Batch(cmd, m.setFocus(panelLog))
	case "x", "c":
		e, u, ok := b.current()
		if !ok {
			return true, nil
		}
		if key == "c" && !e.Dir {
			m.status = "Checkout needs a folder"
			return true, nil
		}
		m.exportPopup(key == "c", e.Name, u, b.rev)
	case "K":
		e, u, ok := b.current()
		if !ok || e.Dir {
			m.status = "Only files can be locked"
			return true, nil
		}
		m.lockURLMenu(e, u)
	case "P":
		_, u, ok := b.current()
		if !ok {
			u = b.url
		}
		return true, m.openPropsView(&propsView{target: u, readOnly: true, loading: true})
	case "esc":
		return true, m.setFocus(m.lastLeft)
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) viewRepoFile(u string, rev int) tea.Cmd {
	m.browser.viewing = true
	m.mainKey = "cat:" + u + "@" + revLabel(rev)
	m.mainTitle = "File · " + m.relURL(u) + " @" + revLabel(rev)
	m.setMainText("Loading…")
	key := m.mainKey
	return m.loadText(key, func(ctx context.Context) (string, error) {
		data, err := m.client.Cat(ctx, u, rev)
		if err != nil {
			return "", err
		}
		if strings.ContainsRune(string(data[:min(len(data), 8000)]), 0) {
			return fmt.Sprintf("Binary file (%d bytes). Export it with x.", len(data)), nil
		}
		return string(data), nil
	})
}

// exportPopup asks where to export (or check out) u.
func (m *Model) exportPopup(checkout bool, name, u string, rev int) {
	what := "Export"
	if checkout {
		what = "Check out"
	}
	parent := filepath.Dir(m.client.Dir)
	p := newInputPopup(fmt.Sprintf("%s %s @%s to", what, m.relURL(u), revLabel(rev)), []string{"Path"}, nil, func(v []string) tea.Cmd {
		dest := strings.TrimSpace(v[0])
		if dest == "" {
			return nil
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(parent, dest)
		}
		m.status = what + "ing…"
		return m.op(func(ctx context.Context) (string, error) {
			var err error
			if checkout {
				err = m.client.Checkout(ctx, u, rev, dest)
			} else {
				err = m.client.Export(ctx, u, rev, dest)
			}
			return fmt.Sprintf("%s to %s done", what, dest), err
		})
	})
	p.inputs[0].SetValue(filepath.Join(parent, name))
	m.popup = p
}

func (m *Model) browserBox(w, h int) string {
	b := m.browser
	title := fmt.Sprintf("Browse · %s @%s", m.relURL(b.url), revLabel(b.rev))
	if b.loading && len(b.entries) == 0 {
		return style.Box(title, []string{style.Dim.Render(" loading…")}, w, h, true)
	}
	dirStyle := lipgloss.NewStyle().Foreground(style.Accent)
	lockStyle := lipgloss.NewStyle().Foreground(style.Yellow)
	lines := []string{style.Dim.Render(" ../")}
	for _, e := range b.entries {
		name := "  " + e.Name
		if e.Dir {
			name = "▸ " + dirStyle.Render(e.Name+"/")
		}
		meta := fmt.Sprintf("r%-6d %-10s %s", e.Revision, truncate(e.Author, 10), e.Date.Local().Format("2006-01-02"))
		if !e.Dir {
			meta += fmt.Sprintf(" %8s", humanSize(e.Size))
		}
		line := " " + style.Pad(name, max(20, w/2-2)) + style.Dim.Render(meta)
		if e.LockOwner != "" {
			line += lockStyle.Render(" locked by " + e.LockOwner)
		}
		lines = append(lines, line)
	}
	if len(b.entries) == 0 {
		lines = append(lines, style.Dim.Render(" (empty)"))
	}
	b.view.cursor = b.cursor + 1 // +1 for the "../" row
	return style.Box(title, b.view.render(lines, w-2, h-2, true), w, h, true)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}
