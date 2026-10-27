package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/svn"
)

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		`log -l 3`:                    {"log", "-l", "3"},
		`commit -m "two words" a.txt`: {"commit", "-m", "two words", "a.txt"},
		`propset x 'it''s' f`:         {"propset", "x", "its", "f"},
		`cat my\ file.txt`:            {"cat", "my file.txt"},
		`  spaced   out  `:            {"spaced", "out"},
		`diff ""`:                     {"diff", ""},
	}
	for in, want := range cases {
		got, err := splitArgs(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("splitArgs(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := splitArgs(`log -m "open`); err == nil {
		t.Error("unterminated quote should fail")
	}
}

func TestCommandPrompt(t *testing.T) {
	m, _ := setup(t)
	press(t, m, ":")
	assertScreen(t, m, "Run svn command")
	typeText(t, m, "svn log -l 1 -q")
	press(t, m, "enter")
	assertScreen(t, m, "svn log -l 1 -q done", "r1 | ")
	if m.focus != panelMain {
		t.Fatalf("output should be focused, focus=%v", m.focus)
	}
	// The output is searchable like any main view.
	press(t, m, "/")
	typeText(t, m, "r1 |")
	press(t, m, "enter")
	assertScreen(t, m, "1 match(es)")
	press(t, m, "esc", "esc")

	// The prompt offers the last command again; a failing one shows its error.
	press(t, m, ":")
	if v := m.popup.(*inputPopup).inputs[0].Value(); v != "log -l 1 -q" {
		t.Fatalf("prefill = %q", v)
	}
	for range "log -l 1 -q" {
		press(t, m, "backspace")
	}
	typeText(t, m, "frobnicate")
	press(t, m, "enter")
	assertScreen(t, m, "svn frobnicate failed", "Unknown subcommand")
}

func TestKeybindingsByName(t *testing.T) {
	for _, bad := range []map[string]map[string]string{
		{"nowhere": {"commit": "x"}},
		{"files": {"comit": "x"}},
		{"files": {"commit": "Y", "add": "Y"}},
	} {
		if err := CheckKeybindings(bad); err == nil {
			t.Errorf("CheckKeybindings(%v) should fail", bad)
		}
	}

	m, _ := setup(t)
	var err error
	m.keys, err = newKeymap(map[string]map[string]string{
		"files":  {"commit": "Y", "select": "s"},
		"global": {"help": "H"},
	})
	if err != nil {
		t.Fatal(err)
	}
	press(t, m, "2", "c")
	if m.popup != nil {
		t.Fatal("c should no longer commit after rebinding")
	}
	press(t, m, "Y")
	assertScreen(t, m, "Commit all 1 change(s)")
	press(t, m, "esc")

	// "space" in the table is the space bar; moving it works both ways.
	selectFile(t, m, "a.txt")
	press(t, m, " ")
	if len(m.selected) != 0 {
		t.Fatal("space should no longer select")
	}
	press(t, m, "s")
	if !m.selected["a.txt"] {
		t.Fatalf("s should select now: %v", m.selected)
	}

	press(t, m, "?")
	if m.popup != nil {
		t.Fatal("? was rebound to H")
	}
	press(t, m, "H")
	assertScreen(t, m, "Keybindings")
	// Help shows the effective key and the name to rebind it with.
	if !strings.Contains(strings.Join(m.popup.(*helpPopup).lines, "\n"), "H") {
		t.Fatal("help should list the new key")
	}
	found := false
	for _, l := range m.popup.(*helpPopup).lines {
		if strings.Contains(l, "files.commit") && strings.Contains(l, "Y") {
			found = true
		}
	}
	if !found {
		t.Fatal("help should show files.commit on Y")
	}
}

func TestRefreshOnFocus(t *testing.T) {
	m, _ := setup(t)
	os.WriteFile(filepath.Join(m.client.Dir, "late.txt"), []byte("x"), 0o644)
	_, cmd := m.Update(tea.FocusMsg{})
	drain(t, m, cmd)
	assertScreen(t, m, "late.txt")
}

func TestSideBySideRows(t *testing.T) {
	text := "Index: a.txt\n===\n--- a.txt\n+++ a.txt\n@@ -10,4 +10,5 @@\n ctx\n-old1\n-old2\n+new1\n+new2\n+new3\n tail\n"
	m := &Model{width: 120, height: 40, sideBySide: true}
	m.mainRaw = svn.ParseDiff(text)
	m.mainLines = make([]string, len(m.mainRaw))
	m.resetDiffView()
	type row struct {
		full              bool
		left, right, o, n int
	}
	var got []row
	for _, r := range m.mainRows {
		got = append(got, row{r.full, r.left, r.right, r.oldNo, r.newNo})
	}
	want := []row{
		{true, -1, -1, 0, 0}, {true, -1, -1, 0, 0}, {true, -1, -1, 0, 0}, {true, -1, -1, 0, 0}, // headers
		{true, -1, -1, 0, 0},   // @@
		{false, 5, 5, 10, 10},  // ctx on both sides
		{false, 6, 8, 11, 11},  // old1 | new1
		{false, 7, 9, 12, 12},  // old2 | new2
		{false, -1, 10, 0, 13}, //      | new3
		{false, 11, 11, 13, 14},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows:\n%v\nwant\n%v", got, want)
	}
	// Narrow terminals fall back to unified.
	m.width = 80
	m.buildRows()
	if len(m.mainRows) != len(m.mainRaw) || !m.mainRows[6].full {
		t.Fatalf("narrow: %d rows", len(m.mainRows))
	}
}

func TestSideBySideUI(t *testing.T) {
	m, _ := setup(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	selectFile(t, m, "a.txt")
	press(t, m, "enter", "v")
	assertScreen(t, m, "Side-by-side diff")
	s := screen(m)
	found := false
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "1 one") && strings.Count(l, "│") >= 3 { // context on both sides
			found = true
		}
	}
	if !found {
		t.Fatalf("expected two-column context row:\n%s", s)
	}
	assertScreen(t, m, "2 LOCAL")

	// Hunk selection and revert still work in two columns.
	press(t, m, "]", "d", "y")
	assertScreen(t, m, "Reverted hunk 1 of a.txt")
	press(t, m, "v")
	assertScreen(t, m, "Unified diff")
}
