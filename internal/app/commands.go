package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/svn"
)

const logPageSize = 100

type infoMsg struct {
	info *svn.Info
	head int
}

type statusMsg struct{ files []svn.FileStatus }

type logMsg struct {
	revs   []svn.Revision
	append bool
}

type diffMsg struct {
	key  string // path or "r<rev>"; stale results are dropped
	text string
}

// opDoneMsg reports the result of a mutating operation; a refresh follows.
type opDoneMsg struct{ status string }

type errMsg struct{ err error }

// authMsg asks for credentials, then re-runs retry.
type authMsg struct {
	err   error
	retry tea.Cmd
}

// svnCmd runs fn asynchronously and turns authentication failures into a
// credentials prompt that retries the same command.
func (m *Model) svnCmd(fn func(ctx context.Context) tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	cmd = func() tea.Msg {
		msg := fn(context.Background())
		if e, ok := msg.(errMsg); ok && errors.Is(e.err, svn.ErrAuth) {
			return authMsg{err: e.err, retry: cmd}
		}
		return msg
	}
	return cmd
}

func (m *Model) loadInfo() tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		info, err := m.client.Info(ctx)
		if err != nil {
			return errMsg{err}
		}
		head, err := m.client.HeadRevision(ctx)
		if err != nil {
			// Offline or unreachable repo: still show local info.
			head = 0
		}
		return infoMsg{info: info, head: head}
	})
}

func (m *Model) loadStatus() tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		files, err := m.client.Status(ctx)
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{files}
	})
}

// loadLog fetches a page of history; from=0 starts at HEAD.
func (m *Model) loadLog(url string, from int) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		revs, err := m.client.Log(ctx, url, svn.LogOptions{Limit: logPageSize, From: from})
		if err != nil {
			return errMsg{err}
		}
		return logMsg{revs: revs, append: from > 0}
	})
}

func (m *Model) loadFileDiff(f svn.FileStatus) tea.Cmd {
	ignoreWS := m.ignoreWhitespace
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		if f.Item == svn.ItemUnversioned {
			return diffMsg{key: f.Path, text: previewUnversioned(filepath.Join(m.client.Dir, f.Path))}
		}
		text, err := m.client.Diff(ctx, f.Path, ignoreWS)
		if err != nil {
			return errMsg{err}
		}
		if strings.TrimSpace(text) == "" {
			text = fmt.Sprintf("No textual changes (%s).", f.Item)
		}
		return diffMsg{key: f.Path, text: text}
	})
}

func (m *Model) loadRevisionDiff(rev svn.Revision, repoRoot string) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		var b strings.Builder
		fmt.Fprintf(&b, "Revision: r%d\nAuthor:   %s\nDate:     %s\n\n", rev.Number, rev.Author,
			rev.Date.Local().Format("2006-01-02 15:04:05"))
		for _, l := range strings.Split(strings.TrimSpace(rev.Message), "\n") {
			b.WriteString("    " + l + "\n")
		}
		b.WriteString("\nChanged paths:\n")
		for _, p := range rev.Paths {
			fmt.Fprintf(&b, "   %s %s", p.Action, p.Path)
			if p.CopyFrom != "" {
				fmt.Fprintf(&b, " (from %s:%d)", p.CopyFrom, p.CopyRev)
			}
			b.WriteString("\n")
		}
		diff, err := m.client.RevisionDiff(ctx, repoRoot, rev.Number)
		if err != nil {
			return errMsg{err}
		}
		b.WriteString("\n" + diff)
		return diffMsg{key: fmt.Sprintf("r%d", rev.Number), text: b.String()}
	})
}

// op runs a mutating svn operation, then reports status text.
func (m *Model) op(fn func(ctx context.Context) (string, error)) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		status, err := fn(ctx)
		if err != nil {
			return errMsg{err}
		}
		return opDoneMsg{status}
	})
}

const previewLimit = 256 << 10

// previewUnversioned renders an unversioned file as an all-added diff.
func previewUnversioned(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return err.Error()
	}
	if st.IsDir() {
		return "Unversioned directory. Press 'a' to add it (recursively)."
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	truncated := len(data) > previewLimit
	if truncated {
		data = data[:previewLimit]
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return fmt.Sprintf("Unversioned binary file (%d bytes).", st.Size())
	}
	var b strings.Builder
	b.WriteString("@@ new file @@\n")
	for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		b.WriteString("+" + l + "\n")
	}
	if truncated {
		b.WriteString("… (truncated)\n")
	}
	return b.String()
}
