package svn

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Add schedules unversioned paths for addition.
func (c *Client) Add(ctx context.Context, paths ...string) error {
	_, err := c.Run(ctx, append([]string{"add", "--parents", "--"}, pathArgs(paths)...)...)
	return err
}

// Revert discards local changes to paths.
func (c *Client) Revert(ctx context.Context, paths ...string) error {
	_, err := c.Run(ctx, append([]string{"revert", "--depth", "infinity", "--"}, pathArgs(paths)...)...)
	return err
}

// Remove schedules paths for deletion (also used to confirm missing files).
func (c *Client) Remove(ctx context.Context, paths ...string) error {
	_, err := c.Run(ctx, append([]string{"rm", "--force", "--"}, pathArgs(paths)...)...)
	return err
}

// Resolve marks conflicted paths as resolved using the given strategy
// (working, mine-full, theirs-full, mine-conflict, theirs-conflict, base).
func (c *Client) Resolve(ctx context.Context, accept string, paths ...string) error {
	_, err := c.Run(ctx, append([]string{"resolve", "--accept", accept, "--"}, pathArgs(paths)...)...)
	return err
}

var committedRe = regexp.MustCompile(`Committed revision (\d+)\.`)

// Commit commits paths with message and returns the new revision.
// The message goes through a temp file to survive any encoding or quoting.
func (c *Client) Commit(ctx context.Context, message string, paths ...string) (int, error) {
	f, err := os.CreateTemp("", "seven-commit-*.txt")
	if err != nil {
		return 0, err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(message); err != nil {
		f.Close()
		return 0, err
	}
	f.Close()

	args := append([]string{"commit", "-F", f.Name(), "--encoding", "UTF-8", "--"}, pathArgs(paths)...)
	out, err := c.Run(ctx, args...)
	if err != nil {
		return 0, err
	}
	if m := committedRe.FindSubmatch(out); m != nil {
		return strconv.Atoi(string(m[1]))
	}
	return 0, nil // nothing to commit
}

var updatedRe = regexp.MustCompile(`(?:Updated to|At) revision (\d+)\.`)

// UpdateResult summarises an `svn update`.
type UpdateResult struct {
	Revision  int
	Output    string
	Conflicts int
}

// Update brings the working copy up to rev (0 means HEAD).
func (c *Client) Update(ctx context.Context, rev int) (*UpdateResult, error) {
	args := []string{"update", "--accept", "postpone"}
	if rev > 0 {
		args = append(args, "-r", strconv.Itoa(rev))
	}
	out, err := c.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	res := &UpdateResult{Output: string(out)}
	if m := updatedRe.FindAllSubmatch(out, -1); len(m) > 0 {
		res.Revision, _ = strconv.Atoi(string(m[len(m)-1][1]))
	}
	for _, line := range strings.Split(res.Output, "\n") {
		if len(line) >= 4 && (line[0] == 'C' || line[1] == 'C' || line[3] == 'C') && line[2] == ' ' {
			res.Conflicts++
		}
	}
	return res, nil
}

// Ignore adds path's base name to its parent directory's svn:ignore.
func (c *Client) Ignore(ctx context.Context, path string) error {
	dir, name := filepath.Dir(path), filepath.Base(path)
	out, err := c.Run(ctx, "propget", "svn:ignore", "--", pathArg(dir))
	if err != nil {
		// A missing property is reported as a warning (W200017) with exit 1
		// on newer clients; treat any propget failure as an empty list.
		out = nil
	}
	existing := strings.TrimRight(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	for _, l := range strings.Split(existing, "\n") {
		if l == name {
			return nil
		}
	}
	value := name
	if existing != "" {
		value = existing + "\n" + name
	}

	f, err := os.CreateTemp("", "seven-ignore-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(value + "\n"); err != nil {
		f.Close()
		return err
	}
	f.Close()
	_, err = c.Run(ctx, "propset", "svn:ignore", "-F", f.Name(), "--", pathArg(dir))
	return err
}
