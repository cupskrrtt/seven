package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cupskrrtt/seven/internal/svn"
)

func TestParseSections(t *testing.T) {
	text := "Revision: r5\n\nChanged paths:\n   M /a\n\n" +
		"Index: a.txt\n===\n--- a.txt\n+++ a.txt\n@@ -1 +1,2 @@\n-x\n+y\n+z\n" +
		"Property changes on: a.txt\n___\nAdded: svn:eol-style\n## -0,0 +1 ##\n+native\n" +
		"Property changes on: dir\n___\nModified: svn:ignore\n## -1 +1 ##\n-old\n+new\n" +
		"Index: b.txt\n===\n--- b.txt\n+++ b.txt\n@@ -1 +1 @@\n-q\n+r\n"
	lines := svn.ParseDiff(text)
	secs, of := parseSections(lines)
	var got []string
	for _, s := range secs {
		got = append(got, fmt.Sprintf("%s+%d-%d", s.name, s.added, s.removed))
	}
	// a.txt's property change stays with a.txt; dir's prop-only change is its own file.
	if strings.Join(got, " ") != "a.txt+3-1 dir+1-1 b.txt+1-1" {
		t.Fatalf("sections = %v", got)
	}
	if of[0] != -1 || of[len(of)-1] != 2 {
		t.Fatalf("sectionOf = %v", of)
	}
}

// setupBigRevision commits 12 files in one revision and opens it in the
// main view, focused.
func setupBigRevision(t *testing.T) *Model {
	t.Helper()
	m, _ := setup(t)
	wc := m.client.Dir
	var names []string
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("f%02d.txt", i)
		body := fmt.Sprintf("file %d\nline\n", i)
		if i == 7 {
			body += "needle here\n"
		}
		os.WriteFile(filepath.Join(wc, name), []byte(body), 0o644)
		names = append(names, name)
	}
	sh(t, wc, "svn", append([]string{"add", "-q"}, names...)...)
	sh(t, wc, "svn", append([]string{"commit", "-q", "-m", "twelve files"}, names...)...)
	press(t, m, "r", "4", "g")
	assertScreen(t, m, "twelve files")
	press(t, m, "enter")
	return m
}

func TestBigDiffStartsFolded(t *testing.T) {
	m := setupBigRevision(t)
	assertScreen(t, m, "· file 1/12 (12 folded)", "▶ trunk/f01.txt  +2 -0", "▶ trunk/f12.txt", "▶ trunk/f07.txt  +3 -0")
	if strings.Contains(screen(m), "+file 1") {
		t.Fatal("folded diff should hide file contents")
	}

	// z unfolds the file at the top; } moves to the next file.
	press(t, m, "z")
	assertScreen(t, m, "(11 folded)", "▼ Index: trunk/f01.txt", "+file 1")
	press(t, m, "}")
	assertScreen(t, m, "· file 2/12")
	press(t, m, "z")
	assertScreen(t, m, "(10 folded)", "+file 2")
	press(t, m, "{", "z")
	assertScreen(t, m, "· file 1/12 (11 folded)", "▶ trunk/f01.txt")

	// Z unfolds everything when something is folded... then folds all.
	press(t, m, "Z")
	assertScreen(t, m, "(12 folded)")
	press(t, m, "Z")
	if strings.Contains(screen(m), "folded)") {
		t.Fatalf("Z should unfold all:\n%s", screen(m))
	}
}

func TestDiffSearch(t *testing.T) {
	m := setupBigRevision(t)

	press(t, m, "/")
	assertScreen(t, m, "Search diff")
	typeText(t, m, "NEEDLE") // has upper case: case-sensitive, no match
	press(t, m, "enter")
	assertScreen(t, m, `No matches for "NEEDLE"`)

	press(t, m, "/")
	for range "NEEDLE" {
		press(t, m, "backspace")
	}
	typeText(t, m, "needle")
	press(t, m, "enter")
	// The match is in folded f07.txt: that file unfolds and scrolls into view.
	assertScreen(t, m, `1 match(es) for "needle"`, "/needle 1/1", "+needle here", "· file 7/12 (11 folded)")

	// Search a term in every file; n/N wrap around.
	press(t, m, "/")
	for range "needle" {
		press(t, m, "backspace")
	}
	typeText(t, m, "line")
	press(t, m, "enter")
	if len(m.mainMatches) != 12 {
		t.Fatalf("matches = %d", len(m.mainMatches))
	}
	press(t, m, "N")
	assertScreen(t, m, "Match 12/12", "/line 12/12")
	press(t, m, "n")
	assertScreen(t, m, "Match 1/12")

	// esc clears the search first, then leaves the main view.
	press(t, m, "esc")
	assertScreen(t, m, "Search cleared")
	if m.focus != panelMain || m.mainSearch != "" {
		t.Fatalf("focus=%v search=%q", m.focus, m.mainSearch)
	}
	press(t, m, "esc")
	if m.focus != panelLog {
		t.Fatalf("second esc should return to Log, focus=%v", m.focus)
	}
}
