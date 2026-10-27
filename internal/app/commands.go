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

type statusMsg struct {
	files   []svn.FileStatus
	against int // HEAD revision for a remote status, else 0
}

type logMsg struct {
	gen    int // matches Model.logGen when the request was made
	revs   []svn.Revision
	append bool
}

type branchesMsg struct {
	branches []svn.Branch
	err      error
}

type blameMsg struct {
	path  string
	lines []svn.BlameLine
}

// revisionMsg delivers a single log entry fetched on demand (e.g. from blame).
type revisionMsg struct{ rev svn.Revision }

type diffMsg struct {
	key  string // path or "r<rev>"; stale results are dropped
	text string
	file string // set for a working-copy file diff, enabling hunk revert
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
	remote := m.remoteStatus
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		if remote {
			files, against, err := m.client.StatusRemote(ctx)
			if err != nil {
				return errMsg{err}
			}
			return statusMsg{files: files, against: against}
		}
		files, err := m.client.Status(ctx)
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{files: files}
	})
}

// loadLog fetches a page of history for the log panel's current target
// and filters; from=0 starts at the top of the range.
func (m *Model) loadLog(from int) tea.Cmd {
	target, gen := m.logURL(), m.logGen
	opts := m.logOpts
	opts.Limit = logPageSize
	if from > 0 {
		opts.From = from
	}
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		revs, err := m.client.Log(ctx, target, opts)
		if err != nil {
			return errMsg{err}
		}
		return logMsg{gen: gen, revs: revs, append: from > 0}
	})
}

func (m *Model) loadBranches(loc svn.Location) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		bs, err := m.client.Branches(ctx, loc)
		return branchesMsg{branches: bs, err: err}
	})
}

func (m *Model) loadBlame(path string) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		lines, err := m.client.Blame(ctx, path)
		if err != nil {
			return errMsg{err}
		}
		return blameMsg{path: path, lines: lines}
	})
}

func (m *Model) loadRevision(target string, rev int) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		r, err := m.client.RevisionInfo(ctx, target, rev)
		if err != nil {
			return errMsg{err}
		}
		return revisionMsg{*r}
	})
}

// loadText runs fn and shows its output in the main view under key.
func (m *Model) loadText(key string, fn func(ctx context.Context) (string, error)) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		text, err := fn(ctx)
		if err != nil {
			return errMsg{err}
		}
		return diffMsg{key: key, text: text}
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
		return diffMsg{key: f.Path, text: text, file: f.Path}
	})
}

// revisionText formats a revision header and changed paths.
func revisionText(rev svn.Revision) string {
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
	return b.String()
}

// loadDirDiff shows the combined diff of a folder's versioned changes and
// lists its unversioned items.
func (m *Model) loadDirDiff(key string, files []svn.FileStatus) tea.Cmd {
	ignoreWS := m.ignoreWhitespace
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		var versioned, unversioned []string
		for _, f := range files {
			if f.Versioned() {
				versioned = append(versioned, f.Path)
			} else {
				unversioned = append(unversioned, f.Path)
			}
		}
		versioned = topLevel(versioned)
		var b strings.Builder
		if len(versioned) > 0 {
			diff, err := m.client.Diff(ctx, versioned[0], ignoreWS, versioned[1:]...)
			if err != nil {
				return errMsg{err}
			}
			b.WriteString(diff)
		}
		if len(unversioned) > 0 {
			b.WriteString("\nUnversioned:\n")
			for _, p := range unversioned {
				b.WriteString("   ? " + p + "\n")
			}
		}
		if b.Len() == 0 {
			b.WriteString("No changes.")
		}
		return diffMsg{key: key, text: b.String()}
	})
}

func (m *Model) loadRevisionDiff(rev svn.Revision, repoRoot string) tea.Cmd {
	return m.svnCmd(func(ctx context.Context) tea.Msg {
		text := revisionText(rev)
		diff, err := m.client.RevisionDiff(ctx, repoRoot, rev.Number)
		if err != nil {
			return errMsg{err}
		}
		return diffMsg{key: fmt.Sprintf("r%d", rev.Number), text: text + "\n" + diff}
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
