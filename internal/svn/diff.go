package svn

import (
	"context"
	"errors"
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

// DiffHunk is a text hunk of a diff: the "@@" line through the line
// before the next hunk or file header. Property hunks ("##") are excluded.
type DiffHunk struct {
	Start, End int // inclusive indices into the []DiffLine
}

// DiffHunks finds the text hunks in lines.
func DiffHunks(lines []DiffLine) []DiffHunk {
	var hunks []DiffHunk
	for i := 0; i < len(lines); i++ {
		if lines[i].Kind != LineHunk || !strings.HasPrefix(lines[i].Text, "@@") {
			continue
		}
		end := i
		for end+1 < len(lines) && lines[end+1].Kind != LineHunk && lines[end+1].Kind != LineHeader {
			end++
		}
		hunks = append(hunks, DiffHunk{Start: i, End: end})
		i = end
	}
	return hunks
}

// HunkPatch builds a standalone patch containing only hunk h together with
// the file header ("Index:" through "+++") it belongs to.
func HunkPatch(lines []DiffLine, h DiffHunk) (string, error) {
	start := -1
	for i := h.Start - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i].Text, "Index: ") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", errors.New("hunk has no file header")
	}
	var b strings.Builder
	for i := start; i < h.Start; i++ {
		if lines[i].Kind != LineHeader {
			break
		}
		b.WriteString(lines[i].Text + "\n")
	}
	for i := h.Start; i <= h.End; i++ {
		b.WriteString(lines[i].Text + "\n")
	}
	return b.String(), nil
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

// Diff returns the local diff of path (and any more paths) against BASE.
func (c *Client) Diff(ctx context.Context, path string, ignoreWhitespace bool, more ...string) (string, error) {
	args := []string{"diff"}
	if ignoreWhitespace {
		args = append(args, "-x", "-w")
	}
	args = append(append(args, "--", pathArg(path)), pathArgs(more)...)
	out, err := c.Run(ctx, args...)
	return string(out), err
}

// RevisionDiff returns the changes introduced by rev under url.
func (c *Client) RevisionDiff(ctx context.Context, url string, rev int) (string, error) {
	out, err := c.Run(ctx, "diff", "-c", strconv.Itoa(rev), url)
	return string(out), err
}
