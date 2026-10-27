package svn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commitFile writes content to name in the working copy and commits it.
func commitFile(t *testing.T, c *Client, name, content, msg string) int {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(c.Dir, name)
	_, statErr := os.Stat(path)
	write(t, path, content)
	if os.IsNotExist(statErr) {
		if err := c.Add(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	rev, err := c.Commit(ctx, msg, name)
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func mustUpdate(t *testing.T, c *Client) {
	t.Helper()
	if _, err := c.Update(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}

func TestBranchesSwitchMerge(t *testing.T) {
	c, _ := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "1\n2\n3\n", "base")
	mustUpdate(t, c)

	info, _ := c.Info(ctx)
	loc := ParseLocation(info.RelativeURL)

	// Create a branch and a tag from trunk.
	brURL := loc.BranchURL(KindBranch, "feature")
	if rev, err := c.CopyURL(ctx, loc.BranchURL(KindTrunk, ""), brURL, "Create feature"); err != nil || rev == 0 {
		t.Fatalf("copy branch: rev=%d err=%v", rev, err)
	}
	if _, err := c.CopyURL(ctx, loc.BranchURL(KindTrunk, ""), loc.BranchURL(KindTag, "v1"), "Tag v1"); err != nil {
		t.Fatal(err)
	}
	bs, err := c.Branches(ctx, loc)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range bs {
		names = append(names, b.URL)
	}
	if got := strings.Join(names, " "); got != "^/trunk ^/branches/feature ^/tags/v1" {
		t.Fatalf("branches = %s", got)
	}

	// Commit on the branch, then switch back to trunk.
	if _, err := c.Switch(ctx, brURL); err != nil {
		t.Fatal(err)
	}
	if info, _ := c.Info(ctx); ParseLocation(info.RelativeURL).Name != "feature" {
		t.Fatalf("after switch: %s", info.RelativeURL)
	}
	featRev := commitFile(t, c, "a.txt", "1\n2\n3\nfeature\n", "feature work")
	commitFile(t, c, "b.txt", "b\n", "second feature commit")
	if _, err := c.Switch(ctx, loc.BranchURL(KindTrunk, "")); err != nil {
		t.Fatal(err)
	}

	// Both feature commits are eligible for merging into trunk.
	eligible, err := c.EligibleLog(ctx, brURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eligible, "feature work") || !strings.Contains(eligible, "second feature commit") {
		t.Fatalf("eligible = %q", eligible)
	}

	// Cherry-pick only the first one.
	if _, err := c.CherryPick(ctx, brURL, featRev); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(c.Dir, "a.txt"))
	if string(data) != "1\n2\n3\nfeature\n" {
		t.Fatalf("after cherry-pick a.txt = %q", data)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("cherry-pick must not bring b.txt")
	}
	if _, err := c.Commit(ctx, "cherry-pick", "."); err != nil {
		t.Fatal(err)
	}

	// A full merge brings the rest (the cherry-picked revision is recorded
	// in mergeinfo and not applied twice).
	mustUpdate(t, c)
	if _, err := c.Merge(ctx, brURL); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "b.txt")); err != nil {
		t.Fatal("merge should bring b.txt")
	}
	if data, _ := os.ReadFile(filepath.Join(c.Dir, "a.txt")); string(data) != "1\n2\n3\nfeature\n" {
		t.Fatalf("after merge a.txt = %q (cherry-pick applied twice?)", data)
	}
	if _, err := c.Commit(ctx, "merge feature", "."); err != nil {
		t.Fatal(err)
	}

	// Delete the branch.
	if _, err := c.DeleteURL(ctx, brURL, "Delete feature"); err != nil {
		t.Fatal(err)
	}
	bs, _ = c.Branches(ctx, loc)
	if len(bs) != 2 {
		t.Fatalf("after delete: %+v", bs)
	}
}

func TestRevertRevisionAndUpdatePath(t *testing.T) {
	c, trunk := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "one\n", "v1")
	r2 := commitFile(t, c, "a.txt", "two\n", "v2")
	commitFile(t, c, "b.txt", "b\n", "add b")
	mustUpdate(t, c)

	// Reverse-merge r2: a.txt goes back to "one", b.txt untouched.
	if _, err := c.RevertRevision(ctx, trunk, r2); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(c.Dir, "a.txt")); string(data) != "one\n" {
		t.Fatalf("after reverse merge a.txt = %q", data)
	}
	if err := c.Revert(ctx, "a.txt", "."); err != nil {
		t.Fatal(err)
	}

	// Update a single file to an older revision.
	if _, err := c.Update(ctx, r2-1, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(c.Dir, "a.txt")); string(data) != "one\n" {
		t.Fatalf("after update -r a.txt = %q", data)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "b.txt")); err != nil {
		t.Fatal("updating a.txt must not touch b.txt")
	}
}

func TestBlame(t *testing.T) {
	c, _ := newTestWC(t)
	ctx := context.Background()
	r1 := commitFile(t, c, "a.txt", "one\ntwo\n", "first")
	r2 := commitFile(t, c, "a.txt", "one\nTWO\nthree\n", "second")
	mustUpdate(t, c)

	lines, err := c.Blame(ctx, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %+v", lines)
	}
	want := []struct {
		rev  int
		text string
	}{{r1, "one"}, {r2, "TWO"}, {r2, "three"}}
	for i, w := range want {
		if lines[i].Revision != w.rev || lines[i].Text != w.text || lines[i].Author == "" {
			t.Errorf("line %d = %+v, want r%d %q", i, lines[i], w.rev, w.text)
		}
	}
}

func TestBlameLocallyModified(t *testing.T) {
	c, _ := newTestWC(t)
	r := commitFile(t, c, "a.txt", "one\ntwo\n", "first")
	write(t, filepath.Join(c.Dir, "a.txt"), "one\nLOCAL\ntwo\n")

	lines, err := c.Blame(context.Background(), "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		rev  int
		text string
	}{{r, "one"}, {0, "LOCAL"}, {r, "two"}}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i, w := range want {
		if lines[i].Revision != w.rev || lines[i].Text != w.text {
			t.Errorf("line %d = %+v, want r%d %q", i, lines[i], w.rev, w.text)
		}
	}
}

func TestLogSearchAndRange(t *testing.T) {
	c, trunk := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "1\n", "fix: parser bug")
	commitFile(t, c, "a.txt", "2\n", "feat: new panel")
	r4 := commitFile(t, c, "a.txt", "3\n", "fix: another BUG")

	revs, err := c.Log(ctx, trunk, LogOptions{Search: "bug"})
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Number != r4 {
		t.Fatalf("search = %+v", revs)
	}
	revs, err = c.Log(ctx, trunk, LogOptions{From: r4 - 1, To: r4 - 1})
	if err != nil || len(revs) != 1 || revs[0].Summary() != "feat: new panel" {
		t.Fatalf("range = %+v, %v", revs, err)
	}
	r, err := c.RevisionInfo(ctx, trunk, r4)
	if err != nil || r.Summary() != "fix: another BUG" {
		t.Fatalf("revision info = %+v, %v", r, err)
	}
}

func TestChangelists(t *testing.T) {
	c, _ := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "1\n", "a")
	commitFile(t, c, "b.txt", "1\n", "b")
	write(t, filepath.Join(c.Dir, "a.txt"), "2\n")
	write(t, filepath.Join(c.Dir, "b.txt"), "2\n")

	if err := c.SetChangelist(ctx, "wip", "a.txt"); err != nil {
		t.Fatal(err)
	}
	files, _ := c.Status(ctx)
	cls := map[string]string{}
	for _, f := range files {
		cls[f.Path] = f.Changelist
	}
	if cls["a.txt"] != "wip" || cls["b.txt"] != "" {
		t.Fatalf("changelists = %v", cls)
	}
	if err := c.RemoveChangelist(ctx, "a.txt"); err != nil {
		t.Fatal(err)
	}
	files, _ = c.Status(ctx)
	for _, f := range files {
		if f.Changelist != "" {
			t.Fatalf("after remove: %+v", f)
		}
	}
}

func TestRevertSingleHunk(t *testing.T) {
	c, _ := newTestWC(t)
	ctx := context.Background()
	var orig strings.Builder
	for i := 1; i <= 20; i++ {
		orig.WriteString("line\n")
	}
	commitFile(t, c, "a.txt", orig.String(), "base")
	// Two far-apart edits give two hunks.
	lines := strings.Split(orig.String(), "\n")
	lines[1], lines[17] = "FIRST", "SECOND"
	write(t, filepath.Join(c.Dir, "a.txt"), strings.Join(lines, "\n"))

	text, err := c.Diff(ctx, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	diff := ParseDiff(text)
	hunks := DiffHunks(diff)
	if len(hunks) != 2 {
		t.Fatalf("hunks = %+v\n%s", hunks, text)
	}
	patch, err := HunkPatch(diff, hunks[1])
	if err != nil {
		t.Fatal(err)
	}
	pf := filepath.Join(t.TempDir(), "h.patch")
	write(t, pf, patch)
	if _, err := c.Patch(ctx, pf, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(c.Dir, "a.txt"))
	if !strings.Contains(string(data), "FIRST") || strings.Contains(string(data), "SECOND") {
		t.Fatalf("after hunk revert:\n%s", data)
	}
}
