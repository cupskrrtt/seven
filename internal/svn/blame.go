package svn

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BlameLine attributes one line of a file to the revision that last
// changed it. Revision is 0 for uncommitted (locally changed) lines.
type BlameLine struct {
	Revision int
	Author   string
	Date     time.Time
	Text     string
}

type xmlBlame struct {
	Targets []struct {
		Entries []struct {
			Line   int `xml:"line-number,attr"`
			Commit struct {
				Revision int    `xml:"revision,attr"`
				Author   string `xml:"author"`
				Date     string `xml:"date"`
			} `xml:"commit"`
		} `xml:"entry"`
	} `xml:"target"`
}

// ParseBlame combines `svn blame --xml` output with the file text it
// describes (the XML carries no line content).
func ParseBlame(data []byte, text string) ([]BlameLine, error) {
	var x xmlBlame
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parse svn blame: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
	out := make([]BlameLine, len(lines))
	for i, l := range lines {
		out[i].Text = l
	}
	for _, t := range x.Targets {
		for _, e := range t.Entries {
			i := e.Line - 1
			if i < 0 || i >= len(out) {
				continue
			}
			out[i].Revision = e.Commit.Revision
			out[i].Author = e.Commit.Author
			out[i].Date, _ = time.Parse(time.RFC3339Nano, e.Commit.Date)
		}
	}
	return out, nil
}

// Blame annotates the working file: svn blames a working-copy path as it is
// on disk, so locally changed lines come back with no revision. The text is
// read from disk to match.
func (c *Client) Blame(ctx context.Context, path string) ([]BlameLine, error) {
	data, err := c.Run(ctx, "blame", "--xml", "--", pathArg(path))
	if err != nil {
		return nil, err
	}
	text, err := os.ReadFile(filepath.Join(c.Dir, path))
	if err != nil {
		return nil, err
	}
	return ParseBlame(data, string(text))
}
