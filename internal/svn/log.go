package svn

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ChangedPath is a path touched by a revision.
type ChangedPath struct {
	Action   string // A, M, D, R
	Kind     string // file, dir
	Path     string
	CopyFrom string
	CopyRev  int
}

// Revision is one entry from `svn log`.
type Revision struct {
	Number  int
	Author  string
	Date    time.Time
	Message string
	Paths   []ChangedPath
}

// Summary returns the first line of the commit message.
func (r Revision) Summary() string {
	msg := strings.TrimSpace(r.Message)
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimRight(msg, "\r")
}

type xmlLog struct {
	Entries []struct {
		Revision int    `xml:"revision,attr"`
		Author   string `xml:"author"`
		Date     string `xml:"date"`
		Msg      string `xml:"msg"`
		Paths    []struct {
			Action   string `xml:"action,attr"`
			Kind     string `xml:"kind,attr"`
			CopyFrom string `xml:"copyfrom-path,attr"`
			CopyRev  int    `xml:"copyfrom-rev,attr"`
			Path     string `xml:",chardata"`
		} `xml:"paths>path"`
	} `xml:"logentry"`
}

// ParseLog parses `svn log --xml [-v]` output.
func ParseLog(data []byte) ([]Revision, error) {
	var x xmlLog
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parse svn log: %w", err)
	}
	revs := make([]Revision, 0, len(x.Entries))
	for _, e := range x.Entries {
		r := Revision{Number: e.Revision, Author: e.Author, Message: e.Msg}
		if e.Date != "" {
			r.Date, _ = time.Parse(time.RFC3339Nano, e.Date)
		}
		for _, p := range e.Paths {
			r.Paths = append(r.Paths, ChangedPath{
				Action: p.Action, Kind: p.Kind, Path: p.Path,
				CopyFrom: p.CopyFrom, CopyRev: p.CopyRev,
			})
		}
		revs = append(revs, r)
	}
	return revs, nil
}

// LogOptions controls Log.
type LogOptions struct {
	Limit int
	// From is the revision to start from (descending); 0 means HEAD.
	From       int
	StopOnCopy bool
}

// Log returns revisions of target (a URL, so revisions newer than the
// working copy's BASE are included), newest first.
func (c *Client) Log(ctx context.Context, target string, opts LogOptions) ([]Revision, error) {
	args := []string{"log", "--xml", "-v"}
	if opts.Limit > 0 {
		args = append(args, "-l", strconv.Itoa(opts.Limit))
	}
	from := "HEAD"
	if opts.From > 0 {
		from = strconv.Itoa(opts.From)
	}
	args = append(args, "-r", from+":1")
	if opts.StopOnCopy {
		args = append(args, "--stop-on-copy")
	}
	args = append(args, target)
	out, err := c.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return ParseLog(out)
}
