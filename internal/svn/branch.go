package svn

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Branch is trunk, a branch or a tag in the standard layout.
type Branch struct {
	Kind     BranchKind
	Name     string
	URL      string // repository-relative, "^/..."
	Revision int    // last changed revision
	Author   string
	Date     time.Time
}

type xmlLists struct {
	Lists []struct {
		Entries []struct {
			Kind   string `xml:"kind,attr"`
			Name   string `xml:"name"`
			Commit struct {
				Revision int    `xml:"revision,attr"`
				Author   string `xml:"author"`
				Date     string `xml:"date"`
			} `xml:"commit"`
		} `xml:"entry"`
	} `xml:"list"`
}

// listDirs returns the directory entries of url as branches of kind.
func (c *Client) listDirs(ctx context.Context, loc Location, url string, kind BranchKind) ([]Branch, error) {
	out, err := c.Run(ctx, "list", "--xml", url)
	if err != nil {
		return nil, err
	}
	var x xmlLists
	if err := xml.Unmarshal(out, &x); err != nil {
		return nil, fmt.Errorf("parse svn list: %w", err)
	}
	var bs []Branch
	for _, l := range x.Lists {
		for _, e := range l.Entries {
			if e.Kind != "dir" {
				continue
			}
			if kind == KindTrunk && e.Name != "trunk" {
				continue // the project root also holds branches/ and tags/
			}
			b := Branch{Kind: kind, Name: e.Name, Revision: e.Commit.Revision, Author: e.Commit.Author}
			b.URL = loc.BranchURL(kind, e.Name)
			b.Date, _ = time.Parse(time.RFC3339Nano, e.Commit.Date)
			bs = append(bs, b)
		}
	}
	return bs, nil
}

// Branches lists trunk, branches and tags of the project loc belongs to.
// Missing branches/ or tags/ directories are skipped.
func (c *Client) Branches(ctx context.Context, loc Location) ([]Branch, error) {
	if !loc.HasLayout() {
		return nil, errors.New("no trunk/branches/tags layout")
	}
	root := loc.Root
	if root == "^" {
		root = "^/" // svn needs the slash to address the repository root
	}
	var all []Branch
	for _, t := range []struct {
		url  string
		kind BranchKind
	}{
		{root, KindTrunk},
		{loc.Root + "/branches", KindBranch},
		{loc.Root + "/tags", KindTag},
	} {
		bs, err := c.listDirs(ctx, loc, t.url, t.kind)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}
		// Newest first within each group.
		sort.SliceStable(bs, func(i, j int) bool { return bs[i].Revision > bs[j].Revision })
		all = append(all, bs...)
	}
	return all, nil
}

func isNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && (strings.Contains(e.Stderr, "E200009") || strings.Contains(e.Stderr, "E160013"))
}

// Switch points the working copy at url (keeping the sub-path the caller
// includes in it) and returns the update summary.
func (c *Client) Switch(ctx context.Context, url string) (*UpdateResult, error) {
	out, err := c.Run(ctx, "switch", "--accept", "postpone", url)
	if err != nil {
		return nil, err
	}
	return parseUpdateOutput(out), nil
}

// CopyURL makes a server-side copy (how branches and tags are created) and
// returns the new revision.
func (c *Client) CopyURL(ctx context.Context, src, dst, message string) (int, error) {
	out, err := withMessageFile(message, func(msgFile string) ([]byte, error) {
		return c.Run(ctx, "copy", "--parents", "-F", msgFile, "--encoding", "UTF-8", src, dst)
	})
	if err != nil {
		return 0, err
	}
	return committedRevision(out), nil
}

// DeleteURL removes url from the repository (e.g. a branch) in one commit.
func (c *Client) DeleteURL(ctx context.Context, url, message string) (int, error) {
	out, err := withMessageFile(message, func(msgFile string) ([]byte, error) {
		return c.Run(ctx, "delete", "-F", msgFile, "--encoding", "UTF-8", url)
	})
	if err != nil {
		return 0, err
	}
	return committedRevision(out), nil
}
