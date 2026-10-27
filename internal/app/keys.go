package app

import (
	"fmt"

	"github.com/cupskrrtt/seven/internal/ui/style"
)

var helpSections = []struct {
	name  string
	binds [][2]string
}{
	{"Global", [][2]string{
		{"tab / shift+tab", "next / previous panel"},
		{"1 2 3 4", "jump to Status / Files / Branches / Log"},
		{"enter", "focus main view"},
		{"r", "refresh"},
		{"u", "update to HEAD"},
		{"U", "update to revision"},
		{"@", "toggle command log"},
		{"?", "this help"},
		{"q", "quit"},
	}},
	{"Files", [][2]string{
		{"enter", "folder: collapse / expand · file: focus diff"},
		{"- / =", "collapse all / expand all"},
		{"`", "toggle tree / flat list"},
		{"space", "select / deselect for commit (folder: all inside)"},
		{"A", "select all / clear selection"},
		{"c", "commit selected (or all changes)"},
		{"C", "commit using $SVN_EDITOR / $EDITOR"},
		{"a", "add unversioned file (folder: all inside)"},
		{"d", "discard changes, file or whole folder (backup saved first)"},
		{"D", "svn rm (schedule deletion)"},
		{"i", "add to svn:ignore"},
		{"b", "blame"},
		{"l", "show this file's log"},
		{"L", "changelist: move / remove / select all"},
		{"R", "resolve conflict (mine / theirs / base / pick hunks…)"},
		{"enter", "on a conflicted file: hunk picker"},
		{"e", "open file in editor"},
		{"w", "toggle ignore whitespace in diff"},
	}},
	{"Branches", [][2]string{
		{"space", "switch working copy to branch / tag"},
		{"n", "new branch from current location"},
		{"t", "new tag from current location"},
		{"m", "merge branch into working copy"},
		{"e", "list revisions not merged yet"},
		{"enter", "show branch log (then c to cherry-pick)"},
		{"D", "delete branch / tag"},
	}},
	{"Log", [][2]string{
		{"j / k", "move (older pages load automatically)"},
		{"enter", "focus revision diff"},
		{"c", "cherry-pick revision (from another branch's log)"},
		{"R", "revert revision (reverse merge, then commit)"},
		{"U", "update working copy / file to revision"},
		{"/", "filter: search text, path, revision range"},
		{"s", "toggle stop-on-copy"},
		{"esc", "reset to working copy log"},
	}},
	{"Diff (main view)", [][2]string{
		{"/", "search (smart case); esc clears"},
		{"n / N", "next / previous match (no search: next / previous hunk)"},
		{"z", "fold / unfold the file at the top"},
		{"Z", "fold / unfold all files (10+ files open folded)"},
		{"} / {", "jump to next / previous file"},
		{"] / [", "select next / previous hunk"},
		{"d", "revert selected hunk (backup saved first)"},
		{"j / k", "scroll"},
		{"ctrl+d / ctrl+u", "half page down / up"},
		{"h / l", "pan left / right"},
		{"g / G", "top / bottom"},
		{"esc", "back to panel"},
	}},
	{"Blame", [][2]string{
		{"j / k", "move"},
		{"enter", "show the revision of this line"},
		{"esc", "back"},
	}},
	{"Conflict picker", [][2]string{
		{"n / p", "next / previous conflict"},
		{"m", "keep mine for this conflict"},
		{"t", "keep theirs for this conflict"},
		{"b", "keep both (mine, then theirs)"},
		{"B", "keep base (common ancestor)"},
		{"z", "undo last pick"},
		{"e", "edit file in $EDITOR"},
		{"esc", "back (picks are already saved)"},
	}},
}

func helpLines() []string {
	var lines []string
	for _, s := range helpSections {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, style.Title.Render(s.name))
		for _, b := range s.binds {
			lines = append(lines, fmt.Sprintf("  %s %s", style.Key.Render(fmt.Sprintf("%-16s", b[0])), b[1]))
		}
	}
	return lines
}
