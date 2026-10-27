package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func typeText(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		press(t, m, string(r))
	}
}

// commitAll commits every change in the working copy via the UI.
func commitAll(t *testing.T, m *Model, msg string) {
	t.Helper()
	press(t, m, "r", "2", "c") // refresh first: tests change files behind seven's back
	assertScreen(t, m, "Commit all")
	typeText(t, m, msg)
	press(t, m, "enter")
	assertScreen(t, m, "Committed r")
}

// selectFile moves the Files cursor to path.
func selectFile(t *testing.T, m *Model, path string) {
	t.Helper()
	press(t, m, "2", "g")
	for i := 0; i < len(m.fileRows); i++ {
		if f, _ := m.currentFile(); f.Path == path {
			return
		}
		press(t, m, "j")
	}
	t.Fatalf("file %s not in %+v", path, m.files)
}

// selectBranch moves the Branches cursor to the branch labelled name.
func selectBranch(t *testing.T, m *Model, name string) {
	t.Helper()
	press(t, m, "3", "g")
	for i := 0; i < len(m.branches); i++ {
		if b, _ := m.currentBranch(); branchLabel(b) == name {
			return
		}
		press(t, m, "j")
	}
	t.Fatalf("branch %s not in %+v", name, m.branches)
}

func TestBranchCreateSwitchMergeCherryPick(t *testing.T) {
	m, _ := setup(t)
	press(t, m, "3")
	assertScreen(t, m, "[3] Branches", "* trunk")

	// Create two branches from trunk.
	for _, name := range []string{"feat", "other"} {
		press(t, m, "n")
		assertScreen(t, m, "New branch from trunk")
		typeText(t, m, name)
		press(t, m, "enter", "enter")
		assertScreen(t, m, "Created branch "+name)
	}

	// Switch to feat (local changes come along) and commit there.
	selectBranch(t, m, "feat")
	press(t, m, " ", "y")
	assertScreen(t, m, "Switched to feat", "branch: feat", "* feat")
	commitAll(t, m, "work on feat")

	// Back to trunk; feat's commit is listed as not merged yet.
	selectBranch(t, m, "trunk")
	press(t, m, " ", "y")
	assertScreen(t, m, "Switched to trunk", "* trunk")
	selectBranch(t, m, "feat")
	press(t, m, "e")
	assertScreen(t, m, "Not yet merged from feat", "work on feat")
	press(t, m, "esc")

	// Cherry-pick it from feat's log.
	press(t, m, "enter")
	assertScreen(t, m, "[4] Log · feat", "work on feat")
	press(t, m, "c")
	assertScreen(t, m, "Merge r", "from feat")
	press(t, m, "y")
	assertScreen(t, m, "Cherry-picked r", "M   a.txt")
	if got := readWC(t, m, "a.txt"); got != "one\nLOCAL\n" {
		t.Fatalf("a.txt = %q", got)
	}

	// Switching branches resets the log to the working copy's own history.
	selectBranch(t, m, "other")
	press(t, m, " ", "y")
	if m.logTarget != "" {
		t.Fatalf("log target should reset after switch, got %q", m.logTarget)
	}

	// Delete a branch.
	selectBranch(t, m, "feat")
	press(t, m, "D", "y")
	assertScreen(t, m, "Deleted feat")
	for _, b := range m.branches {
		if b.Name == "feat" {
			t.Fatalf("feat still listed: %+v", m.branches)
		}
	}
}

func TestMergeBranch(t *testing.T) {
	m, _ := setup(t)
	press(t, m, "3", "n")
	typeText(t, m, "feat")
	press(t, m, "enter", "enter")
	selectBranch(t, m, "feat")
	press(t, m, " ", "y")
	commitAll(t, m, "feat change")
	selectBranch(t, m, "trunk")
	press(t, m, " ", "y", "u") // update so the working copy is single-revision

	selectBranch(t, m, "feat")
	press(t, m, "m")
	assertScreen(t, m, "Merge all unmerged changes from feat")
	press(t, m, "y")
	assertScreen(t, m, "Merged feat — review and commit")
	if got := readWC(t, m, "a.txt"); got != "one\nLOCAL\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(m.client.Dir, "b.txt")); err != nil {
		t.Fatal("merge should bring b.txt, committed on feat")
	}
}

func TestBlameJumpToRevision(t *testing.T) {
	m, _ := setup(t)
	selectFile(t, m, "a.txt")
	press(t, m, "b")
	// a.txt is "one\nLOCAL\n": the second line is a local change.
	assertScreen(t, m, "Blame · a.txt", "r2", "│ one", "uncommitted", "│ LOCAL")
	press(t, m, "j", "enter")
	assertScreen(t, m, "That line is an uncommitted local change")
	press(t, m, "k")
	press(t, m, "enter")
	assertScreen(t, m, "Revision r2", "Add files", "A /trunk/a.txt")
	if m.blame != nil {
		t.Fatal("blame should close when showing a revision")
	}
	press(t, m, "esc")
	if m.focus != panelFiles {
		t.Fatalf("esc should return to Files, focus=%v", m.focus)
	}
}

func TestRevertHunk(t *testing.T) {
	m, _ := setup(t)
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, "line")
	}
	os.WriteFile(filepath.Join(m.client.Dir, "h.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	sh(t, m.client.Dir, "svn", "add", "-q", "h.txt")
	sh(t, m.client.Dir, "svn", "commit", "-q", "-m", "h", "h.txt")
	lines[1], lines[17] = "FIRST", "SECOND"
	os.WriteFile(filepath.Join(m.client.Dir, "h.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	press(t, m, "r")

	selectFile(t, m, "h.txt")
	press(t, m, "enter")
	press(t, m, "d")
	assertScreen(t, m, "Select a hunk with ] / [ first")
	press(t, m, "n", "n")
	assertScreen(t, m, "hunk 2/2")
	press(t, m, "d")
	assertScreen(t, m, "Discard hunk 2 of 2 in h.txt?")
	press(t, m, "y")
	assertScreen(t, m, "Reverted hunk 2 of h.txt", "backup:")

	got := readWC(t, m, "h.txt")
	if !strings.Contains(got, "FIRST") || strings.Contains(got, "SECOND") {
		t.Fatalf("h.txt after hunk revert:\n%s", got)
	}
	// The diff reloaded in place and now has a single hunk.
	if m.focus != panelMain || len(m.mainHunks) != 1 {
		t.Fatalf("focus=%v hunks=%d", m.focus, len(m.mainHunks))
	}
	// The backup holds the file as it was before the revert.
	backups, _ := filepath.Glob(filepath.Join(m.backupRoot, "*", "files", "h.txt"))
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	if data, _ := os.ReadFile(backups[0]); !strings.Contains(string(data), "SECOND") {
		t.Fatalf("backup content = %q", data)
	}
}

func TestRevertAndDeleteKeepBackups(t *testing.T) {
	m, _ := setup(t)
	selectFile(t, m, "a.txt")
	press(t, m, "d")
	assertScreen(t, m, "A backup is saved first")
	press(t, m, "y")
	assertScreen(t, m, "Reverted a.txt", "backup:")
	if got := readWC(t, m, "a.txt"); got != "one\n" {
		t.Fatalf("a.txt = %q", got)
	}

	selectFile(t, m, "b.txt")
	press(t, m, "d", "y")
	assertScreen(t, m, "Deleted b.txt")
	if _, err := os.Stat(filepath.Join(m.client.Dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("b.txt should be deleted")
	}

	for name, want := range map[string]string{"a.txt": "one\nLOCAL\n", "b.txt": "new\n"} {
		copies, _ := filepath.Glob(filepath.Join(m.backupRoot, "*", "files", name))
		if len(copies) != 1 {
			t.Fatalf("%s backups = %v", name, copies)
		}
		if data, _ := os.ReadFile(copies[0]); string(data) != want {
			t.Errorf("%s backup = %q, want %q", name, data, want)
		}
	}
	patches, _ := filepath.Glob(filepath.Join(m.backupRoot, "*", "changes.patch"))
	if len(patches) != 1 {
		t.Fatalf("patches = %v (only the versioned revert has a diff)", patches)
	}
}

func TestLogFiltersAndFileLog(t *testing.T) {
	m, _ := setup(t)
	commitAll(t, m, "fix: crash on start")
	os.WriteFile(filepath.Join(m.client.Dir, "c.txt"), []byte("c\n"), 0o644)
	sh(t, m.client.Dir, "svn", "add", "-q", "c.txt")
	commitAll(t, m, "feat: c file")

	press(t, m, "4", "/")
	assertScreen(t, m, "Filter log")
	typeText(t, m, "CRASH")
	press(t, m, "enter", "enter", "enter")
	assertScreen(t, m, `[4] Log · "CRASH"`, "fix: crash on start")
	if len(m.revs) != 1 {
		t.Fatalf("search should match one revision, got %+v", m.revs)
	}

	press(t, m, "esc")
	assertScreen(t, m, "Log reset to the working copy")
	if len(m.revs) < 4 {
		t.Fatalf("reset log = %d revisions", len(m.revs))
	}

	// Revision range.
	press(t, m, "/", "enter", "enter")
	typeText(t, m, "2:3")
	press(t, m, "enter")
	if len(m.revs) != 2 || m.revs[0].Number != 3 || m.revs[1].Number != 2 {
		t.Fatalf("range 2:3 = %+v", m.revs)
	}
	press(t, m, "esc")

	// File log from the Files panel: only revisions touching c.txt.
	os.WriteFile(filepath.Join(m.client.Dir, "c.txt"), []byte("c2\n"), 0o644)
	press(t, m, "r")
	selectFile(t, m, "c.txt")
	press(t, m, "l")
	assertScreen(t, m, "[4] Log · c.txt", "feat: c file")
	if len(m.revs) != 1 || m.focus != panelLog {
		t.Fatalf("file log = %+v focus=%v", m.revs, m.focus)
	}
}

func TestRevertRevisionAndUpdateFile(t *testing.T) {
	m, _ := setup(t)
	commitAll(t, m, "local edit") // r3: a.txt = one\nLOCAL
	press(t, m, "u")

	// Undo r3 via reverse merge.
	press(t, m, "4", "g")
	assertScreen(t, m, "local edit")
	press(t, m, "R")
	assertScreen(t, m, "Undo all of r3")
	press(t, m, "y")
	assertScreen(t, m, "Reverted r3 in the working copy")
	if got := readWC(t, m, "a.txt"); got != "one\n" {
		t.Fatalf("after reverse merge a.txt = %q", got)
	}
	selectFile(t, m, "a.txt")
	press(t, m, "d", "y") // discard the reverse merge again

	// Update just a.txt to r2 from its log (path filter).
	press(t, m, "4", "/", "enter")
	typeText(t, m, "a.txt")
	press(t, m, "enter", "enter")
	assertScreen(t, m, "[4] Log · a.txt")
	press(t, m, "j", "U")
	assertScreen(t, m, "Update to r2", "a.txt → r2", "Working copy → r2")
	press(t, m, "f")
	assertScreen(t, m, "Updated a.txt to r2")
	if got := readWC(t, m, "a.txt"); got != "one\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(m.client.Dir, "b.txt")); err != nil {
		t.Fatal("b.txt must be untouched")
	}
}

func TestChangelists(t *testing.T) {
	m, _ := setup(t)
	os.WriteFile(filepath.Join(m.client.Dir, "b.txt"), []byte("new\n"), 0o644)
	sh(t, m.client.Dir, "svn", "add", "-q", "b.txt")
	press(t, m, "r")

	selectFile(t, m, "a.txt")
	press(t, m, "L")
	assertScreen(t, m, "Changelist · a.txt", "New changelist")
	press(t, m, "n")
	typeText(t, m, "wip")
	press(t, m, "enter")
	assertScreen(t, m, "Moved a.txt to changelist wip", "a.txt [wip]")

	// b.txt can now join the existing changelist via its number.
	selectFile(t, m, "b.txt")
	press(t, m, "L")
	assertScreen(t, m, "Move to wip")
	press(t, m, "1")
	assertScreen(t, m, "b.txt [wip]")

	press(t, m, "L", "s")
	assertScreen(t, m, "Selected 2 file(s) in wip")
	press(t, m, "c")
	assertScreen(t, m, "Commit 2 file(s)")
	press(t, m, "esc")

	press(t, m, "L", "x")
	assertScreen(t, m, "Removed b.txt from wip")
}
