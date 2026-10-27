package svn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProperties(t *testing.T) {
	c, _ := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "x\n", "a")

	multi := "line one\nline two"
	if err := c.SetProp(ctx, "custom:note", multi, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProp(ctx, "svn:eol-style", "native", "a.txt"); err != nil {
		t.Fatal(err)
	}
	props, err := c.Props(ctx, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range props {
		got[p.Name] = p.Value
	}
	if got["custom:note"] != multi || got["svn:eol-style"] != "native" {
		t.Fatalf("props = %q", got)
	}
	if err := c.DeleteProp(ctx, "custom:note", "a.txt"); err != nil {
		t.Fatal(err)
	}
	props, _ = c.Props(ctx, "a.txt")
	if len(props) != 1 {
		t.Fatalf("after delete: %+v", props)
	}
}

func TestLocksAndRemoteStatus(t *testing.T) {
	c, trunk := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "1\n", "a")
	commitFile(t, c, "b.txt", "1\n", "b")
	mustUpdate(t, c)

	// Another working copy changes b.txt, adds c.txt and locks a.txt.
	other := filepath.Join(filepath.Dir(c.Dir), "other")
	if _, err := c.Run(ctx, "checkout", trunk, other); err != nil {
		t.Fatal(err)
	}
	o := &Client{Dir: other, Bin: c.Bin, Timeout: c.Timeout}
	commitFile(t, o, "b.txt", "2\n", "remote edit")
	commitFile(t, o, "c.txt", "new\n", "remote add")
	if err := o.Lock(ctx, "editing", "a.txt"); err != nil {
		t.Fatal(err)
	}

	files, head, err := c.StatusRemote(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if head != 5 {
		t.Errorf("against = %d, want 5", head)
	}
	st := map[string]FileStatus{}
	for _, f := range files {
		st[f.Path] = f
	}
	if f := st["b.txt"]; !f.RemoteChanged || f.RemoteItem != ItemModified || f.LocalChange() {
		t.Errorf("b.txt = %+v", f)
	}
	if f := st["c.txt"]; !f.RemoteChanged || f.RemoteItem != ItemAdded {
		t.Errorf("c.txt = %+v", f)
	}
	if f := st["a.txt"]; f.RemoteLockOwner == "" || f.Locked {
		t.Errorf("a.txt should be locked by the other working copy: %+v", f)
	}

	// Our working copy can't lock it without stealing; unlock from the owner.
	if err := c.Lock(ctx, "mine", "a.txt"); err == nil {
		t.Error("locking a file locked elsewhere should fail")
	}
	if err := o.Unlock(ctx, false, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Lock(ctx, "mine", "a.txt"); err != nil {
		t.Fatal(err)
	}
	local, _ := c.Status(ctx)
	if len(local) != 1 || !local[0].Locked {
		t.Fatalf("after lock: %+v", local)
	}
	if err := c.Unlock(ctx, false, "a.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestBrowseCatExportCheckout(t *testing.T) {
	c, trunk := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "v1\n", "v1")
	if err := os.Mkdir(filepath.Join(c.Dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(c.Dir, "sub", "b.txt"), "b\n")
	if err := c.Add(ctx, "sub"); err != nil {
		t.Fatal(err)
	}
	r3, err := c.Commit(ctx, "sub", "sub")
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, c, "a.txt", "v2\n", "v2")

	entries, err := c.List(ctx, trunk, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].Dir || entries[0].Name != "sub" || entries[1].Name != "a.txt" {
		t.Fatalf("entries = %+v", entries)
	}
	old, err := c.Cat(ctx, trunk+"/a.txt", r3)
	if err != nil || string(old) != "v1\n" {
		t.Fatalf("cat @r%d = %q, %v", r3, old, err)
	}

	dest := filepath.Join(t.TempDir(), "exp")
	if err := c.Export(ctx, trunk, r3, dest); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(data) != "v1\n" {
		t.Fatalf("export a.txt = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dest, ".svn")); !os.IsNotExist(err) {
		t.Fatal("export must not be a working copy")
	}
	co := filepath.Join(t.TempDir(), "co")
	if err := c.Checkout(ctx, trunk, 0, co); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(co, ".svn")); err != nil {
		t.Fatal("checkout should be a working copy")
	}
}

func TestCleanupAndRelocate(t *testing.T) {
	c, trunk := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "1\n", "a")
	write(t, filepath.Join(c.Dir, "junk.tmp"), "x")
	if err := c.Cleanup(ctx, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "junk.tmp")); err != nil {
		t.Fatal("plain cleanup must keep unversioned files")
	}
	if err := c.Cleanup(ctx, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "junk.tmp")); !os.IsNotExist(err) {
		t.Fatal("cleanup --remove-unversioned should delete junk.tmp")
	}

	// Move the repository and relocate the working copy to it.
	oldRoot := strings.TrimSuffix(trunk, "/trunk")
	oldPath := strings.TrimPrefix(oldRoot, "file://")
	newPath := oldPath + "-moved"
	if err := os.Rename(filepath.FromSlash(strings.TrimPrefix(oldPath, "/")), filepath.FromSlash(strings.TrimPrefix(newPath, "/"))); err != nil {
		// Unix paths keep their leading slash.
		if err := os.Rename(oldPath, newPath); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Relocate(ctx, oldRoot, "file://"+newPath); err != nil {
		t.Fatal(err)
	}
	info, err := c.Info(ctx)
	if err != nil || info.RepoRoot != "file://"+newPath {
		t.Fatalf("after relocate: %+v %v", info, err)
	}
}

func TestShelvePatchRoundTrip(t *testing.T) {
	c, _ := newTestWC(t)
	ctx := context.Background()
	commitFile(t, c, "a.txt", "a\n", "a")
	commitFile(t, c, "bin.dat", "\x00\x01\x02", "bin")
	write(t, filepath.Join(c.Dir, "a.txt"), "a2\n")
	write(t, filepath.Join(c.Dir, "bin.dat"), "\x00\x09\x09")
	write(t, filepath.Join(c.Dir, "new.txt"), "n\n")
	if err := c.Add(ctx, "new.txt"); err != nil {
		t.Fatal(err)
	}

	patch, err := c.DiffGit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pf := filepath.Join(t.TempDir(), "shelf.patch")
	write(t, pf, patch)
	if err := c.Revert(ctx, "."); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(c.Dir, "new.txt"))
	if files, _ := c.Status(ctx); len(files) != 0 {
		t.Fatalf("after revert: %+v", files)
	}

	info, _ := c.Info(ctx)
	if d := PathDepth(info.RelativeURL); d != 1 {
		t.Fatalf("depth of %s = %d", info.RelativeURL, d)
	}
	if _, err := c.Patch(ctx, pf, PatchOptions{Strip: PathDepth(info.RelativeURL)}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a.txt": "a2\n", "bin.dat": "\x00\x09\x09", "new.txt": "n\n"} {
		if data, _ := os.ReadFile(filepath.Join(c.Dir, name)); string(data) != want {
			t.Errorf("%s = %q, want %q", name, data, want)
		}
	}
	files, _ := c.Status(ctx)
	for _, f := range files {
		if f.Path == "new.txt" && f.Item != ItemAdded {
			t.Errorf("new.txt should be scheduled for addition again: %+v", f)
		}
	}
}
