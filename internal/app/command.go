package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/svn"
)

// commandOutputMsg carries the result of a command typed at the ':' prompt.
type commandOutputMsg struct {
	line   string
	output string
	err    error
}

// commandPrompt (':') runs any svn subcommand in the working copy and shows
// its output in the main view, where it can be searched.
func (m *Model) commandPrompt() {
	p := newInputPopup("Run svn command", []string{"svn"}, nil, func(v []string) tea.Cmd {
		line := strings.TrimSpace(v[0])
		if line == "" {
			return nil
		}
		args, err := splitArgs(line)
		if err != nil {
			return errCmd(err)
		}
		if len(args) > 0 && args[0] == "svn" {
			args = args[1:] // "svn status" and "status" both work
		}
		if len(args) == 0 {
			return nil
		}
		m.lastCommand = strings.Join(args, " ")
		m.status = "svn " + m.lastCommand + " …"
		return m.svnCmd(func(ctx context.Context) tea.Msg {
			out, err := m.client.Run(ctx, args...)
			if errors.Is(err, svn.ErrAuth) {
				return errMsg{err} // svnCmd turns this into a credentials prompt and retries
			}
			return commandOutputMsg{line: "svn " + strings.Join(args, " "), output: string(out), err: err}
		})
	})
	p.inputs[0].SetValue(m.lastCommand)
	p.inputs[0].CursorEnd()
	m.popup = p
}

func (m *Model) showCommandOutput(msg commandOutputMsg) tea.Cmd {
	m.conflict, m.blame, m.props, m.browser = nil, nil, nil, nil
	m.commandRuns++
	m.mainKey = fmt.Sprintf("cmd:%d", m.commandRuns)
	m.mainTitle = msg.line
	text := msg.output
	if msg.err != nil {
		text = strings.TrimRight(text, "\n") + "\n\n" + msg.err.Error()
		m.status = msg.line + " failed"
	} else {
		m.status = msg.line + " done"
	}
	if strings.TrimSpace(text) == "" {
		text = "(no output)"
	}
	m.setMainText(text)
	m.focus = panelMain
	// The command may have changed the working copy.
	return tea.Batch(m.loadInfo(), m.loadStatus())
}

// splitArgs splits a command line like a POSIX shell would for simple
// cases: whitespace separates words, '...' and "..." quote, and a
// backslash escapes the next character outside single quotes.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case unicode.IsSpace(r):
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}
