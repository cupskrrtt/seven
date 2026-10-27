package svn

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
)

// Info describes a working copy as reported by `svn info`.
type Info struct {
	Path        string
	URL         string
	RelativeURL string
	RepoRoot    string
	RepoUUID    string
	WCRoot      string
	Revision    int
	LastChanged int
	Author      string
}

type xmlInfo struct {
	Entries []struct {
		Path        string `xml:"path,attr"`
		Revision    int    `xml:"revision,attr"`
		URL         string `xml:"url"`
		RelativeURL string `xml:"relative-url"`
		Repository  struct {
			Root string `xml:"root"`
			UUID string `xml:"uuid"`
		} `xml:"repository"`
		WCInfo struct {
			Root string `xml:"wcroot-abspath"`
		} `xml:"wc-info"`
		Commit struct {
			Revision int    `xml:"revision,attr"`
			Author   string `xml:"author"`
		} `xml:"commit"`
	} `xml:"entry"`
}

// ParseInfo parses `svn info --xml` output.
func ParseInfo(data []byte) (*Info, error) {
	var x xmlInfo
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parse svn info: %w", err)
	}
	if len(x.Entries) == 0 {
		return nil, fmt.Errorf("parse svn info: no entries")
	}
	e := x.Entries[0]
	return &Info{
		Path:        e.Path,
		URL:         e.URL,
		RelativeURL: e.RelativeURL,
		RepoRoot:    e.Repository.Root,
		RepoUUID:    e.Repository.UUID,
		WCRoot:      e.WCInfo.Root,
		Revision:    e.Revision,
		LastChanged: e.Commit.Revision,
		Author:      e.Commit.Author,
	}, nil
}

// Info returns information about the working copy root.
func (c *Client) Info(ctx context.Context) (*Info, error) {
	out, err := c.Run(ctx, "info", "--xml")
	if err != nil {
		if e, ok := err.(*Error); ok && strings.Contains(e.Stderr, "E155007") {
			return nil, fmt.Errorf("%s is not a working copy", c.Dir)
		}
		return nil, err
	}
	return ParseInfo(out)
}

// HeadRevision returns the youngest revision of the repository.
func (c *Client) HeadRevision(ctx context.Context) (int, error) {
	out, err := c.Run(ctx, "info", "--xml", "-r", "HEAD")
	if err != nil {
		return 0, err
	}
	info, err := ParseInfo(out)
	if err != nil {
		return 0, err
	}
	return info.Revision, nil
}
