package svn

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Merge performs an automatic merge of src into the working copy. Since
// svn 1.8 this covers both "sync from trunk" and the old reintegrate.
func (c *Client) Merge(ctx context.Context, src string) (*UpdateResult, error) {
	out, err := c.Run(ctx, "merge", "--accept", "postpone", src)
	if err != nil {
		return nil, err
	}
	return parseUpdateOutput(out), nil
}

// CherryPick merges the single revision rev of src into the working copy.
func (c *Client) CherryPick(ctx context.Context, src string, rev int) (*UpdateResult, error) {
	out, err := c.Run(ctx, "merge", "--accept", "postpone", "-c", strconv.Itoa(rev), src)
	if err != nil {
		return nil, err
	}
	return parseUpdateOutput(out), nil
}

// RevertRevision undoes revision rev of src in the working copy with a
// reverse merge; committing the result records the undo.
func (c *Client) RevertRevision(ctx context.Context, src string, rev int) (*UpdateResult, error) {
	out, err := c.Run(ctx, "merge", "--accept", "postpone", "-c", "-"+strconv.Itoa(rev), src)
	if err != nil {
		return nil, err
	}
	return parseUpdateOutput(out), nil
}

// EligibleLog returns the log of revisions in src not yet merged into the
// working copy, as svn formats it. Empty means everything is merged.
func (c *Client) EligibleLog(ctx context.Context, src string) (string, error) {
	out, err := c.Run(ctx, "mergeinfo", "--show-revs", "eligible", "--log", src)
	return string(out), err
}

// IsMixedRevision reports whether a merge was refused because the working
// copy has mixed revisions (fixed by updating first).
func IsMixedRevision(err error) bool {
	var e *Error
	return errors.As(err, &e) && strings.Contains(e.Stderr, "E195020")
}
