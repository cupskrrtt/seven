package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/svn"
)

func sh(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// setup returns a model for a trunk checkout containing a committed,
// locally modified a.txt and an unversioned b.txt, plus the trunk URL.
func setup(t *testing.T) (*Model, string) {
	t.Helper()
	if _, err := exec.LookPath("svnadmin"); err != nil {
		t.Skip("svn not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	sh(t, root, "svnadmin", "create", repo)
	url := "file://" + filepath.ToSlash(repo)
	if !strings.HasPrefix(url, "file:///") {
		url = "file:///" + filepath.ToSlash(repo)
	}
	sh(t, root, "svn", "mkdir", "-q", "-m", "layout", url+"/trunk")
	wc := filepath.Join(root, "wc")
	sh(t, root, "svn", "checkout", "-q", url+"/trunk", wc)
	os.WriteFile(filepath.Join(wc, "a.txt"), []byte("one\n"), 0o644)
	sh(t, wc, "svn", "add", "-q", "a.txt")
	sh(t, wc, "svn", "commit", "-q", "-m", "Add files")
	os.WriteFile(filepath.Join(wc, "a.txt"), []byte("one\nLOCAL\n"), 0o644)
	os.WriteFile(filepath.Join(wc, "b.txt"), []byte("new\n"), 0o644)

	client, err := svn.NewClient(wc)
	if err != nil {
		t.Skip(err)
	}
	m := New(client, nil)
	m.refreshEvery = 0
	m.backupRoot = filepath.Join(root, "backups")
	m.shelfRoot = filepath.Join(root, "shelves")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	drain(t, m, m.Init())
	return m, url + "/trunk"
}

// drain runs cmd and every command produced in response, synchronously.
// Cursor-blink ticks from text inputs are dropped so this terminates.
func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 200 {
			t.Fatal("drain: too many steps")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("drain: command timed out")
		}
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			if strings.Contains(fmt.Sprintf("%T", msg), "cursor.") {
				continue
			}
			_, next := m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		_, cmd := m.Update(msg)
		drain(t, m, cmd)
	}
}

func screen(m *Model) string { return ansi.Strip(m.View()) }

func assertScreen(t *testing.T, m *Model, want ...string) {
	t.Helper()
	s := screen(m)
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Fatalf("screen missing %q:\n%s", w, s)
		}
	}
}

func TestInitialRender(t *testing.T) {
	m, _ := setup(t)
	assertScreen(t, m, "trunk", "r2", "M   a.txt", "?   b.txt", "Add files", "+LOCAL", "Diff · a.txt")
}

func TestAddAndCommit(t *testing.T) {
	m, _ := setup(t)

	press(t, m, "j") // b.txt
	assertScreen(t, m, "Diff · b.txt", "+new")
	press(t, m, "a")
	assertScreen(t, m, "Added b.txt", "A   b.txt")

	// Select only b.txt and commit it.
	press(t, m, " ")
	if !m.selected["b.txt"] || len(m.selected) != 1 {
		t.Fatalf("selected = %v", m.selected)
	}
	press(t, m, "c")
	assertScreen(t, m, "Commit 1 file(s)")
	press(t, m, "m", "y", " ", "c", "o", "m", "m", "i", "t", "enter")

	assertScreen(t, m, "Committed r3", "my commit")
	if len(m.files) != 1 || m.files[0].Path != "a.txt" || len(m.selected) != 0 {
		t.Fatalf("after commit files = %+v selected = %v", m.files, m.selected)
	}
}

func TestRevertNeedsConfirm(t *testing.T) {
	m, _ := setup(t)
	press(t, m, "d")
	assertScreen(t, m, "Discard all local changes to a.txt?")
	press(t, m, "esc")
	if len(m.files) != 2 {
		t.Fatalf("esc should not revert, files = %+v", m.files)
	}
	press(t, m, "d", "y")
	assertScreen(t, m, "Reverted a.txt")
	if len(m.files) != 1 || m.files[0].Path != "b.txt" {
		t.Fatalf("files = %+v", m.files)
	}
}

func TestUpdateBehindHead(t *testing.T) {
	m, trunk := setup(t)
	other := filepath.Join(t.TempDir(), "other")
	sh(t, ".", "svn", "checkout", "-q", trunk, other)
	os.WriteFile(filepath.Join(other, "c.txt"), []byte("remote\n"), 0o644)
	sh(t, other, "svn", "add", "-q", "c.txt")
	sh(t, other, "svn", "commit", "-q", "-m", "Remote change")

	press(t, m, "r")
	assertScreen(t, m, "behind HEAD r3", "Remote change")
	press(t, m, "u")
	assertScreen(t, m, "Updated to r3", "(up to date)")
}

func TestLogPanelShowsRevision(t *testing.T) {
	m, _ := setup(t)
	press(t, m, "4")
	assertScreen(t, m, "Revision r2", "Author:", "A /trunk/a.txt", "+one")
	press(t, m, "?")
	assertScreen(t, m, "Keybindings")
	press(t, m, "esc", "q")
}

// setupConflict returns a model whose working copy has f.txt in a text
// conflict with two hunks: line 2 and line 7 changed on both sides.
func setupConflict(t *testing.T) *Model {
	t.Helper()
	m, trunk := setup(t)
	wc := m.client.Dir
	base := "1\n2\n3\n4\n5\n6\n7\n8\n"
	os.WriteFile(filepath.Join(wc, "f.txt"), []byte(base), 0o644)
	sh(t, wc, "svn", "add", "-q", "f.txt")
	sh(t, wc, "svn", "commit", "-q", "-m", "add f", "f.txt")

	other := filepath.Join(t.TempDir(), "other")
	sh(t, ".", "svn", "checkout", "-q", trunk, other)
	os.WriteFile(filepath.Join(other, "f.txt"), []byte("1\nTHEIRS2\n3\n4\n5\n6\nTHEIRS7\n8\n"), 0o644)
	sh(t, other, "svn", "commit", "-q", "-m", "theirs")

	os.WriteFile(filepath.Join(wc, "f.txt"), []byte("1\nMINE2\n3\n4\n5\n6\nMINE7\n8\n"), 0o644)
	press(t, m, "u")
	assertScreen(t, m, "1 conflict(s)", "C   f.txt")
	if m.files[0].Path != "f.txt" {
		t.Fatalf("conflicted file should sort first: %+v", m.files)
	}
	for _, f := range m.files {
		if strings.HasPrefix(f.Path, "f.txt.") {
			t.Fatalf("conflict artifact %s should be hidden", f.Path)
		}
	}
	selectFile(t, m, "f.txt")
	return m
}

func readWC(t *testing.T, m *Model, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.client.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestConflictHunkPicker(t *testing.T) {
	m := setupConflict(t)

	press(t, m, "enter")
	assertScreen(t, m, "Conflict · f.txt · 1/2", "← mine (m)", "← theirs (t)", "MINE2", "THEIRS2")

	press(t, m, "m")
	assertScreen(t, m, "Kept mine for conflict 1", "Conflict · f.txt · 1/1")

	// Undo, then pick differently.
	press(t, m, "z")
	assertScreen(t, m, "Conflict · f.txt · 1/2")
	press(t, m, "b") // both for line 2
	press(t, m, "t") // theirs for line 7
	assertScreen(t, m, "All conflicts picked", "Mark it resolved now?")
	if got, want := readWC(t, m, "f.txt"), "1\nMINE2\nTHEIRS2\n3\n4\n5\n6\nTHEIRS7\n8\n"; got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}

	press(t, m, "y")
	assertScreen(t, m, "Resolved f.txt", "M   f.txt")
	if m.conflict != nil || m.focus != panelFiles {
		t.Fatalf("picker should close: conflict=%v focus=%v", m.conflict, m.focus)
	}
	if _, err := os.Stat(filepath.Join(m.client.Dir, "f.txt.mine")); !os.IsNotExist(err) {
		t.Errorf("svn resolve should remove artifacts, stat err = %v", err)
	}
}

func TestResolveMenuTheirsFull(t *testing.T) {
	m := setupConflict(t)

	press(t, m, "R")
	assertScreen(t, m, "Resolve f.txt", "Pick hunks", "Mine (full)", "Theirs (full)", "Mine for conflicts", "Base")

	// Theirs (full) discards local work, so it asks first; esc cancels.
	press(t, m, "t")
	assertScreen(t, m, "Discard local changes")
	press(t, m, "esc")
	if !m.files[0].Conflicted() {
		t.Fatal("cancel must leave the conflict alone")
	}

	press(t, m, "R", "t", "y")
	assertScreen(t, m, "Took theirs for f.txt")
	if got := readWC(t, m, "f.txt"); got != "1\nTHEIRS2\n3\n4\n5\n6\nTHEIRS7\n8\n" {
		t.Fatalf("file = %q", got)
	}
	for _, f := range m.files {
		if f.Path == "f.txt" {
			t.Fatalf("theirs-full leaves no local change, but status has %+v", f)
		}
	}
}

func TestResolveMenuMineConflict(t *testing.T) {
	m := setupConflict(t)
	press(t, m, "R", "M")
	assertScreen(t, m, "Kept mine (conflicts) for f.txt")
	if got := readWC(t, m, "f.txt"); got != "1\nMINE2\n3\n4\n5\n6\nMINE7\n8\n" {
		t.Fatalf("file = %q", got)
	}
}
