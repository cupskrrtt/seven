package svn

import (
	"context"
	"errors"
	"fmt"
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

// withMessageFile writes a log message to a temp file for -F, which
// survives any encoding or quoting, and passes its path to fn.
func withMessageFile(message string, fn func(path string) ([]byte, error)) ([]byte, error) {
	f, err := os.CreateTemp("", "seven-msg-*.txt")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(message); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return fn(f.Name())
}

// committedRevision extracts N from "Committed revision N."; 0 if absent.
func committedRevision(out []byte) int {
	if m := committedRe.FindSubmatch(out); m != nil {
		n, _ := strconv.Atoi(string(m[1]))
		return n
	}
	return 0
}

// Commit commits paths with message and returns the new revision (0 if
// there was nothing to commit).
func (c *Client) Commit(ctx context.Context, message string, paths ...string) (int, error) {
	out, err := withMessageFile(message, func(msgFile string) ([]byte, error) {
		args := append([]string{"commit", "-F", msgFile, "--encoding", "UTF-8", "--"}, pathArgs(paths)...)
		return c.Run(ctx, args...)
	})
	if err != nil {
		return 0, err
	}
	return committedRevision(out), nil
}

var updatedRe = regexp.MustCompile(`(?:Updated to|At) revision (\d+)\.`)

// UpdateResult summarises an update, switch or merge.
type UpdateResult struct {
	Revision  int
	Output    string
	Conflicts int
}

// parseUpdateOutput reads the final revision and counts conflicted paths
// (a "C" in the text, property or tree-conflict column).
func parseUpdateOutput(out []byte) *UpdateResult {
	res := &UpdateResult{Output: string(out)}
	if m := updatedRe.FindAllSubmatch(out, -1); len(m) > 0 {
		res.Revision, _ = strconv.Atoi(string(m[len(m)-1][1]))
	}
	for _, line := range strings.Split(strings.ReplaceAll(res.Output, "\r\n", "\n"), "\n") {
		if len(line) >= 5 && line[2] == ' ' && (line[0] == 'C' || line[1] == 'C' || line[3] == 'C') {
			res.Conflicts++
		}
	}
	return res
}

// Update brings the working copy, or just paths, up to rev (0 means HEAD).
func (c *Client) Update(ctx context.Context, rev int, paths ...string) (*UpdateResult, error) {
	args := []string{"update", "--accept", "postpone"}
	if rev > 0 {
		args = append(args, "-r", strconv.Itoa(rev))
	}
	if len(paths) > 0 {
		args = append(append(args, "--"), pathArgs(paths)...)
	}
	out, err := c.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseUpdateOutput(out), nil
}

// SetChangelist moves paths into the named changelist.
func (c *Client) SetChangelist(ctx context.Context, name string, paths ...string) error {
	_, err := c.Run(ctx, append([]string{"changelist", name, "--"}, pathArgs(paths)...)...)
	return err
}

// RemoveChangelist takes paths out of whatever changelist they are in.
func (c *Client) RemoveChangelist(ctx context.Context, paths ...string) error {
	_, err := c.Run(ctx, append([]string{"changelist", "--remove", "--"}, pathArgs(paths)...)...)
	return err
}

// ErrPatchRejected is returned when svn patch could not apply every hunk.
var ErrPatchRejected = errors.New("patch did not apply cleanly")

// Patch applies a unified diff file to the working copy root; reverse
// undoes the changes it describes instead.
func (c *Client) Patch(ctx context.Context, patchFile string, reverse bool) (string, error) {
	args := []string{"patch"}
	if reverse {
		args = append(args, "--reverse-diff")
	}
	out, err := c.Run(ctx, append(args, patchFile)...)
	if err != nil {
		return string(out), err
	}
	if strings.Contains(string(out), "rejected hunk") {
		return string(out), fmt.Errorf("%w:\n%s", ErrPatchRejected, out)
	}
	return string(out), nil
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

	_, err = withMessageFile(value+"\n", func(valueFile string) ([]byte, error) {
		return c.Run(ctx, "propset", "svn:ignore", "-F", valueFile, "--", pathArg(dir))
	})
	return err
}
