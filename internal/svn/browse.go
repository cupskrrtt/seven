package svn

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Entry is an item in a repository directory listing.
type Entry struct {
	Name      string
	Dir       bool
	Size      int64
	Revision  int // last changed
	Author    string
	Date      time.Time
	LockOwner string
}

type xmlListing struct {
	Lists []struct {
		Entries []struct {
			Kind   string `xml:"kind,attr"`
			Name   string `xml:"name"`
			Size   int64  `xml:"size"`
			Commit struct {
				Revision int    `xml:"revision,attr"`
				Author   string `xml:"author"`
				Date     string `xml:"date"`
			} `xml:"commit"`
			Lock *struct {
				Owner string `xml:"owner"`
			} `xml:"lock"`
		} `xml:"entry"`
	} `xml:"list"`
}

// ParseList parses `svn list --xml` output, directories first.
func ParseList(data []byte) ([]Entry, error) {
	var x xmlListing
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parse svn list: %w", err)
	}
	var dirs, files []Entry
	for _, l := range x.Lists {
		for _, e := range l.Entries {
			en := Entry{Name: e.Name, Dir: e.Kind == "dir", Size: e.Size, Revision: e.Commit.Revision, Author: e.Commit.Author}
			en.Date, _ = time.Parse(time.RFC3339Nano, e.Commit.Date)
			if e.Lock != nil {
				en.LockOwner = e.Lock.Owner
			}
			if en.Dir {
				dirs = append(dirs, en)
			} else {
				files = append(files, en)
			}
		}
	}
	return append(dirs, files...), nil
}

// revArgs pins url at rev (0 = HEAD) as both operative and peg revision,
// so paths that were later moved or deleted still resolve.
func revArgs(url string, rev int) []string {
	if rev <= 0 {
		return []string{url}
	}
	r := strconv.Itoa(rev)
	return []string{"-r", r, url + "@" + r}
}

// List returns the entries of a repository directory at rev (0 = HEAD).
func (c *Client) List(ctx context.Context, url string, rev int) ([]Entry, error) {
	out, err := c.Run(ctx, append([]string{"list", "--xml"}, revArgs(url, rev)...)...)
	if err != nil {
		return nil, err
	}
	return ParseList(out)
}

// Cat returns a file's content at rev (0 = HEAD).
func (c *Client) Cat(ctx context.Context, url string, rev int) ([]byte, error) {
	return c.Run(ctx, append([]string{"cat"}, revArgs(url, rev)...)...)
}

// Export writes an unversioned copy of url at rev to dest.
func (c *Client) Export(ctx context.Context, url string, rev int, dest string) error {
	_, err := c.Run(ctx, append(append([]string{"export"}, revArgs(url, rev)...), dest)...)
	return err
}

// Checkout creates a new working copy of url at rev in dest.
func (c *Client) Checkout(ctx context.Context, url string, rev int, dest string) error {
	_, err := c.Run(ctx, append(append([]string{"checkout"}, revArgs(url, rev)...), dest)...)
	return err
}

// Lock locks working-copy paths or URLs so others cannot commit them.
func (c *Client) Lock(ctx context.Context, message string, targets ...string) error {
	_, err := withMessageFile(message, func(msgFile string) ([]byte, error) {
		return c.Run(ctx, append([]string{"lock", "-F", msgFile, "--encoding", "UTF-8", "--"}, pathArgs(targets)...)...)
	})
	return err
}

// Unlock releases locks; force breaks a lock held by someone else.
func (c *Client) Unlock(ctx context.Context, force bool, targets ...string) error {
	args := []string{"unlock"}
	if force {
		args = append(args, "--force")
	}
	_, err := c.Run(ctx, append(append(args, "--"), pathArgs(targets)...)...)
	return err
}

// Cleanup recovers an interrupted working copy; it can also delete
// unversioned and/or ignored files.
func (c *Client) Cleanup(ctx context.Context, removeUnversioned, removeIgnored bool) error {
	args := []string{"cleanup"}
	if removeUnversioned {
		args = append(args, "--remove-unversioned")
	}
	if removeIgnored {
		args = append(args, "--remove-ignored")
	}
	_, err := c.Run(ctx, args...)
	return err
}

// Relocate rewrites the working copy's repository root from fromRoot to
// toRoot, e.g. after the server moved.
func (c *Client) Relocate(ctx context.Context, fromRoot, toRoot string) error {
	_, err := c.Run(ctx, "relocate", strings.TrimRight(fromRoot, "/"), strings.TrimRight(toRoot, "/"))
	return err
}

// PathDepth is the number of path segments in a relative URL such as
// "^/proj/trunk" (2); "^/" is 0.
func PathDepth(relURL string) int {
	p := strings.Trim(strings.TrimPrefix(relURL, "^"), "/")
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

// DiffGit returns a git-format diff of paths (all changes if none), which
// `svn patch` can re-apply including binary files, adds and properties.
// Its paths are relative to the repository root: apply it with
// PatchOptions.Strip set to PathDepth of the working copy's relative URL.
func (c *Client) DiffGit(ctx context.Context, paths ...string) (string, error) {
	args := []string{"diff", "--git"}
	if len(paths) > 0 {
		args = append(append(args, "--"), pathArgs(paths)...)
	}
	out, err := c.Run(ctx, args...)
	return string(out), err
}
