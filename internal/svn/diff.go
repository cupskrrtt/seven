package svn

import (
	"context"
	"strconv"
	"strings"
)

// LineKind classifies a line of unified diff output.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdded
	LineRemoved
	LineHunk   // @@ -a,b +c,d @@
	LineHeader // Index:, ===, ---, +++, property headers
)

// DiffLine is one classified line of a diff.
type DiffLine struct {
	Kind LineKind
	Text string
}

// ParseDiff classifies each line of unified diff text.
func ParseDiff(text string) []DiffLine {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	raw := strings.Split(strings.TrimRight(text, "\n"), "\n")
	lines := make([]DiffLine, 0, len(raw))
	inHunk := false
	for _, l := range raw {
		kind := LineContext
		switch {
		case strings.HasPrefix(l, "Index: "), strings.HasPrefix(l, "====="),
			strings.HasPrefix(l, "Property changes on: "):
			kind, inHunk = LineHeader, false
		case !inHunk && (strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ")):
			kind = LineHeader
		case strings.HasPrefix(l, "@@"), strings.HasPrefix(l, "## "):
			kind, inHunk = LineHunk, true
		case strings.HasPrefix(l, "+"):
			kind = LineAdded
		case strings.HasPrefix(l, "-"):
			kind = LineRemoved
		}
		lines = append(lines, DiffLine{Kind: kind, Text: l})
	}
	return lines
}

// pathArg escapes a working copy path so svn does not treat a trailing
// "@..." as a peg revision.
func pathArg(p string) string {
	if strings.Contains(p, "@") {
		return p + "@"
	}
	return p
}

func pathArgs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = pathArg(p)
	}
	return out
}

// Diff returns the local diff of path against BASE.
func (c *Client) Diff(ctx context.Context, path string, ignoreWhitespace bool) (string, error) {
	args := []string{"diff"}
	if ignoreWhitespace {
		args = append(args, "-x", "-w")
	}
	args = append(args, "--", pathArg(path))
	out, err := c.Run(ctx, args...)
	return string(out), err
}

// RevisionDiff returns the changes introduced by rev under url.
func (c *Client) RevisionDiff(ctx context.Context, url string, rev int) (string, error) {
	out, err := c.Run(ctx, "diff", "-c", strconv.Itoa(rev), url)
	return string(out), err
}
