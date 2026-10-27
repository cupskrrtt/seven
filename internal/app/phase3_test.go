package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/config"
)

func TestPropertiesEditor(t *testing.T) {
	m, _ := setup(t)
	press(t, m, "1", "P")
	assertScreen(t, m, "Properties · .", "no properties — press a to add one")

	press(t, m, "a")
	assertScreen(t, m, "Add property", "svn:ignore", "svn:externals", "svn:needs-lock")
	press(t, m, "esc")

	// Simulate $EDITOR returning a multi-line value.
	f := filepath.Join(t.TempDir(), "value.txt")
	os.WriteFile(f, []byte("*.log\nbuild\n"), 0o644)
	_, cmd := m.Update(propEditedMsg{path: ".", name: "svn:ignore", file: f})
	drain(t, m, cmd)
	assertScreen(t, m, "Set svn:ignore on .", "svn:ignore", "*.log", "build")

	press(t, m, "d")
	assertScreen(t, m, "Remove svn:ignore from .?")
	press(t, m, "y")
	assertScreen(t, m, "Deleted svn:ignore", "no properties")
	press(t, m, "esc")
	if m.props != nil {
		t.Fatal("esc should close the property view")
	}
}

func TestIncomingChangesAndLocks(t *testing.T) {
	m, trunk := setup(t)
	other := filepath.Join(t.TempDir(), "other")
	sh(t, ".", "svn", "checkout", "-q", trunk, other)
	os.WriteFile(filepath.Join(other, "c.txt"), []byte("remote\n"), 0o644)
	sh(t, other, "svn", "add", "-q", "c.txt")
	sh(t, other, "svn", "commit", "-q", "-m", "remote add")

	press(t, m, "*")
	assertScreen(t, m, "incoming", "vs r3", "c.txt ↓A") // "." is behind HEAD too
	// The remote-only entry is not something to commit.
	press(t, m, "2", "c")
	assertScreen(t, m, "Commit all 1 change(s)")
	press(t, m, "esc")
	press(t, m, "x")
	assertScreen(t, m, "Hide incoming changes")
	press(t, m, "*")
	if m.remoteStatus || strings.Contains(screen(m), "incoming") {
		t.Fatal("incoming changes should be hidden again")
	}

	// Lock and unlock a.txt.
	selectFile(t, m, "a.txt")
	press(t, m, "K")
	assertScreen(t, m, "Lock a.txt")
	typeText(t, m, "editing")
	press(t, m, "enter")
	assertScreen(t, m, "Locked a.txt", "a.txt (locked by me)")
	press(t, m, "K")
	assertScreen(t, m, "Release your lock on a.txt?")
	press(t, m, "y")
	assertScreen(t, m, "Unlocked a.txt")
}

func TestRepoBrowser(t *testing.T) {
	m, _ := setup(t)
	commitAll(t, m, "second version") // r3: a.txt = one\nLOCAL

	press(t, m, "B")
	assertScreen(t, m, "Browse · ^/trunk @HEAD", "a.txt", "../")
	press(t, m, "enter") // a.txt
	assertScreen(t, m, "File · ^/trunk/a.txt @HEAD", "LOCAL")
	press(t, m, "/")
	typeText(t, m, "local")
	press(t, m, "enter")
	assertScreen(t, m, "1 match(es)")
	press(t, m, "esc", "esc") // clear search, then back to the listing
	assertScreen(t, m, "Browse · ^/trunk @HEAD")

	// Older revision.
	press(t, m, "R")
	typeText(t, m, "2")
	press(t, m, "enter")
	assertScreen(t, m, "Browse · ^/trunk @r2")
	press(t, m, "enter")
	if s := screen(m); strings.Contains(s, "LOCAL") || !strings.Contains(s, "File · ^/trunk/a.txt @r2") {
		t.Fatalf("a.txt@r2 should not contain LOCAL:\n%s", s)
	}
	press(t, m, "esc")

	// Up to the repository root, then export trunk.
	press(t, m, "h")
	assertScreen(t, m, "Browse · ^/ @r2", "trunk/")
	press(t, m, "x")
	assertScreen(t, m, "Export ^/trunk @r2 to")
	press(t, m, "enter")
	assertScreen(t, m, "Export to")
	exported := filepath.Join(filepath.Dir(m.client.Dir), "trunk", "a.txt")
	if data, err := os.ReadFile(exported); err != nil || string(data) != "one\n" {
		t.Fatalf("exported a.txt = %q, %v", data, err)
	}

	press(t, m, "esc")
	if m.browser != nil || m.focus != panelFiles {
		t.Fatalf("esc should close the browser, focus=%v", m.focus)
	}
}

func TestShelveAndUnshelve(t *testing.T) {
	m, _ := setup(t)
	os.WriteFile(filepath.Join(m.client.Dir, "c.txt"), []byte("added\n"), 0o644)
	sh(t, m.client.Dir, "svn", "add", "-q", "c.txt")
	press(t, m, "r", "2", "S")
	assertScreen(t, m, "Shelves", "Shelve all 2 local change(s)")
	press(t, m, "s")
	assertScreen(t, m, "WIP on trunk")
	press(t, m, "enter")
	assertScreen(t, m, `Shelved 2 path(s) as "WIP on trunk"`)

	if got := readWC(t, m, "a.txt"); got != "one\n" {
		t.Fatalf("a.txt after shelve = %q", got)
	}
	if _, err := os.Stat(filepath.Join(m.client.Dir, "c.txt")); !os.IsNotExist(err) {
		t.Fatal("added c.txt should be removed by shelving")
	}
	for _, f := range m.files {
		if f.Path != "b.txt" { // unversioned files are left alone
			t.Fatalf("unexpected change after shelving: %+v", f)
		}
	}

	press(t, m, "S")
	assertScreen(t, m, "WIP on trunk", "2 path(s), from trunk r1")
	press(t, m, "1", "v")
	assertScreen(t, m, "Shelf · WIP on trunk", "+LOCAL", "+added")
	press(t, m, "esc", "S", "1", "p")
	assertScreen(t, m, `Unshelved "WIP on trunk"`, "M   a.txt", "A   c.txt")
	if got := readWC(t, m, "a.txt"); got != "one\nLOCAL\n" {
		t.Fatalf("a.txt after unshelve = %q", got)
	}
	shelves, _ := m.listShelves()
	if len(shelves) != 0 {
		t.Fatalf("pop should drop the shelf: %+v", shelves)
	}
}

func TestActionsCleanup(t *testing.T) {
	m, _ := setup(t)
	press(t, m, "x")
	assertScreen(t, m, "Actions", "Cleanup", "Relocate", "Repository browser")
	press(t, m, "c")
	assertScreen(t, m, "Cleanup done")
	press(t, m, "x", "u")
	assertScreen(t, m, "Delete unversioned files")
	press(t, m, "y")
	if _, err := os.Stat(filepath.Join(m.client.Dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("b.txt should be deleted by cleanup --remove-unversioned")
	}
}

func TestCustomCommandAndRemap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	m, _ := setup(t)
	m.custom = []config.CustomCommand{
		{Key: "X", Context: "files", Command: "echo file={{.File}} quoted={{quote .File}}", ShowOutput: true, Description: "Echo"},
		{Key: "V", Context: "log", Command: "echo rev={{.Revision}}", ShowOutput: true, Confirm: true},
	}
	m.remap = map[string]string{"Q": "?"}

	selectFile(t, m, "a.txt")
	press(t, m, "X")
	assertScreen(t, m, "Echo", "file=a.txt quoted=a.txt")
	press(t, m, "esc")

	press(t, m, "4", "g", "V")
	assertScreen(t, m, "Run custom command", "echo rev=2")
	press(t, m, "y")
	assertScreen(t, m, "rev=2")
	press(t, m, "esc")

	// The same key does nothing in another panel.
	press(t, m, "2", "V")
	if m.popup != nil {
		t.Fatal("log-only command fired in the Files panel")
	}

	press(t, m, "Q")
	assertScreen(t, m, "Keybindings")
}

func TestMouse(t *testing.T) {
	m, _ := setup(t)
	statusH, filesH, branchesH, _ := m.leftHeights(m.height - 1)

	// Click the second row of the Files panel (b.txt).
	_, cmd := m.Update(tea.MouseMsg{X: 5, Y: statusH + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drain(t, m, cmd)
	if f, _ := m.currentFile(); f.Path != "b.txt" || m.focus != panelFiles {
		t.Fatalf("click selected %q, focus %v", f.Path, m.focus)
	}
	// Wheel over the Log panel focuses it and moves the selection.
	_, cmd = m.Update(tea.MouseMsg{X: 5, Y: statusH + filesH + branchesH + 2, Button: tea.MouseButtonWheelDown})
	drain(t, m, cmd)
	if m.focus != panelLog || m.logList.cursor != 1 {
		t.Fatalf("wheel: focus %v cursor %d", m.focus, m.logList.cursor)
	}
	// Clicking the main view focuses it.
	_, cmd = m.Update(tea.MouseMsg{X: m.leftWidth() + 5, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drain(t, m, cmd)
	if m.focus != panelMain {
		t.Fatalf("focus = %v", m.focus)
	}
}
