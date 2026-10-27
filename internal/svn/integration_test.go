package svn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestWC creates a repository with a standard layout and returns a client
// for a fresh checkout of trunk, plus the trunk URL.
func newTestWC(t *testing.T) (*Client, string) {
	t.Helper()
	if _, err := exec.LookPath("svnadmin"); err != nil {
		t.Skip("svnadmin not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if out, err := exec.Command("svnadmin", "create", repo).CombinedOutput(); err != nil {
		t.Fatalf("svnadmin create: %v\n%s", err, out)
	}
	url := "file://" + filepath.ToSlash(repo)
	if !strings.HasPrefix(filepath.ToSlash(repo), "/") {
		url = "file:///" + filepath.ToSlash(repo) // Windows drive paths
	}

	c, err := NewClient(root)
	if err != nil {
		t.Skip("svn not installed")
	}
	ctx := context.Background()
	if _, err := c.Run(ctx, "mkdir", "-m", "layout", url+"/trunk", url+"/branches", url+"/tags"); err != nil {
		t.Fatal(err)
	}
	wc := filepath.Join(root, "wc")
	if _, err := c.Run(ctx, "checkout", url+"/trunk", wc); err != nil {
		t.Fatal(err)
	}
	c.Dir = wc
	return c, url + "/trunk"
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statusOf(t *testing.T, c *Client) map[string]Item {
	t.Helper()
	files, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]Item{}
	for _, f := range files {
		m[filepath.ToSlash(f.Path)] = f.Item
	}
	return m
}

func TestWorkflow(t *testing.T) {
	c, trunk := newTestWC(t)
	ctx := context.Background()

	info, err := c.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loc := ParseLocation(info.RelativeURL); loc.Kind != KindTrunk {
		t.Errorf("location = %+v", loc)
	}

	// Add, including a path that looks like a peg revision and a non-ASCII name.
	names := []string{"a.txt", "img@2x.png", "héllo.txt"}
	for _, n := range names {
		write(t, filepath.Join(c.Dir, n), "one\ntwo\n")
	}
	if st := statusOf(t, c); st["a.txt"] != ItemUnversioned {
		t.Fatalf("status before add = %v", st)
	}
	if err := c.Add(ctx, names...); err != nil {
		t.Fatal(err)
	}
	rev, err := c.Commit(ctx, "Add files\n\nwith a body, \"quotes\" and ünïcode", names...)
	if err != nil {
		t.Fatal(err)
	}
	if rev != 2 {
		t.Errorf("commit rev = %d, want 2", rev)
	}
	if st := statusOf(t, c); len(st) != 0 {
		t.Errorf("status after commit = %v", st)
	}

	// Modify, diff, revert.
	write(t, filepath.Join(c.Dir, "a.txt"), "one\nTWO\n")
	diff, err := c.Diff(ctx, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-two") || !strings.Contains(diff, "+TWO") {
		t.Errorf("diff = %q", diff)
	}
	if err := c.Revert(ctx, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if st := statusOf(t, c); len(st) != 0 {
		t.Errorf("status after revert = %v", st)
	}

	// Ignore an unversioned file.
	write(t, filepath.Join(c.Dir, "build.log"), "noise")
	if err := c.Ignore(ctx, "build.log"); err != nil {
		t.Fatal(err)
	}
	if st := statusOf(t, c); st["build.log"] != "" || st["."] == "" {
		t.Errorf("status after ignore = %v (want build.log hidden, . prop-modified)", st)
	}
	// Committing a directory property requires the directory to be at HEAD.
	if _, err := c.Commit(ctx, "ignore", "."); !IsOutOfDate(err) {
		t.Fatalf("expected out-of-date error, got %v", err)
	}
	if _, err := c.Update(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Commit(ctx, "ignore", "."); err != nil {
		t.Fatal(err)
	}

	// Remove.
	if err := c.Remove(ctx, "img@2x.png"); err != nil {
		t.Fatal(err)
	}
	if st := statusOf(t, c); st["img@2x.png"] != ItemDeleted {
		t.Errorf("status after rm = %v", st)
	}
	if _, err := c.Commit(ctx, "rm", "img@2x.png"); err != nil {
		t.Fatal(err)
	}

	// Log.
	revs, err := c.Log(ctx, trunk, LogOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 4 || revs[0].Summary() != "rm" || revs[2].Summary() != "Add files" {
		t.Errorf("log = %+v", revs)
	}
	if _, err := c.RevisionDiff(ctx, trunk, revs[0].Number); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateConflictResolve(t *testing.T) {
	c, trunk := newTestWC(t)
	ctx := context.Background()

	write(t, filepath.Join(c.Dir, "a.txt"), "one\n")
	if err := c.Add(ctx, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Commit(ctx, "add", "a.txt"); err != nil {
		t.Fatal(err)
	}

	// A second working copy commits a conflicting change.
	other := filepath.Join(filepath.Dir(c.Dir), "wc2")
	if _, err := c.Run(ctx, "checkout", trunk, other); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(other, "a.txt"), "theirs\n")
	if _, err := c.Run(ctx, "commit", "-m", "theirs", other); err != nil {
		t.Fatal(err)
	}

	head, err := c.HeadRevision(ctx)
	if err != nil || head != 3 {
		t.Fatalf("head = %d, %v", head, err)
	}

	write(t, filepath.Join(c.Dir, "a.txt"), "mine\n")
	res, err := c.Update(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision != 3 || res.Conflicts != 1 {
		t.Errorf("update = %+v", res)
	}
	files, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || files[0].Path != "a.txt" || !files[0].Conflicted() {
		t.Fatalf("expected a.txt conflicted first, got %+v", files)
	}
	if err := c.Resolve(ctx, "mine-full", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if st := statusOf(t, c); st["a.txt"] != ItemModified {
		t.Errorf("status after resolve = %v", st)
	}
}
