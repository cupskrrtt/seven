package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cupskrrtt/seven/internal/svn"
)

func rowNames(rows []fileRow) string {
	var out []string
	for _, r := range rows {
		name := strings.Repeat("  ", r.depth) + r.node.name
		if r.node.dir {
			name += "/"
		}
		out = append(out, name)
	}
	return strings.Join(out, "\n")
}

func TestBuildTree(t *testing.T) {
	files := []svn.FileStatus{
		{Path: "z.txt", Item: svn.ItemModified},
		{Path: filepath.FromSlash("src/app/ui/view.go"), Item: svn.ItemModified},
		{Path: filepath.FromSlash("src/app/ui/model.go"), Item: svn.ItemAdded},
		{Path: filepath.FromSlash("src/main.go"), Item: svn.ItemModified},
		{Path: "docs", Item: svn.ItemAdded}, // an added folder with an entry of its own
		{Path: filepath.FromSlash("docs/a.md"), Item: svn.ItemAdded},
		{Path: "a.txt", Item: svn.ItemUnversioned},
	}
	root := buildTree(files)
	got := rowNames(flatten(root, 0, map[string]bool{}, nil))
	want := strings.Join([]string{
		"docs/",
		"  a.md",
		"src/",
		"  app/ui/", // single-child chain compressed
		"    model.go",
		"    view.go",
		"  main.go",
		"a.txt",
		"z.txt",
	}, "\n")
	if got != want {
		t.Fatalf("tree:\n%s\nwant:\n%s", got, want)
	}
	// Collapsing a folder hides its contents.
	got = rowNames(flatten(root, 0, map[string]bool{"src": true}, nil))
	if strings.Contains(got, "main.go") || !strings.Contains(got, "src/") {
		t.Fatalf("collapsed tree:\n%s", got)
	}
	if n := len(filesUnder(root.children[1])); n != 3 {
		t.Fatalf("files under src = %d", n)
	}
	if got := topLevel([]string{"docs", filepath.FromSlash("docs/a.md"), "z.txt"}); len(got) != 2 {
		t.Fatalf("topLevel = %v", got)
	}
}

// setupTree adds nested changes: src/app/x.go and src/app/y.go modified,
// src/new.txt unversioned.
func setupTree(t *testing.T) *Model {
	t.Helper()
	m, _ := setup(t)
	wc := m.client.Dir
	os.MkdirAll(filepath.Join(wc, "src", "app"), 0o755)
	for _, f := range []string{"x.go", "y.go"} {
		os.WriteFile(filepath.Join(wc, "src", "app", f), []byte("orig\n"), 0o644)
	}
	sh(t, wc, "svn", "add", "-q", "src")
	sh(t, wc, "svn", "commit", "-q", "-m", "add src", "src")
	for _, f := range []string{"x.go", "y.go"} {
		os.WriteFile(filepath.Join(wc, "src", "app", f), []byte("changed\n"), 0o644)
	}
	os.WriteFile(filepath.Join(wc, "src", "new.txt"), []byte("n\n"), 0o644)
	press(t, m, "r")
	return m
}

func TestFileTreeUI(t *testing.T) {
	m := setupTree(t)
	press(t, m, "2", "g")
	assertScreen(t, m, "▼ src/", "▼ app/", "M   x.go", "?   new.txt", "M   a.txt")

	// The folder row shows the combined diff.
	assertScreen(t, m, "Diff · src/", "+changed", "Unversioned:", "? src")

	// Collapse and expand.
	press(t, m, "enter")
	assertScreen(t, m, "▶ src/ (3)")
	if len(m.fileRows) != 3 { // ▶ src/, a.txt, b.txt
		t.Fatalf("collapsed rows:\n%s", rowNames(m.fileRows))
	}
	press(t, m, "enter")
	assertScreen(t, m, "x.go")
	press(t, m, "-")
	assertScreen(t, m, "▶ src/")
	press(t, m, "=")
	assertScreen(t, m, "x.go")

	// Selecting a folder selects its versioned files only.
	press(t, m, "g", " ")
	if len(m.selected) != 2 || !m.selected[filepath.FromSlash("src/app/x.go")] {
		t.Fatalf("selected = %v", m.selected)
	}
	assertScreen(t, m, "● ▼ src/", "2 selected")
	press(t, m, "g", " ")
	if len(m.selected) != 0 {
		t.Fatalf("second space should clear, selected = %v", m.selected)
	}
	// Partial selection shows ◐ on the folder.
	selectFile(t, m, filepath.FromSlash("src/app/x.go"))
	press(t, m, " ")
	assertScreen(t, m, "◐ ▼ src/")

	// Flat view and back.
	press(t, m, "`")
	assertScreen(t, m, filepath.FromSlash("src/app/x.go"))
	for _, r := range m.fileRows {
		if r.node.dir {
			t.Fatalf("flat view has a folder row:\n%s", rowNames(m.fileRows))
		}
	}
	press(t, m, "`")
	assertScreen(t, m, "▼ src/")
}

func TestDiscardFolder(t *testing.T) {
	m := setupTree(t)
	press(t, m, "2", "g", "d")
	assertScreen(t, m, "Discard all changes under src/", "2 versioned change(s)", "1 unversioned item(s)")
	press(t, m, "y")
	assertScreen(t, m, "Discarded 3 item(s) under src/", "backup:")
	if got := readWC(t, m, filepath.Join("src", "app", "x.go")); got != "orig\n" {
		t.Fatalf("x.go = %q", got)
	}
	if _, err := os.Stat(filepath.Join(m.client.Dir, "src", "new.txt")); !os.IsNotExist(err) {
		t.Fatal("new.txt should be deleted")
	}
	// a.txt outside the folder is untouched.
	if got := readWC(t, m, "a.txt"); got != "one\nLOCAL\n" {
		t.Fatalf("a.txt = %q", got)
	}
	for _, rel := range []string{"src/app/x.go", "src/new.txt"} {
		copies, _ := filepath.Glob(filepath.Join(m.backupRoot, "*", "files", filepath.FromSlash(rel)))
		if len(copies) != 1 {
			t.Fatalf("backup of %s = %v", rel, copies)
		}
	}
}

func TestAddFolderUnversioned(t *testing.T) {
	m := setupTree(t)
	press(t, m, "2", "g", "a")
	assertScreen(t, m, "Added 1 item(s) under src", "A   new.txt")
}
